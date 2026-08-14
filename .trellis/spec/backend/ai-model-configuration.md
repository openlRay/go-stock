# AI Model Configuration Contract

## Scenario: Capability-driven AI configuration with immediate persistence

### 1. Scope / Trigger

Use this contract when changing AI configuration persistence, Wails/Web RPCs,
provider detection, generation or reasoning parameters, direct chat requests,
tool chat requests, or Eino Agent model construction.

The backend is the single owner of provider capabilities and validation. The
frontend renders the returned capability descriptor; it must not maintain an
independent provider matrix.

The base URL field is an editable autocomplete boundary. Preset providers are
suggestions, not an allowlist: users must be able to type or paste a custom
OpenAI-compatible HTTP(S) endpoint.

### 2. Signatures

The browser-facing `App` methods are:

```go
func (a *App) GetAIModelCapabilities(baseURL, modelName string) data.AIModelCapabilities
func (a *App) CreateAIConfig(config *data.AIConfig) (*data.AIConfig, error)
func (a *App) UpdateAIConfig(config *data.AIConfig) (*data.AIConfig, error)
func (a *App) CopyAIConfig(id uint) (*data.AIConfig, error)
func (a *App) DeleteAIConfig(id uint) (*data.DeleteAIConfigResult, error)
func (a *App) SetDefaultAIConfig(id uint) (*data.AIConfig, error)
```

The import compatibility boundary remains:

```go
func (a *App) UpdateAiConfigs(aiConfigs []*data.AIConfig) string
```

Runtime parameter resolution is pure with respect to the saved value:

```go
func ResolveEffectiveAIParameters(
    config AIConfig,
    sessionThinkingEnabled bool,
) (EffectiveAIParameters, []string, error)

func WithSessionThinkingOverride(config AIConfig, enabled bool) AIConfig
```

### 3. Contracts

`AIConfig` persists these user-configurable fields in `ai_config`:

| JSON field | Go type | Contract |
| --- | --- | --- |
| `name` | `string` | Required, trimmed, case-insensitively unique. |
| `baseUrl` | `string` | Required HTTP(S) URL, normalized without a trailing slash. |
| `apiKey` | `string` | Required and never included in logs or formatted errors. |
| `modelName` | `string` | Required and trimmed. |
| `maxTokens` | `int` | Positive visible-output limit. |
| `maxCompletionTokens` | `*int` | Optional provider capability; mutually exclusive with `max_tokens` on the wire. |
| `temperature` | `float64` | Value is active only when `temperatureConfigured` is true or legacy storage contains a non-zero value. Explicit zero must round-trip. |
| `topP`, `presencePenalty`, `frequencyPenalty` | `*float64` | Optional values preserving explicit zero. |
| `topK`, `reasoningBudget` | `*int` | Optional values constrained by the selected capability profile. |
| `seed` | `*int64` | Optional deterministic seed where supported. |
| `stopSequences` | `[]string` | JSON-serialized; empty entries are invalid. |
| `responseFormat` | `string` | `text` or a capability-supported structured format. |
| `reasoningMode` | `string` | `off`, `auto`, or `on`, limited by the provider profile. |
| `reasoningEffort` | `string` | Provider-owned enum returned by the capability descriptor. |
| `timeOut` | `int` | Positive seconds. |
| `httpProxyEnabled`, `httpProxy` | `bool`, `string` | Enabled proxy requires a valid HTTP(S) URL. |
| `thinking` | `bool` | Legacy compatibility projection of `reasoningMode != off`; not an independent source of truth after normalization. |
| `isDefault` | `bool` | Persisted global-default marker. Exactly one row is default whenever at least one AI configuration exists. |

Provider detection must be shared by capabilities, validation, direct request
construction, and Agent construction. Unknown remote OpenAI-compatible hosts
remain conservative even when their model name resembles an OpenAI model.
Recognized OpenAI reasoning model names may select the OpenAI profile only on
official OpenAI or loopback endpoints.

Successful create, update, copy, and delete operations must reload runtime
settings and emit `aiConfigsChanged`. The manager reloads from the backend
source of truth after success. Other selectors preserve their current ID when
it still exists and otherwise choose a safe first-item or null fallback.

Model resolution is shared across direct OpenAI calls, Agent construction,
Feishu, cron natural-language parsing, and any feature that does not own an
explicit model choice:

1. a positive explicit ID that still exists;
2. the persisted `isDefault=true` configuration;
3. the lowest stable ID as a legacy-data compatibility fallback.

