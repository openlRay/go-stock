# Announcement AI Analysis Contract

## 1. Scope / Trigger

Use this contract when changing single-announcement AI interpretation, announcement PDF acquisition, context-window preflight, its Wails/Web RPCs or SSE events, or its latest-result persistence.

The announcement PDF text is the only factual source. Title-only fallback, OCR, external tools/data, segmentation, recursive summaries, model retry, and truncation are outside this contract.

## 2. Signatures

The browser-facing `App` methods are:

```go
func (a *App) GetAnnouncementAIAnalysis(artCode string) (*models.AnnouncementAIAnalysis, error)
func (a *App) StartAnnouncementAIAnalysis(req models.AnnouncementAIAnalysisRequest) (string, error)
func (a *App) AbortAnnouncementAIAnalysis(requestID string)
```

The event name is `announcementAIAnalysis`; every payload includes `requestId`, `artCode`, and one phase: `preparing`, `preflight`, `streaming`, `completed`, `failed`, or `cancelled`.

Persistence uses the `announcement_ai_analysis` table with a unique `art_code` business key.

## 3. Contracts

- The frontend sends announcement metadata plus an existing `aiConfigId`; the backend validates every field.
- The backend constructs only `https://pdf.dfcfw.com/pdf/H2_<artCode>_1.pdf` from a strict `artCode` allowlist. Arbitrary URLs are never accepted.
- PDF download is bounded, context-aware, no-retry, and no-redirect; status, media type, `%PDF-` signature, size, extraction errors, and negligible text are checked before model invocation.
- Preflight includes the versioned built-in prompt, metadata, full extracted text, message overhead, and output reserve. Reliable model capability data wins; unknown models use a visible 200,000-token fallback. `AIConfig.MaxTokens` is an output limit, never context capacity.
- Announcement generation sends exactly one tool-free streaming HTTP request with redirects and automatic retries disabled. Only final-answer deltas are displayed and saved; reasoning deltas are ignored.
- The active request ID and cancellation state are checked under the announcement mutex immediately before upsert. A stale/cancelled worker cannot replace the saved row.
- Upsert happens only after a successful non-empty stream. `completed` is emitted only after upsert succeeds. The previous saved result remains visible and stored during reanalysis, failure, cancellation, preflight rejection, or provider error.
- Business events use `a.emit`, so Wails events and Web SSE receive the same typed payload.

## 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| Invalid/missing metadata or zero AI config ID | Reject before worker or network activity. |
| Missing/invalid saved AI config | Return a user-safe configuration error. |
| PDF redirect, non-2xx, oversized body, wrong type/signature | Fail before parsing/model invocation. |
| Empty/scanned/unsupported PDF text | Explain that OCR is unsupported; never analyze the title. |
| Estimated total exceeds safe context budget | Emit `context_too_large`; make zero model requests. |
| Provider 4xx/5xx, malformed/truncated stream | Fail after exactly one provider request; do not retry or persist. |
| Cancellation or stale request ID | Emit/handle cancellation and preserve the previous row. |
| Upsert failure | Emit `save_failed`; keep the previous row and current generated draft available for copying. |
| Successful upsert | Emit `completed` using the saved result; do not introduce a fallible post-save reload that can report false failure. |

## 5. Good / Base / Bad Cases

- Good: an unknown custom model shows the 200K fallback, passes preflight, streams one source-only request, and atomically replaces the row after completion.
- Base: reopening an announcement loads the latest saved result without consuming another model request.
- Bad: following a provider redirect, retrying unsupported parameters, truncating a long report, analyzing only the title, or deleting the old row before generation.

## 6. Tests Required

- Persistence: unique `art_code`, successful replacement, empty/save failure preservation.
- PDF: canonical URL, invalid ID, no redirects, size/type/signature/status checks, parse/empty-text behavior.
- Preflight: known capacity, unknown 200K fallback, boundary and over-limit rejection.
- Model transport: tools absent, reasoning ignored, text response format, exactly one request on success/error/malformed stream, redirects not followed.
- App lifecycle: phase order, persistence before completion, cancellation/stale request never persists.
- Web: generated binding allowlist includes all three methods and typed events serialize over SSE.
- Frontend/build: saved result remains visible during replacement, event filtering uses request ID plus `artCode`, desktop and Web builds compile.

## 7. Wrong vs Correct

### Wrong

```go
resp, _ := httpClient.Do(request) // follows redirects by default
deleteOldResult(artCode)
streamWithRetry(...)
```

This can escape the fixed PDF origin, make more than one model request, and lose the last successful interpretation.

### Correct

```go
client.CheckRedirect = func(*http.Request, []*http.Request) error {
    return http.ErrUseLastResponse
}
// Preflight first, stream once, then upsert while the request is still active.
```

Keep redirects/retries disabled and replace the unique row only after complete successful generation.
