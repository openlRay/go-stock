# Cron Schedule Editor Contract

## Scenario: Common schedules with optional AI-assisted filling

### 1. Scope / Trigger

Use this contract when changing cron task schedule editing, natural-language
schedule parsing, generated Wails/Web bindings for the parser, execution-time
previews, or the scheduler timezone.

The database continues to persist one six-field `CronExpr`; the structured
schedule is an editor and validation boundary, not a new persistence model.

### 2. Signatures

```go
type CronScheduleParseRequest struct {
    Text       string `json:"text"`
    AIConfigID int    `json:"aiConfigId"`
}

type CronScheduleParseResult struct {
    Mode          string `json:"mode"`
    IntervalValue int    `json:"intervalValue,omitempty"`
    IntervalUnit  string `json:"intervalUnit,omitempty"`
    Time          string `json:"time,omitempty"`
    Weekdays      []int  `json:"weekdays,omitempty"`
    MonthDay      int    `json:"monthDay,omitempty"`
    Timezone      string `json:"timezone"`
    Summary       string `json:"summary"`
    CronExpr      string `json:"cronExpr"`
}

func (a *App) ParseCronScheduleText(
    req agent.CronScheduleParseRequest,
) (*agent.CronScheduleParseResult, error)
```

The scheduler and previews use `Asia/Shanghai`; the UI label is
`中国标准时间（北京/上海，UTC+8）`.

### 3. Contracts

- Common manual modes are `interval`, `daily`, `weekly`, and `monthly`.
- Common rules always use six fields in the order `second minute hour day month weekday`, with seconds fixed to `0`.
- Interval units are `minute` (`1..59`) and `hour` (`1..23`).
- Fixed times use 24-hour `HH:mm`; weekdays use integers `0..6`, where `0` is Sunday; month days use `1..31`.
- AI is optional and only fills the editor. It cannot persist the task, change task parameters, or bypass the final user confirmation.
- Model output supplies an intent only. The backend validates every field and deterministically owns `CronExpr`, `Summary`, and `Timezone`; never trust a model-provided Cron string.
- AI parsing uses a request-local AI configuration copy with reasoning disabled and does not mutate saved configuration.
- `AIConfigID=0` means resolve the global default model through the shared AI configuration resolver. The cron UI intentionally sends `0` and does not render a model selector.
- Existing legal expressions that cannot map to a common mode remain unchanged as custom Cron.
- `frontend/wailsjs/go/main/App.js`, `App.d.ts`, and `models.ts` must expose the same RPC and payload shape so the Web binding-derived allowlist also sees the method.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| Empty text | Return a user-safe error before model construction. |
| `AIConfigID=0` and no model configuration exists | Return a safe prompt to configure a default model; keep manual and expert editing available. |
| Positive AI config ID no longer exists | Apply the shared resolver fallback to the persisted default, then the stable legacy first row. |
| Model call, JSON decoding, or unsupported/ambiguous intent fails | Return a safe error without exposing the model response or secrets and do not overwrite the form. |
| Intent field is missing or outside its declared range | Reject it; do not repair it with an invented value. |
| Generated Cron fails the six-field parser | Reject the result. |
| Expert Cron is legal but not representable by a common mode | Preserve it as custom Cron. |
| Expert Cron is illegal | Show the backend validation error and disable final application. |
| AI configs change while the editor is open | Preserve the selected ID when valid; otherwise choose the first valid ID or `null`. |
| Host operating-system timezone differs from China Standard Time | Schedule and preview using `Asia/Shanghai`, not `time.Local`. |

`ValidateCronExpr` currently returns the success string `有效表达式` and errors
beginning with `无效表达式`. Frontend checks must compare the success value
exactly; `includes("有效")` is invalid because `无效表达式` contains the same
substring.

### 5. Good / Base / Bad Cases

- Good: `每天12点整` parses to `daily`, `12:00`, and `0 0 12 * * *`, then waits for the user to confirm.
- Good: `0 30 9 * * 1-5` converts to the weekly common form; a legal expression such as `0 0 20 1,15 * *` remains custom because one monthly day field cannot represent it.
- Base: without any AI configuration, manual and expert editing remain usable.
- Base: the cron AI helper sends `aiConfigId: 0` and immediately follows a newly selected global default without reopening a model picker.
- Bad: the frontend asks the model for a Cron string and saves it directly.
- Bad: the cron dialog keeps a separate model selector or indexes the first configuration itself.
- Bad: switching a common-mode tab leaves a previous custom Cron active or fails to update summary and preview.
- Bad: the UI says UTC+8 while the scheduler uses the host operating-system timezone.

### 6. Tests Required

- Backend intent normalization: minute/hour intervals, daily, deduplicated/sorted weekdays, monthly, invalid units/ranges/times/days/modes, and `Asia/Shanghai` output.
- Default-model resolution: `AIConfigID=0` uses the persisted default, while a database with no configurations returns a safe error without affecting manual editing.
- Scheduler timezone: the app scheduler location and calculated next-run values are `Asia/Shanghai`.
- Binding/Web: `ParseCronScheduleText` exists in generated bindings and `go test -tags web .` confirms the allowlist method is implemented.
- Frontend production build: common fields, AI RPC payload, generated types, and expert controls compile.
- Manual UI: changing each tab updates only its fields, Cron, summary, and five-run preview; AI fills but does not close or save the editor; legal custom Cron round-trips unchanged.

### 7. Wrong vs Correct

#### Wrong

```js
if (validation.includes('有效')) {
  applyCron(cron)
}
```

This accepts `无效表达式：...` as successful validation.

#### Correct

```js
if (validation === '有效表达式') {
  applyCron(cron)
}
```

#### Wrong

```go
result.CronExpr = modelOutput.CronExpr
```

#### Correct

```go
result.CronExpr = fmt.Sprintf("0 %d %d * * *", minute, hour)
_, err := sixFieldParser.Parse(result.CronExpr)
```

The backend generates a deterministic expression from validated structured
fields and validates it again before returning it.