The first created configuration becomes default. Creating a later
configuration does not change the default, copying never copies the default
flag, and deleting the default promotes the lowest remaining ID. Updating a
configuration through the normal edit RPC cannot change `isDefault`; default
switching is exclusively owned by `SetDefaultAIConfig`.

`SetDefaultAIConfig` must clear the previous marker and set the target marker
inside one database transaction, then reload runtime settings and emit
`aiConfigsChanged`. The model manager displays a `默认` tag for the selected
row and a `设为默认` action for every other row. Business UIs that rely on the
global default, including the cron AI helper, must not render another model
selector.

Startup migration is synchronous in `db.Init`. It adds the `is_default`
column before any settings read, repairs multiple defaults by keeping the
lowest marked ID, and assigns the lowest ID when legacy rows have no default.
Do not repair the schema lazily from `GetSettingConfig`; concurrent reads can
otherwise repeatedly fail with `no such column: is_default`.

The session thinking switch is a one-request upper bound:

- `false`: force reasoning off and remove effort/budget from the request copy;
- `true`: preserve the saved mode, effort, and budget exactly;
- neither path mutates the object loaded from settings or writes to SQLite.

Delete checks `Settings.FeishuBotAiConfigId` and cron JSON keys
`aiConfigId`/`ai_config_id` inside the same transaction. References produce a
structured unsuccessful result and leave the row unchanged.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| Create contains a non-zero ID | Reject with a user-safe error. |
| Update/copy/delete uses zero or missing ID | Reject; do not create or delete another row. |
| Required text is empty | Reject before persistence. |
| Base or enabled proxy URL is not HTTP(S) | Reject before network or persistence work. |
| Base URL is not one of the frontend presets | Keep the typed value and validate it as HTTP(S); do not force the user to select a preset. |
| Name already exists ignoring case | Reject create/update; copy chooses the next `-副本N` name. |
| Numeric value is NaN, infinite, or outside capability range | Reject and name the invalid field without exposing secrets. |
| Saved field is unsupported by the selected profile | Reject save until the user explicitly clears or changes it; never silently persist a different semantic. |
| Reasoning mode is `off` with effort or budget | Reject. |
| Gemini has both effort and budget | Reject because the current mapping treats them as mutually exclusive. |
| Claude mode is `on` without budget | Reject; use `auto` for adaptive thinking. |
| Claude budget is greater than or equal to `maxTokens` | Reject. |
| Referenced delete | Return `success=false` with all recognized references; do not delete. |
| Cron JSON is malformed and appears to contain an AI config key | Fail closed with a safe error; do not delete. |
| Unknown remote compatible endpoint requests vendor reasoning | Reject or strip through the conservative profile; never infer from a spoofed model name. |
| Direct request has `maxCompletionTokens` | Send `max_completion_tokens` and omit `max_tokens`. |
| Session thinking is disabled | Strip request reasoning fields and reasoning history content where applicable. |
| `SetDefaultAIConfig` receives zero or a missing ID | Reject without clearing the current default. |
| Legacy database has no `is_default` column | Add it synchronously during database initialization before settings are read. |
| Legacy database has zero or multiple defaults | Keep exactly one stable default: the lowest marked ID, or otherwise the lowest row ID. |
| A copied configuration originates from the default | Persist the copy with `isDefault=false`; keep the original default unchanged. |
| The current default is deleted and rows remain | Promote the lowest remaining ID in the same transaction. |
| A caller passes no explicit model ID | Resolve the persisted default; fall back to the first stable row only for legacy compatibility. |

### 5. Good / Base / Bad Cases

- Good: loopback `http://127.0.0.1:8317/v1` plus `gpt-5.6-sol` exposes OpenAI
  reasoning effort and maps it consistently in direct and Agent requests.
- Good: `https://custom.example/v1` plus `gpt-5-clone` stays on the generic
  profile and does not receive `reasoning_effort`.
- Good: the base URL autocomplete accepts a pasted custom endpoint, debounces
  capability discovery while typing, and reloads immediately on preset select.
- Good: a saved `reasoningMode=off` remains off when the conversation thinking
  switch is on.
- Good: setting the second row as default immediately changes cron AI parsing
  and other zero-ID callers without adding selectors to those business UIs.
- Good: upgrading a database with two accidental default rows keeps only the
  lowest marked row before the first settings query.
- Base: a generic compatible configuration can save `text`, common sampling
  parameters, and no reasoning configuration.
- Bad: a Vue component hard-codes provider ranges or enum values instead of
  rendering `GetAIModelCapabilities`.
