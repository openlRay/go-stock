# Implementation Plan

## 1. Establish baselines and reconcile concurrent AI-config work

- [ ] Re-read the active diffs before editing and stop if product files overlap with incompatible uncommitted changes.
- [ ] Load `trellis-before-dev` and the backend/Web runtime specs.
- [ ] Record baseline Go tests, Web-tag tests, frontend build, and desktop build.
- [ ] Inspect whether the parallel AI model configuration redesign has introduced shared context-window/effective-parameter APIs; consume them when present and do not redefine `MaxTokens` semantics.

Baseline verification:

```bash
go test ./backend/data ./backend/agent
go test -tags web .
npm --prefix frontend run build
go build .
```

## 2. Add announcement analysis contracts and persistence

- [ ] Add typed request, event, preflight, result, phase, and safe error contracts.
- [ ] Add `AnnouncementAIAnalysis` with unique `art_code`, metadata, model/prompt provenance, final content, and timestamps.
- [ ] Register the model in `AutoMigrate`.
- [ ] Implement validated get-latest and atomic upsert-by-`art_code` service methods.
- [ ] Add isolated SQLite tests proving one-row uniqueness, successful replacement, and preservation of the old result when generation/save does not complete.

Rollback point: the additive model/service can be removed before UI wiring without changing existing AI history behavior.

## 3. Implement bounded PDF acquisition and text extraction

- [ ] Promote `github.com/ledongthuc/pdf` to a direct dependency at the existing checked version.
- [ ] Add strict `artCode` validation and canonical fixed-host URL construction.
- [ ] Download with request context, timeout, no automatic retry, status/type/signature validation, and a maximum byte limit; permit only one strictly recognized, time-bounded Eastmoney cookie challenge handshake to the same fixed URL.
- [ ] Extract plain text in memory and normalize it without collapsing useful paragraph/table separation.
- [ ] Return stable errors for invalid ID, HTTP failure, oversized/non-PDF body, parse error, and scanned/empty text.
- [ ] Add deterministic unit tests with `httptest` and local PDF fixtures; keep live endpoint tests optional/integration-only.

Verification:

```bash
go test ./backend/data -run 'Announcement|PDF'
```

## 4. Add context-capacity resolver and preflight

- [ ] Add a reusable conservative mixed-language token estimator with table-driven tests.
- [ ] Resolve context window from shared capability data when available and otherwise return the confirmed 200,000 Token fallback with an explicit source.
- [ ] Keep context capacity separate from configured completion/output token limits.
- [ ] Build the full internal prompt input and calculate estimate, reserve, safe budget, and allow/reject decision.
- [ ] Prove an over-budget request is rejected before the model transport is invoked.

Tests must cover known capacity, unknown-model 200K fallback, exact boundary, over-limit rejection, empty input, and Chinese/Latin mixed text.

## 5. Implement the versioned prompt and strict one-request stream

- [ ] Add `announcement-analysis-v1` internal system/user prompt builders.
- [ ] Refactor the current OpenAI-compatible SSE parser/request execution into a reusable one-request helper without changing existing caller retry behavior.
- [ ] Add a dedicated announcement streaming function that disables tools and all retry/fallback paths and sends exactly one provider request.
- [ ] Stream only final answer content and metadata; do not persist/display reasoning deltas.
- [ ] Add transport tests asserting request count equals one on success, provider 4xx, malformed stream, and unsupported-parameter failure.
- [ ] Ensure request bodies contain the selected announcement text and no tool definitions or external-data messages.

Rollback point: existing AI summary/stock analysis retains its orchestration around the extracted low-level helper.

## 6. Add App lifecycle, cancellation, persistence ordering, and events

- [ ] Add a dedicated mutex/cancel/request-ID state to `App`.
- [ ] Expose get/start/abort methods and start the analysis worker asynchronously.
- [ ] Emit typed preparing, preflight, streaming, completed, failed, and cancelled events through `a.emit`.
- [ ] Accumulate content and upsert only after a successful complete stream; emit completed only after the database write succeeds.
- [ ] Preserve the prior row on preflight rejection, provider error, cancellation, empty output, and save failure.
- [ ] Add tests for cancellation, stale request IDs, replacement ordering, save failure, and event phase ordering.

## 7. Regenerate bindings and verify Web RPC/SSE compatibility

- [ ] Regenerate `frontend/wailsjs/go/main/App.js`, `App.d.ts`, and model bindings using Wails v2.11.
- [ ] Confirm the generated exports enter the Web allowlist and every exported method exists on `App`.
- [ ] Extend Web tests to cover the new read/start/abort methods and typed SSE payload serialization where practical.
- [ ] Confirm no desktop-only API or browser-local path is used.

Binding command when no global CLI is installed:

```bash
go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 generate module
```

Verification:

```bash
go test -tags web .
rg -n "GetAnnouncementAIAnalysis|StartAnnouncementAIAnalysis|AbortAnnouncementAIAnalysis" frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts
```

## 8. Implement the single-announcement modal UX

- [ ] Add the per-row “AI 解读” action without modifying the title/PDF click behavior.
- [ ] Load model configurations and the saved result when the modal opens.
- [ ] Add model-only selection, start/reanalyze, cancel, copy, and open-original actions.
- [ ] Render Markdown and model/time metadata; show capacity estimate/source and every lifecycle/error state.
- [ ] Keep the saved result visible until a replacement is both generated and persisted successfully.
- [ ] Register/unregister one event listener and filter events by request ID plus `artCode`.
- [ ] Handle no-model, empty-result, capacity overflow, cancellation, provider failure, and save failure without stale loading state.

Manual checks:

1. Clicking the announcement title still opens the original PDF.
2. First analysis streams in the modal and survives closing/reopening after save.
3. Reanalysis failure/cancellation leaves the saved result unchanged.
4. Unknown model shows the 200K fallback; oversized input is blocked before generation.
5. Desktop and Web UI receive equivalent events; copy and original-link actions work in both.

## 9. Full quality gate

- [ ] Format changed Go files and run focused tests after each backend slice.
- [ ] Run all Go tests, Web-tag tests, frontend build, and desktop build.
- [ ] Check dependency and generated-binding diffs are intentional.
- [ ] Review logs/errors for API keys, full announcement contents, raw provider bodies, and internal stack leakage.
- [ ] Verify the complete data flow: row metadata -> backend validation -> PDF -> extraction -> preflight -> one request -> typed event -> atomic upsert -> reload.
- [ ] Run `trellis-check`, resolve findings, and update durable specs if implementation reveals a reusable contract.

Final verification:

```bash
gofmt -w <changed-go-files>
go test ./...
go test -tags web ./...
npm --prefix frontend run build
go build .
git diff --check
```

## Risk controls

- Do not modify, delete, stage, or commit the unrelated task `.trellis/tasks/08-12-ai-model-config-redesign/` unless reconciling an overlapping product-code API is required; preserve its work and adapt around it.
- Do not use announcement titles as a fallback source, accept arbitrary PDF URLs, perform OCR, truncate over-limit text, call tools, or retry the model.
- Do not save partial/cancelled/failed output or delete the prior successful result before replacement succeeds.
- Do not hand-edit only one side of generated Wails bindings; regenerate and validate the Web binding allowlist.
- Stage only files produced by this task.
