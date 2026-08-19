# AI 交互式生成选股条件：技术设计

## 1. 设计目标

在现有策略新增/编辑弹框中增加一个可折叠的轻量 AI 助手。助手维护独立建议稿，通过“当前条件 + 本轮调整指令”反复生成新的自然语言选股条件；只有用户明确应用时才更新 `saveForm.query`，最终持久化继续走原有保存接口。

批准的视觉基准见同目录 `design-mockup.png`；实现应保持其“当前条件”和“AI 建议稿”明确分区的层级关系，同时以项目现有响应式和组件规范为准。

## 2. 现有边界与复用依据

- `frontend/src/components/SelectStock.vue` 拥有策略表单、搜索和自定义策略列表。
- `models.CustomStrategy` 与 `data.CustomStrategyApi` 继续拥有策略数据，数据库契约不变。
- `data.SearchStockApi` 继续负责把最终自然语言条件交给东方财富；AI 助手不直接调用选股接口。
- `cron-schedule-editor.vue` 提供“AI 填充但再次确认后使用”的交互依据。
- `my.vue` / `MottoApi.Polish` 提供模型选择、请求级配置副本、只返回草稿不入库的依据。
- 公告 AI 分析提供 request ID、取消和 latest-wins 的生命周期依据。
- 复杂表单弹框应使用 `AppModalShell`，业务字段和 AI 状态留在业务组件。

## 3. 前端组件与布局

### 3.1 策略弹框

把 `SelectStock.vue` 中现有 `n-modal preset="dialog"` 策略表单迁移到 `AppModalShell`，宽度约 `760px`，保留原字段和底部“取消/保存”操作。迁移仅改变稳定弹框骨架，不改变保存语义。

“选股条件”表单项包含：

1. 原有权威 textarea，始终允许手工编辑；
2. 右侧或下方“AI 帮我生成”开关；
3. 展开后的业务组件 `StrategyConditionAssistant.vue`。

### 3.2 轻量助手

`StrategyConditionAssistant.vue` 负责：

- 加载并监听现有 chat 模型配置，默认选择全局默认模型；
- 输入本轮生成/调整指令；
- 首轮提供示例按钮，如“稳健低估值”“短线放量突破”“排除 ST 和创业板”；
- 展示 loading、取消入口、安全错误、建议稿、调整摘要和有限警告；
- 允许用户直接编辑建议稿；
- 建议存在时把主按钮切换为“继续调整”；
- 发出 `apply(query)` 事件，由父组件显式写入 `saveForm.query`；
- 关闭、切换策略或卸载时调用取消 RPC，并清空内存会话。

助手不保存完整聊天列表。下一轮请求的 `currentQuery` 使用当前可编辑建议稿；没有建议时使用父组件传入的正式选股条件。

## 4. RPC 与数据契约

新增业务 DTO：

```go
type StrategyConditionAIRequest struct {
    RequestID    string `json:"requestId"`
    Instruction  string `json:"instruction"`
    CurrentQuery string `json:"currentQuery"`
    AIConfigID   int    `json:"aiConfigId"`
}

type StrategyConditionAIResult struct {
    Query    string   `json:"query"`
    Summary  string   `json:"summary"`
    Warnings []string `json:"warnings"`
}
```

App 暴露：

```go
GenerateStrategyCondition(req StrategyConditionAIRequest) (*StrategyConditionAIResult, error)
AbortStrategyConditionGeneration(requestID string)
```

`RequestID` 由前端为每轮请求生成，只用于活动请求匹配和取消，不进入持久化状态。新增 Wails 方法后同步生成的 `App.js`、`App.d.ts` 和 `models.ts`。

## 5. 后端职责

### 5.1 Agent 服务

新增 `backend/agent/strategy_condition_ai.go`：

- 校验调整指令非空；当前条件允许为空；
- 使用 `ResolveAIConfig` 解析显式或默认 chat 配置；
- 对配置副本应用 `WithSessionThinkingOverride(..., false)`；
- 使用无工具的 chat model，模型不得访问外部行情或自行执行选股；
- system prompt 明确输出固定 JSON：`query`、`summary`、`warnings`；
- user message 使用 JSON 数据承载 `currentQuery` 与 `instruction`，区分系统约束与用户内容；
- 复用 `extractJSONObject` 后解码 typed output；
- 规范化并校验所有字段后返回，不写数据库。