- Bad: rendering the base URL as a closed select whose options are treated as
  the complete set of supported endpoints.
- Bad: an Agent path assigns to `AIConfig.Thinking` on a cached settings object.
- Bad: direct chat sends both `max_tokens` and `max_completion_tokens`.
- Bad: a component calls `EventsOff("aiConfigsChanged")` for its private
  subscription; this removes other active components' listeners. Retain and
  invoke the unsubscribe callback returned by `EventsOn`.
- Bad: a zero-ID caller indexes `AiConfigs[0]` directly instead of using the
  shared resolver.
- Bad: an edit payload is allowed to change `isDefault` alongside ordinary
  model fields.

### 6. Tests Required

- Provider table tests: official OpenAI, loopback OpenAI reasoning, spoofed
  remote model name, OpenRouter, Ark, DashScope, Claude, Gemini, DeepSeek, and
  Ollama.
- Validation tests: optional explicit zero, range failures, enum failures,
  unsupported legacy values, Gemini conflict, and Claude budget rules.
- Resolver tests: session off strips reasoning, session on preserves saved
  values including saved `off`, and the source config is unchanged.
- Request tests: common fields are identical for plain/tool requests, token
  limit keys are mutually exclusive, and provider reasoning payloads use the
  declared profile.
- Persistence tests: create/update nullable round-trip, case-insensitive name
  collision, sequential copy naming, missing IDs, referenced deletion, both
  cron key spellings, and malformed possible references.
- Default-model tests: first-create default, explicit/default/legacy resolver
  order, invalid default IDs, concurrent switching, copied-default behavior,
  deletion promotion, and exactly-one-default repair.
- Migration test: a legacy table gains `is_default`; zero/multiple defaults are
  repaired deterministically and the migration is idempotent.
- Runtime tests: direct OpenAI and Agent-style zero-ID paths use the selected
  default while a valid explicit ID remains stable.
- Web binding tests: all six new methods are present in generated bindings and
  the binding-derived RPC allowlist implements them.
- Frontend build: capability-driven controls and generated model types compile.
- Manual frontend assertion: choose a preset, then replace it with a custom
  HTTP(S) URL by typing/pasting; both values remain editable and reach save
  validation unchanged.

### 7. Wrong vs Correct

#### Wrong

```go
aiConfig.Thinking = sessionThinkingEnabled
model := createChatModel(ctx, *aiConfig)
```

This mutates shared settings state and turns the conversation switch into a
persistent model-policy override.

#### Correct

```go
requestConfig := WithSessionThinkingOverride(*aiConfig, sessionThinkingEnabled)
model := createChatModel(ctx, requestConfig)
```

The request gets an isolated copy. Disabling the session strips reasoning;
enabling it preserves exactly what was saved.

#### Wrong

```go
body["max_tokens"] = config.MaxTokens
body["max_completion_tokens"] = *config.MaxCompletionTokens
```

#### Correct

```go
if config.MaxCompletionTokens != nil {
    body["max_completion_tokens"] = *config.MaxCompletionTokens
} else {
    body["max_tokens"] = config.MaxTokens
}
```

#### Wrong

```vue
<n-select v-model:value="config.baseUrl" :options="providerPresets" />
```

This turns convenience presets into a closed provider allowlist.

#### Correct

```vue
<n-auto-complete
  v-model:value="config.baseUrl"
  :options="providerPresets"
  @update:value="debouncedCapabilityReload"
  @select="immediateCapabilityReload"
/>
```

Presets remain discoverable while custom compatible endpoints stay usable.

#### Wrong

```go
config := settings.AiConfigs[0]
```

This silently ignores the persisted global default and gives each call site a
different fallback policy.

#### Correct

```go
config, ok := settings.ResolveAIConfig(explicitID)
```

The shared resolver preserves valid explicit choices and otherwise applies the
persisted-default and legacy-fallback contract consistently.

#### Wrong

```go
db.Model(&AIConfig{}).Where("is_default = ?", true).Update("is_default", false)
db.Model(&AIConfig{}).Where("id = ?", id).Update("is_default", true)
```

Two independent writes can expose zero defaults or leave the database partly
updated after a failure.

#### Correct

```go
db.Transaction(func(tx *gorm.DB) error {
    if err := tx.Model(&AIConfig{}).
        Where("is_default = ?", true).
        Update("is_default", false).Error; err != nil {
        return err
    }
    return tx.Model(&AIConfig{}).
        Where("id = ?", id).
        Update("is_default", true).Error
})
```

Default switching is atomic and is followed by runtime reload plus the
`aiConfigsChanged` event.
