# Technical Design

## Architecture and boundaries

Use one backend-owned configuration contract across persistence, capability discovery, validation, direct HTTP requests, and Eino Agent creation.

```text
Vue configuration form
  -> capability descriptor RPC
  -> single-item CRUD RPC
  -> validation + normalization service
  -> SQLite ai_config
  -> runtime settings refresh + aiConfigsChanged event

Saved AIConfig + per-session thinking override
  -> effective parameter resolver
  -> direct streaming/tool body builder
  -> Eino provider adapter factory
```

The current bulk `UpdateAiConfigs`/`UpdateAiConfigsOnly` path remains for settings import compatibility. The configuration manager stops calling it.

## Data contract and migration

Extend `AIConfig` with structured optional fields:

```go
MaxCompletionTokens *int
Temperature         *float64
TemperatureConfigured bool
TopP                *float64
TopK                *int
PresencePenalty     *float64
FrequencyPenalty    *float64
Seed                *int64
StopSequences       []string // persisted with a JSON serializer
ResponseFormat      string   // empty/text/json_object
ReasoningMode       string   // off/auto/on
ReasoningEffort     string
ReasoningBudget     *int
IsDefault           bool
```

Compatibility rules:

- Existing `max_tokens` remains the visible-output/default token ceiling for adapters that use it.
- `max_completion_tokens` is separate because some reasoning APIs include reasoning tokens in this limit.
- Pointer fields preserve explicit zero values; the old non-pointer `Temperature` column migrates without data loss. If changing the existing Go field to a pointer creates unsafe GORM migration behavior, keep the storage column and introduce an explicit “configured” flag rather than destructive migration.
- Existing `thinking=true` migrates at read/normalization time to `reasoningMode=on`; `false` migrates to `off`. The legacy column remains readable during the compatibility window.
- `AutoMigrate` adds columns only; no destructive table rewrite or data deletion is planned.

## Capability descriptor API

Add a read-only App method such as:

```go
GetAIModelCapabilities(baseURL string, modelName string) AIModelCapabilities
```

The descriptor contains:

- provider/profile identifier and display name;
- supported fields and their labels, descriptions, ranges, defaults, units, and enum options;
- conflicts such as Temperature vs Top P;
- reasoning mode/effort/budget support;
- incompatible saved-field warnings when a full configuration is validated.

Provider detection is extracted from `chat_model_factory.go` into a shared backend package or data-layer helper so the descriptor, validation, direct request builder, and Agent factory use the same result.

## Single-item CRUD contracts

Add App methods with typed results/errors:

```go
CreateAIConfig(config *AIConfig) (*AIConfig, error)
UpdateAIConfig(config *AIConfig) (*AIConfig, error)
CopyAIConfig(id uint) (*AIConfig, error)
DeleteAIConfig(id uint) (*DeleteAIConfigResult, error)
```

- Create rejects a nonzero ID, validates/normalizes, inserts, reloads the row, refreshes runtime settings, and emits `aiConfigsChanged`.
- Update requires an existing ID and updates an explicit field map so zero, false, empty, and null states are handled deliberately.
- Copy runs on the server so the source cannot drift between confirmation and insert. It clears identity/timestamps/session state and chooses `名称-副本`, `名称-副本2`, etc.
- Delete checks references and deletes in one transaction. A referenced config returns a structured result/error carrying every recognized reference and leaves the row unchanged.
- All failures use user-safe messages and never include API keys.

## Delete reference protection

The transaction checks:

1. `Settings.FeishuBotAiConfigId` and reports a “飞书机器人” reference.
2. Every `CronTask.Params` JSON object for both `aiConfigId` and legacy `ai_config_id`; reports task ID, name, and task type when matched.

Malformed unrelated cron JSON does not block deletion, but malformed JSON that appears to contain the target key is logged without secrets and returns a safe validation error so a possible reference is not silently orphaned.

## Global default model contract

All callers share one model-resolution order:

1. a positive explicit ID that still exists;
2. the persisted `is_default=true` row;
3. the lowest stable ID as a legacy-data compatibility fallback.

The first created configuration becomes default. Creating later configurations does not replace it, copying never copies the default flag, and deleting the default promotes the lowest remaining ID inside the delete transaction. Normal update RPCs cannot change `isDefault`; only `SetDefaultAIConfig(id)` owns default switching.

`SetDefaultAIConfig` clears the old marker and sets the target marker in one database transaction, then refreshes runtime settings and emits `aiConfigsChanged`. Zero or missing IDs fail without clearing the current default.