建议校验边界：

- `instruction`：trim 后非空，最多 2000 rune；
- `currentQuery`：最多 4000 rune；
- 结果 `query`：trim 后非空，最多 2000 rune；
- `summary`：最多 300 rune；
- `warnings`：最多 5 条，每条最多 200 rune，空项去除。

查询建议保留可读的中文自然语言与分号结构，不输出 Markdown、代码块、标题或解释前缀。模型无法判断的条件写入 `warnings`，不能擅自声称已验证行情数据。

### 5.2 App 请求生命周期

在 `App` 增加独立的 mutex、cancel 和活动 request ID：

- 新请求注册时取消上一条策略条件请求，实现 latest-wins；
- `AbortStrategyConditionGeneration` 只取消匹配的活动请求；
- 每次注册同时分配内部单调 generation；方法结束时同时匹配 request ID 和 generation，避免复用同一 request ID 时旧请求清除新请求；
- app context 缺失时使用 `context.Background()`；
- `context.Canceled` 返回稳定的取消语义，其他底层错误记录安全摘要并返回可操作中文提示。

该状态不与公告分析、Agent 聊天或格言润色共享，避免不同功能互相取消。

## 6. 数据流

```text
正式 saveForm.query ─┐
                    ├─> 当前上下文 + 本轮指令 + AI 配置
最新 AI 建议稿 ─────┘              │
                                   ▼
前端生成 requestId → GenerateStrategyCondition
                                   │
                                   ▼
App 注册活动请求并取消旧请求
                                   │
                                   ▼
Agent 解析配置 → 调用无工具模型 → JSON 解码/校验
                                   │
                                   ▼
前端独立建议稿（不改 saveForm.query）
        │                 │
        │继续调整         │明确应用
        └────下一轮───────┘        ▼
                              saveForm.query
                                   │
                                   ▼
                         原有 SaveCustomStrategy
```

## 7. 状态与错误矩阵

| 场景 | 助手行为 | 正式选股条件 |
| --- | --- | --- |
| 首次生成成功 | 展示独立建议、摘要和警告 | 不变 |
| 继续调整成功 | 用新建议替换旧建议 | 不变 |
| 用户直接编辑建议 | 编辑内存建议，下一轮基于编辑值 | 不变 |
| 点击应用 | 建议写入表单并提示尚未保存 | 明确更新 |
| 放弃建议 | 清空建议和指令 | 不变 |
| 请求失败/空结果/非法 JSON/超长 | 保留上一份成功建议并显示安全错误 | 不变 |
| 关闭弹框/切换策略 | 取消活动请求并清空助手状态 | 保留对应表单初始化值 |
| 旧请求晚到 | request ID 不匹配，忽略结果 | 不变 |
| 无 AI 配置 | 显示设置提示，禁用生成按钮 | 仍可手工编辑 |

## 8. 兼容性与回滚

- 不增加数据库列，不迁移历史策略，不改变 `CustomStrategy` JSON 或保存 API。
- 新增 RPC 和前端业务组件是追加能力；移除这些文件和入口即可回滚。
- `SelectStock.vue` 的弹框骨架迁移需要保持关闭、校验、保存和重新加载列表行为一致。
- AI 生成内容只有在用户应用并保存后才影响后续手工或定时选股。

## 9. 验证设计

- Agent 单元测试使用 fake model，覆盖成功生成、继续调整 payload、默认/指定配置、非法 JSON、空 query、长度边界、模型失败、取消和源配置不被修改。
- App 测试覆盖匹配取消、错误 request ID 不取消、后发请求取消前一请求、旧请求不能清理新状态。
- 前端无现成单元测试框架，使用构建、差异检查和必要的本地外部浏览器交互验收。
- 浏览器验收检查：新增/编辑策略、空条件生成、基于现有条件优化、连续两轮调整、编辑建议、应用/放弃、关闭取消、无配置提示和保存后重新打开。
