# Technical Design

## Architecture and boundaries

Keep announcement acquisition, preflight, one-shot streaming, persistence, and UI rendering behind typed boundaries:

```text
StockNoticeList row
  -> load latest result by art_code
  -> start analysis with selected AI config
  -> backend validates metadata and downloads fixed-host PDF
  -> extract full text and run token/capacity preflight
  -> exactly one tool-free streaming model request
  -> accumulate final Markdown
  -> atomic upsert by art_code after successful completion
  -> typed App event -> Wails events / Web SSE -> modal
```

No layer may fall back to title-only analysis. The frontend supplies announcement metadata for display and prompt context, but the backend owns validation, PDF URL construction, text extraction, capacity decisions, prompt construction, request count, and persistence.

## Data contracts

Add typed request, event, preflight, and result contracts under `backend/models` (or a focused model file in that package).

The analysis request contains:

- `artCode`
- `stockCode`
- `stockName`
- `title`
- `noticeType`
- `noticeDate`
- `aiConfigId`

The persisted `AnnouncementAIAnalysis` record contains:

- GORM identity/timestamps;
- `art_code` as a unique indexed business key;
- stock code/name, title, notice type/date, and canonical PDF URL;
- AI config ID and returned model name;
- internal prompt version and the fixed analysis instruction identifier;
- final Markdown content and provider chat/response ID when available.

Do not reuse `AIResponseResult`: its stock-analysis query and history semantics would mix two distinct products. Register the new model in the existing `AutoMigrate` path. Use `INSERT ... ON CONFLICT(art_code) DO UPDATE` (or an equivalent GORM transaction) only after a complete successful result exists. Never delete the previous row before generation.

## App/RPC surface and concurrency

Expose focused methods on `App`:

```go
GetAnnouncementAIAnalysis(artCode string) (*models.AnnouncementAIAnalysis, error)
StartAnnouncementAIAnalysis(req models.AnnouncementAIAnalysisRequest) (string, error)
AbortAnnouncementAIAnalysis(requestID string)
```

`Start...` validates basic input, creates a request ID, installs a cancelable context, starts the worker, and returns immediately. The App owns one active announcement analysis at a time behind a dedicated mutex/cancel/request-ID tuple. Starting another analysis cancels the previous one; events always include both request ID and `artCode`, so stale events cannot update the current modal.

Use one typed event payload with explicit phases/statuses instead of unstructured map parsing:

- `preparing`: downloading/extracting PDF;
- `preflight`: estimated input tokens, selected/default context capacity, and capacity source;
- `streaming`: final-answer Markdown delta and model/response metadata;
- `completed`: persisted result metadata;
- `failed`: stable user-safe error code/message;
- `cancelled`: no persistence mutation.

Emit through `a.emit`, never directly through the Wails runtime. This preserves the existing Wails/Web SSE boundary. Regenerated Wails bindings make the new RPCs available to Web mode through the binding-derived allowlist.

## Announcement download and PDF extraction

Validate `artCode` with a strict length and character allowlist before any request. Construct the canonical URL from the fixed HTTPS host and escaped path segment; do not accept an arbitrary frontend URL:

```text
https://pdf.dfcfw.com/pdf/H2_<artCode>_1.pdf
```

Use the shared HTTP transport behavior with a request-scoped timeout, no automatic retries, a bounded response size, and expected headers. Reject non-2xx responses, non-PDF content/signature, oversized bodies, and empty data with stable errors. A strictly recognized Eastmoney bot challenge may complete one time-bounded cookie handshake against the same fixed URL. The sandbox exposes no filesystem, process or network host API and only accepts validated `__tst_status` and `EO_Bot_Ssid` cookies. Unknown scripts, additional cookies, repeated challenge responses and any invalid second response still fail before parsing or model invocation.

Promote `github.com/ledongthuc/pdf` to a direct dependency and parse from an in-memory `bytes.Reader`; no browser, OCR, or persistent temporary file is needed. Normalize extracted whitespace conservatively while retaining paragraphs and table-like line breaks. Treat empty/negligible text as a likely scanned or unsupported PDF and do not call the model.

Tests use local fixtures or `httptest` responses; normal automated tests must not depend on the live Eastmoney endpoint.

## Token estimation and context preflight

Create a reusable non-Agent token estimator in the data/model boundary rather than importing unexported Agent internals. It should conservatively estimate mixed Chinese/Latin text and be table-tested.

Resolve context capacity in this order:

