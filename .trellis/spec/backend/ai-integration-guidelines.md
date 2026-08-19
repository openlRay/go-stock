# AI 集成规范

## 适用范围

新增或修改模型配置解析、provider capability、直接模型请求、Eino Agent、AI tool、结构化输出、流式生成和 AI 后台任务时使用本规范。单个 AI 功能的 prompt、字段和状态机写入对应 task design，不进入本规范。

## 配置与能力的单一来源

- 保存的模型配置由后端负责 normalize、validate 和 provider capability 解析；前端只渲染后端描述，不维护第二套 provider 矩阵。
- 显式模型 ID、全局默认配置和 legacy fallback 使用共享 resolver，调用方不得直接取配置列表第一项。
- provider detection、字段校验、直接 HTTP request builder 和 Agent factory 必须复用同一 capability 语义。
- 自定义 OpenAI-compatible endpoint 是合法边界；不能仅凭 model name 把未知远程 host 识别成官方 provider。
- 可选参数必须保留“未配置”与显式零值差异，不能在 JSON、GORM 和 request builder 之间丢失。

证据：`backend/data/ai_config_service.go`、`backend/data/ai_model_capabilities.go`、`backend/agent/chat_model_factory.go`。

## Request-local 状态

- 会话级 thinking、工具开关、临时 timeout 或功能级参数覆盖必须作用于配置副本，不修改设置缓存或写回 SQLite。
- resolver 应尽量保持纯函数：相同保存配置和会话输入得到相同 effective parameters、warning 和 error。
- 多条模型调用路径复用 common generation/reasoning mapping，避免 plain chat、tool chat 和 Agent 语义漂移。
- request builder 只发送 provider 支持且通过校验的字段；互斥字段不得同时上送。

```go
// 错误：请求级开关污染共享设置。
savedConfig.Thinking = sessionThinking

// 正确：复制后解析本次请求的有效参数。
requestConfig := WithSessionThinkingOverride(savedConfig, sessionThinking)
```

## 模型输出与确定性边界

- 模型输出永远视为不可信外部输入。JSON 能解码不代表字段合法，所有 enum、范围、时间、ID 和必填字段仍需后端校验。
- 模型只提供意图或内容；可确定计算的 Cron、摘要、数据库键和权限结果由后端生成。
- 不完整、含糊或越界输出必须失败，不能猜测补齐后继续持久化。
- 需要结构化输出时使用 typed DTO 和稳定 schema；不要在多个调用方用 map/字符串各自解析同一 payload。
- prompt 与模型文本不能绕过普通业务 validation、引用检查或权限边界。

证据：`backend/agent/cron_schedule_ai.go`、`backend/data/announcement_ai_analysis.go`。

## Context、stream 与取消

- 每次模型调用接收 context；用户取消、替换请求和 shutdown 必须能停止网络与 stream 读取。
- reasoning delta 与 final-answer delta 分开处理。只有产品明确需要的 final content 进入 UI 和持久化。
- latest-request-wins 流程在启动、事件处理和最终持久化前核对 request ID/active state。
- 完整非空 stream 成功且保存成功后才能发 completed；失败、取消或过期请求保留上一份成功数据。
- retry/fallback 是功能级显式决策。付费请求、严格单次分析和含副作用工具不得由通用 helper 隐式重试。
- tool 使用由功能边界决定；不允许模型自行扩大数据源或调用未授权工具。

## 场景：客户端 request ID 与 latest-wins 清理隔离

### 1. Scope / Trigger

前端提供 request ID，后端保存当前 cancel 并在请求结束时清理活动状态的 AI/RPC 流程适用。客户端 request ID 只用于关联和显式取消，不能被当作后端请求实例的唯一身份。

### 2. Signatures

- 活动状态：`{requestID string, generation uint64, cancel context.CancelFunc}`。
- 注册：每个新请求在锁内递增 `generation`，保存本次 `(requestID, generation, cancel)`，再取消上一请求。
- 清理：`clear(requestID, generation)` 只有两个值都与当前活动状态匹配时才清空。
- 显式取消：`abort(requestID)` 只取消当前匹配的 request ID，不接受 generation 作为前端参数。

### 3. Contracts

- request ID 可以被重试、旧页面或异常调用方复用；内部 generation 必须单调区分每个后端请求实例。
- 旧请求的 `defer clear`、失败回调或取消完成不得清除同 ID 新请求的 cancel。
- generation 只存在于进程内，不进入 RPC、事件或持久化契约。
- 关闭页面或发起更新请求时，前端仍使用 request ID 过滤响应；后端使用 ID + generation 保护内部状态。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 新请求使用不同 request ID | 取消旧请求，新 generation 成为活动状态 |
| 新请求复用相同 request ID | 取消旧请求，但旧请求结束时不得清理新 cancel |
| abort 的 request ID 不匹配 | 不取消当前请求，不改变活动状态 |
| 旧请求在新请求之后返回 | 返回取消/过期结果，不产生写入或 completed 副作用 |

### 5. Good / Base / Bad Cases

- Good：两个同 ID 请求并发，第一条被替换后结束；第二条仍可被显式取消。
- Base：不同 ID 请求按 latest-wins 正常替换，活动状态最终清空。
- Bad：清理函数只比较 request ID，导致旧请求 `defer` 清除复用同 ID 的新请求句柄。

### 6. Tests Required