Startup migration runs synchronously during `db.Init`: add `is_default` before any settings read, repair multiple defaults by keeping the lowest marked ID, and assign the lowest row when legacy data has no default. The migration is idempotent and must not be moved into lazy settings reads.

## Effective parameter resolver

Create a pure resolver with inputs `(savedConfig, sessionThinkingEnabled)` and output `(EffectiveAIParameters, warnings, error)`.

- It applies legacy defaults, selects a capability profile, validates ranges/enums, resolves incompatible combinations, and strips unsupported fields.
- Session thinking off forces reasoning off for that call.
- Session thinking on preserves the saved reasoning mode, effort, and budget.
- The resolver copies data and never mutates the configuration object returned from settings.
- Direct chat/tool requests and Agent creation consume the same resolved semantics. Provider-specific mapping remains close to the adapter, but capability decisions do not.

For direct requests, centralize construction of the common generation/reasoning fields so `AskAi` and `AskAiWithToolsDepth` cannot drift. Keep `model`, `messages`, `tools`, and streaming control owned by request code.

## Frontend behavior

- Remove the global “保存配置” button and stale instruction text.
- Keep one drawer for add/edit. Copy is a confirmation followed by immediate server-side duplication, not a second editable draft flow.
- Use Naive UI dialog confirmation for copy/delete. Disable the row action while its request is running.
- The drawer groups fields into connection, generation, reasoning, and network sections.
- The drawer save button says “保存”, prevents double submit, closes only after success, and preserves values after failure.
- Reload capabilities when base URL or model name changes; ignore stale async responses by request sequence.
- Render the base URL as an editable autocomplete: presets remain discoverable, while arbitrary HTTP(S) compatible endpoints can be typed or pasted. Debounce capability reloads during typing and reload immediately when a preset is selected.
- Render only descriptor-supported controls. Every advanced label uses a consistent help icon/tooltip component or local render pattern.
- Show incompatible legacy values as warnings; never discard them merely because a field becomes hidden. Save requires explicit normalization accepted by the backend.
- Emit/listen for `aiConfigsChanged` so model selectors in the active UI reload without using the existing `updateSettings` event, which reloads the entire desktop window.

## Wails and Web compatibility

Regenerate `frontend/wailsjs/go/main/App.js`, `App.d.ts`, and `models.ts` after adding methods/types. `web_server.go` derives its RPC allowlist from generated `App.js`, so successful binding generation makes the new APIs available in Web mode while preserving the desktop-only exclusions documented by the backend runtime guidelines.

## Security and error handling

- Required values are trimmed and validated; base URLs must be HTTP(S), proxy values must be valid URLs when enabled, and enum/numeric fields must match the selected capability profile.
- API keys are accepted and persisted as before but masked from logs, formatted errors, confirmation text, and result metadata.
- Capability detection does not call third-party endpoints and therefore does not need the API key.
- Runtime fallback retries may omit an unsupported reasoning field for availability, but should return/log a diagnosable warning without changing the saved configuration.

## Rollout and rollback

- Schema additions are backward compatible; rollback to an older binary ignores new columns.
- The legacy bulk update method stays available for imports and can serve as a temporary UI fallback during development, but the released manager must use single-item RPCs.
- If a provider mapping proves unstable, remove that field from its backend capability profile; stored values remain in SQLite and the UI shows them as incompatible rather than deleting them.

## Validation and regression matrix

- Create rejects a nonzero ID; update/copy/delete/default-switch reject zero or missing IDs.
- Required text is trimmed; base URL and an enabled proxy must be valid HTTP(S) URLs. Preset base URLs are suggestions, not an allowlist.
- Optional numeric values preserve explicit zero and reject NaN, infinity, unsupported fields and capability-range violations.
- `reasoningMode=off` rejects effort/budget; Gemini effort and budget are mutually exclusive; Claude `on` requires a budget smaller than `maxTokens`.
- Direct requests send either `max_tokens` or `max_completion_tokens`, never both.
- A session-level thinking override operates on a copy: disabling strips reasoning fields; enabling preserves the saved mode, including a saved `off`.
- A referenced delete returns every recognized reference and leaves the row unchanged; malformed possible-reference JSON fails closed.
- Provider, resolver, persistence, default migration, binding and frontend build tests must cover the exact behavior above. Manual UI verification must prove that preset and pasted custom HTTP(S) base URLs remain editable and reach backend validation unchanged.