1. shared backend model capability/profile data when available;
2. reliable provider model metadata when already available through the selected configuration flow;
3. default `200000` Token fallback for unknown custom models.

Do not use the legacy `AIConfig.MaxTokens` value as context capacity: it is also sent as an output limit today and the parallel AI-model-configuration task explicitly separates those semantics.

Preflight estimates the complete system prompt, announcement metadata, full PDF text, message overhead, and output reserve. A request is allowed only when the total remains under a conservative safe budget derived from the context window (target: no more than 85% after required output reserve). Return the estimate, context window, safe budget, and source to the UI. When over budget, emit `failed/context_too_large` before any model HTTP request; do not truncate, summarize, retry, or override.

If the shared model capability work lands first, consume its context-window field. If it has not landed, implement a small compatibility resolver with the same interface and the 200K fallback so the announcement feature does not redefine output-token semantics.

## Internal prompt and strict single request

Add a versioned internal prompt constant such as `announcement-analysis-v1`. It must:

- state that the supplied announcement is the only factual source;
- forbid outside knowledge, invented figures, tool calls, and investment instructions;
- require separate Markdown sections for core facts, key figures/terms, source-grounded impact analysis, risks/uncertainties, and follow-up items;
- require explicit “not stated in the announcement” wording when evidence is absent;
- distinguish facts from inference.

The user message contains normalized announcement metadata followed by the full extracted text. The UI does not expose system/user prompt selectors, tools, thinking toggles, or free-form questions.

Refactor the existing OpenAI-compatible streaming code at the lowest useful level: extract a shared “perform one streaming chat-completion request and parse SSE” helper. Existing callers may retain their retry orchestration, while announcement analysis calls the one-request helper directly with tools disabled and retry/fallback disabled. Provider reasoning deltas are not displayed or persisted; only final answer content forms the announcement interpretation.

Use the selected model configuration's validated effective request parameters where available. Context capacity remains a separate value. Reserve a bounded completion budget suitable for the fixed report structure; do not blindly treat a large context-window value as `max_tokens` output.

## Persistence semantics

Accumulate streamed final content in memory. Persistence occurs only after all of these conditions hold:

1. the stream ended successfully rather than through cancellation/error;
2. final content is non-empty;
3. the database upsert transaction succeeds.

Only then emit `completed`. A failed save emits `failed/save_failed`, leaves the previous row untouched, and keeps the generated content visible for copying during the current modal session. Retrieval trims/validates `artCode` and returns at most the single unique row.

## Frontend behavior

Extend `StockNoticeList.vue` without changing the title link:

- add an “AI 解读” action per row;
- open a responsive modal and immediately load the saved result by `artCode`;
- show the saved result when present, with its model and generation time;
- load existing AI configs, default to the last valid model selection or first available model;
- offer “开始解读” when empty and “重新解读” when a saved result exists;
- display preparing/preflight/streaming/completed/failed/cancelled state;
- show token estimate, context window, and whether the 200K default was used;
- provide cancel while active, copy result, and open-original-announcement actions;
- retain the saved result until a new result is successfully completed and persisted.

Register the event listener once during component mount and remove it during unmount. Ignore payloads whose request ID or `artCode` does not match the active modal. Use `MdPreview` and the existing clipboard/runtime bridge patterns. No global history page is added.

## Error and security behavior

Use stable internal error categories with user-safe Chinese messages for invalid metadata, no AI configuration, download failure, oversized/non-PDF response, PDF parse failure, scanned/empty text, context overflow, provider failure, cancellation, and save failure. Logs may include `artCode`, status, sizes, estimates, and model name, but never API keys, full announcement text, or provider response bodies containing secrets.

The fixed-host URL construction prevents SSRF. Bounded downloads and response/body limits prevent uncontrolled memory growth. Cancellation must stop the download or model request through its context and must not mutate persistence.

## Compatibility, rollout, and rollback

- The new table is additive; older binaries ignore it. SQLite data remains under the existing persisted `data` directory in Docker.
- Wails bindings must be regenerated with the repository's Wails v2.11 line. The CLI is not currently global, so use an exact-version invocation such as `go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 generate module` if needed.
- Web mode receives the same events over SSE and calls the same RPCs; no browser-local file path crosses into the container.
- If model capability redesign lands concurrently, adapt to its shared capability/effective-parameter APIs and do not revert or duplicate its changes.
- Rollback can remove the new UI/RPC/worker while leaving the additive table harmless. Do not drop the table automatically.
