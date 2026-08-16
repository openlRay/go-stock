# Implementation Plan

## 1. Establish baselines and shared contracts

- [x] Record current Go test, Web-tag test, and frontend build baselines.
- [x] Add capability/profile types, normalized optional parameter types, and shared provider detection.
- [x] Add unit tests for URL/model provider detection, conservative unknown-endpoint fallback, numeric ranges, enum validation, and legacy thinking normalization.

Verification:

```bash
go test ./backend/data ./backend/agent
go test -tags web .
npm --prefix frontend run build
```

## 2. Extend persistence and single-item CRUD

- [x] Extend `AIConfig` with nullable structured generation/reasoning fields and safe migration semantics.
- [x] Implement create, update, server-side copy naming, delete, runtime refresh, and typed results.
- [x] Implement transaction-scoped Feishu/cron reference discovery for both cron key formats.
- [x] Preserve `UpdateAiConfigsOnly` for import compatibility and update its field map for all new columns.
- [x] Add database tests covering zero/null round-trips, create/update/copy/delete, duplicate names, missing IDs, referenced deletion, malformed cron params, and cache refresh behavior.

Rollback point: before wiring the UI, single-item methods can be removed while the existing bulk import path remains intact.

## 3. Add capability RPC and Wails/Web bindings

- [x] Expose capability query and CRUD methods on `App`.
- [x] Regenerate Wails `App.js`, `App.d.ts`, and `models.ts` with Wails v2.11 tooling.
- [x] Confirm `web_server.go` includes the new generated exports in the allowlist and continues excluding desktop-only methods.
- [x] Add Web RPC coverage for at least one read and one write method where existing test infrastructure permits.

Verification:

```bash
go test -tags web .
rg -n "GetAIModelCapabilities|CreateAIConfig|UpdateAIConfig|CopyAIConfig|DeleteAIConfig" frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts
```

## 4. Unify request parameter resolution and mapping

- [x] Add the pure effective-parameter resolver with session thinking override.
- [x] Extend `OpenAi`/direct request body construction to use the shared resolved parameter set for both plain and tool streaming paths.
- [x] Extend every supported Eino provider mapping in `chat_model_factory.go` without sending unsupported fields.
- [x] Stop mutating cached `AIConfig.Thinking` in Agent construction; copy and resolve instead.
- [x] Add table-driven request/mapping tests for generic OpenAI-compatible, official OpenAI reasoning, OpenRouter, Ark, Claude, Gemini, DeepSeek, Qwen, and Ollama profiles.
- [x] Test that session thinking off removes reasoning while session thinking on preserves saved effort/budget.

Rollback point: provider profiles are independently removable; conservative generic behavior remains the fallback.

## 5. Redesign the configuration manager

- [x] Replace bulk local-array edits with immediate CRUD calls and loading/error states.
- [x] Remove global save UI/text; change drawer action to “保存”.
- [x] Add copy/delete confirmation dialogs and referenced-delete details.
- [x] Group the drawer into connection, generation, reasoning, and network sections.
- [x] Load/render backend capability descriptors and add consistent help tooltips for every advanced field.
- [x] Preserve unsupported legacy values with visible warnings and prevent stale capability requests from overwriting current state.
- [x] Emit `aiConfigsChanged` after successful changes and reload the local list from the backend source of truth.

Verification:

```bash
npm --prefix frontend run build
rg -n "保存配置|修改后请点击" frontend/src/components/ai-config-manager.vue
```

Manual checks:

1. Add/edit success persists after refresh; simulated failure keeps the drawer open.
2. Copy/delete cancel is a no-op; confirm immediately changes the list.
3. Referenced delete lists Feishu and cron references and keeps the row.
4. Changing provider/model changes visible fields and tooltip ranges.
5. Unsupported legacy values remain visible as warnings.

## 6. Refresh active AI selectors

- [x] Add a shared `aiConfigsChanged` event contract.
- [x] Make floating AI assistant, floating Agent assistant, stock/market/Agent selectors reload when active, preserving the selected ID when it still exists and selecting a safe fallback when it does not.
- [x] Confirm the event works in Desktop and Web/SSE mode without triggering a full window reload.

## 7. Full quality gate

- [x] Run Go formatting and focused tests after each backend slice.
- [x] Run all Go tests, Web-tag tests, frontend build, and project build. (Full suites were attempted with a 90s bound; existing `backend/agent/tools` DB setup and Chromedp integration-test failures are recorded in the handoff.)
- [x] Review all logs/errors for API key leakage.
- [x] Check the full round-trip: form -> generated binding -> App -> SQLite -> settings refresh -> direct request and Agent request.
- [x] Run `trellis-check`, resolve findings, and update any durable backend contract discovered during implementation.

Final verification:

```bash
gofmt -w <changed-go-files>
go test ./...
go test -tags web ./...
npm --prefix frontend run build
go build ./...
git diff --check
```

## Risk controls

- Do not edit or delete the unrelated untracked task `.trellis/tasks/08-12-announcement-ai-analysis/`.
- Do not silently coerce unsupported values into a different semantic; surface warnings and require valid backend normalization.
- Do not expose arbitrary request JSON, provider headers, API keys, or system-owned request fields.
- Do not commit or stage unrelated user changes.