- 用 channel 控制两个同 ID 请求的开始与结束顺序，不使用 `time.Sleep`。
- 断言第一条被取消后，活动 cancel 仍属于第二条请求。
- 调用 `abort(requestID)`，断言第二条收到 `context.Canceled`，最终状态为空。
- 不同 ID、错误 ID abort 和旧请求晚到仍保留原有回归覆盖。

### 7. Wrong vs Correct

```go
// Wrong：同 ID 新请求会被旧请求的 defer 误清理。
defer clear(requestID)

// Correct：内部 generation 区分每个请求实例。
generation := register(requestID, cancel)
defer clear(requestID, generation)
```

## Prompt、数据与安全

- System prompt、用户输入、外部文档和 tool result 标明来源与信任级别；外部内容不能覆盖系统安全约束。
- 固定来源分析必须在后端约束允许的数据源，不能由前端传任意 URL 或 prompt 绕过。
- API Key、完整 prompt、完整模型响应、reasoning 内容和外部文档全文不得进入日志/error。
- 可以记录 request ID、配置 ID、model name、token estimate、阶段、耗时和受限错误分类。
- context window 与 output token limit 是不同概念，不得用一个字段替代另一个。

## 持久化与事件

- AI 生成结果先在内存中累积/校验，再在事务或原子 upsert 中替换旧结果。
- 事件 payload 使用 `backend/models` 中的 typed struct 和稳定 phase/code；Web SSE 与 Wails 共享同一事件语义。
- 前端或其他消费者通过 request ID 和业务 ID 过滤过期事件，不解析可变错误字符串决定状态。
- 保存失败发独立错误状态，不能先发 completed 再尝试 reload 或落库。

## 场景：模型配置草稿可用性测试

### 1. Scope / Trigger

配置页需要在保存前验证 chat 或 embedding 服务是否可用时适用；测试属于一次真实但最小的请求，不是配置持久化流程。

### 2. Signatures

- RPC：`TestAIConfig(config *data.AIConfig) *models.AIConfigTestResult`。
- Service：`AIConfigTestService.Test(ctx context.Context, config *data.AIConfig) *models.AIConfigTestResult`。
- Result：`success`、安全 `message`、`latencyMillis`、可选且受限的 `responsePreview`。

### 3. Contracts

- RPC 接收完整配置副本；不得先创建、更新或读取数据库记录来完成测试。
- chat 复用统一 model factory；embedding 请求标准 `/embeddings`，并校验至少一个非空向量。
- 测试副本关闭 reasoning，继承配置级 proxy、extra headers 和 timeout；context 与 HTTP client 都必须受同一请求上界约束。
- 返回值只包含用户可操作的安全摘要。API Key、reasoning、完整 provider body、带路径的 endpoint 和代理凭据不得进入结果或日志。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 配置为空或字段校验失败 | 不发网络请求，返回安全校验文案 |
| chat 返回空 final content | `success=false`，不回传 reasoning 或原始 response |
| embedding 非 2xx、格式错误或空向量 | 返回分类安全文案，不回传 provider body |
| context 取消或超时 | 分别返回“已取消”或“连接超时”，请求停止 |
| 返回文本包含当前 API Key | preview 脱敏后再截断 |

### 5. Good / Base / Bad Cases

- Good：编辑抽屉把未保存草稿直接交给 dry-run service，测试完成后数据库内容和草稿状态都不变。
- Base：列表对已保存配置组装同样的完整副本，复用同一 RPC。
- Bad：为了测试先保存草稿；直接把 `err.Error()`、响应 body 或 reasoning 展示给前端。

### 6. Tests Required

- fake chat model 覆盖成功、空响应、provider error、取消和超时，并断言源配置未被修改。
- `httptest.Server` 覆盖 embedding method/path/body、extra header、proxy、超时、非 2xx、非法 JSON 和空向量。
- App/RPC 测试断言草稿零写入，结果及日志不包含 API Key、完整 provider body 或代理凭据。
- routine test 不访问真实模型服务。

### 7. Wrong vs Correct

```go
// Wrong：测试行为污染保存配置，并把底层错误原文暴露给 UI。
db.Save(config)
return &Result{Message: err.Error()}

// Correct：复制配置、设置请求级边界，只返回安全摘要。
requestConfig := cloneAIConfigForTest(config)
requestConfig = data.WithSessionThinkingOverride(requestConfig, false)
return tester.Test(ctx, &requestConfig)
```

## 禁止模式

- 直接信任模型返回的 Cron、SQL、URL、ID 或权限结论。
- 在缓存配置对象上修改 request-local thinking/timeout。
- direct request 与 Agent factory 各维护一套 provider 判断。
- stream 中途把 partial content 写成最终成功结果。
- 为兼容 provider 自动重复付费请求却不向调用方暴露请求次数。
- 记录 API Key、完整 prompt、完整 provider body 或 reasoning。

## 测试与验证

- capability/resolver 使用 table test 覆盖官方、loopback、自定义和 spoofed endpoint。
- 断言 request-local override 不修改源配置，互斥字段不会同时上送。
- 结构化输出测试覆盖非法 JSON、字段缺失、enum/range 越界和 deterministic normalization。
- stream transport 使用本地 server 覆盖正常、错误、截断、取消和精确请求次数。
- latest-wins 测试证明旧 request 不能发 completed 或覆盖持久化。
- routine test 不访问真实付费模型。
