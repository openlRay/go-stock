# 策略定时选股推送：技术设计

## 1. 设计目标

新增独立的 `strategy_screening` 定时任务类型，让用户选择一条本地自定义策略和推送数量，按 Cron 计划调用现有自然语言选股能力，并复用统一任务完成通知向本地/Web、飞书和钉钉推送股票列表。

## 2. 现有边界与复用点

- `models.CustomStrategy` 与 `data.CustomStrategyApi` 是自定义策略的数据所有者。
- `data.SearchStockApi.SearchStock` 是自然语言选股的唯一外部 HTTP 入口。
- `agent.CronTaskApi` 负责任务参数校验、类型分派、执行结果和运行信息持久化。
- `App.executeCronTask` 与 `App.pushCronTaskResult` 是调度和立即执行共享的唯一通知边界。
- `cron-task-manager.vue` 已有按任务类型展示参数面板、生成 JSON、编辑回显和强制推送开关模式；本功能沿用 `stock_analysis`、`market_analysis`、`motto_push` 三种既有模式，不新建页面或弹框。

## 3. 任务类型与参数契约

后端新增稳定常量：

```go
const CronTaskTypeStrategyScreening = "strategy_screening"
```

参数使用现有 `CronTask.Params` JSON 字段，不增加数据库列：

```go
type StrategyScreeningTaskParams struct {
    StrategyID uint `json:"strategyId"`
    PushLimit  int  `json:"pushLimit"`
}
```

约束：

- `StrategyID > 0`。
- `PushLimit` 允许 `0`、`10`、`20`、`50`，其中 `0` 表示“全部”；字段缺失按 `20` 处理，其他值拒绝保存和执行。为区分“缺失”和显式 `0`，参数解析先检查原始 JSON 是否包含 `pushLimit`。
- Create/Update 在后端解析并规范化参数，同时强制 `NotifyOnCompletion=true`；前端的禁用开关只提供即时反馈，后端约束负责防绕过。
- 任务只保存策略 ID，不复制名称或 Query。编辑策略后，下次执行自动使用最新条件；策略被删除后任务明确失败。

## 4. 策略读取与执行

### 4.1 策略读取

在 `CustomStrategyApi` 增加 `GetByID(id uint) (*models.CustomStrategy, error)`：

- `id == 0` 或记录不存在返回可识别的 not-found error。
- 数据库错误保留 cause，Cron 边界只暴露安全中文摘要。
- 执行时对 `Name`、`Query` 做 `TrimSpace`；Query 为空视为无效策略。

### 4.2 外部选股调用

`CronTaskApi` 增加窄的可替换 `strategySearch` 依赖，生产默认实现调用：

```go
data.NewSearchStockApi(query).SearchStock(5000)
```

这样生产继续复用现有接口，测试可以注入固定响应，不访问真实东方财富服务或读取真实 `qgqp_b_id`。

执行数据流：

```text
Cron task
  → 解析并校验 Params
  → CustomStrategyApi.GetByID(strategyId)
  → 读取最新 Name / Query
  → SearchStockApi.SearchStock(Query, 5000)
  → 校验 code/data/result/dataList
  → 提取前 pushLimit 条代码与名称
  → 生成 cronTaskContent
  → ExecuteTask 持久化运行结果
  → App.executeCronTask 统一推送
```

### 4.3 响应解析

- 成功响应要求 `code == 100` 且 `data.result.dataList` 为数组。
- 命中总数使用 `len(dataList)`，最多代表本次接口返回的 5000 条；通知文案不声称超过接口上限的精确全市场总数。
- 股票代码读取 `SECURITY_CODE`；名称优先 `SECURITY_SHORT_NAME`，兼容 `SECURITY_NAME_ABBR`。
- 保留上游返回顺序，不在本地重新排序。
- 空 `dataList` 是成功结果，不是异常。
- 缺少 `qgqp_b_id` 映射为明确配置提示；其他外部错误和畸形响应使用安全通用错误，日志不得输出完整响应正文或凭据。

## 5. 通知内容

成功摘要：

- 有命中：固定数量时为 `策略「名称」筛选完成，命中 N 只，推送前 M 只`；“全部”可完整容纳时为 `命中并推送 N 只`，受通知长度限制时为 `命中 N 只，实际展示 M 只`。
- 无命中：`策略「名称」筛选完成，本次没有符合条件的股票`。

Markdown 正文包含：

- 策略名称；
- 规范化后的选股条件；
- 命中总数与本次列出数量；
- `序号 / 股票代码 / 股票名称` 表格。

PlainText 使用等价的逐行列表。策略文本和单元格进行换行规整、Markdown 转义和 rune 截断；格式化时预留标题和说明空间，并只追加能够同时安全容纳于 Markdown/PlainText 长度预算的完整股票行，避免统一截断发生在半行或表格中间。“全部”超出预算时正文明确写出实际展示数量。最终仍受现有 `cronNotificationMaxRunes` 统一上限保护。一次执行只返回一个 `cronTaskContent`，不在 handler 中直接调用任何通知渠道。

## 6. 前端表单

`cron-task-manager.vue` 新增业务参数状态：

```js
const strategyScreeningParamsData = reactive({
  strategyId: null,
  pushLimit: 20
})
```

交互：

- 任务类型选择 `strategy_screening` 时展示一个 `n-card`，包含可筛选的“我的策略”下拉框和 `10/20/50/全部` 推送数量下拉框；“全部”的值为 `0`。
- 复用现有 `GetAllCustomStrategies` RPC；页面初始化及打开新建表单时刷新列表，避免长期驻留页面使用过期选项。
- 没有策略时显示提示并阻止提交；策略选择缺失时显示字段相关提示。
- `generatedParamsJson` 统一生成 `{strategyId, pushLimit}`。
- 编辑任务时解析并回显；旧参数缺少 `pushLimit` 时回退为 20。
- `strategy_screening` 与 `motto_push` 都强制开启并禁用“执行完成后推送”，说明文案根据类型变化。
- 切换到其他任务类型后开关恢复可编辑，不改变其他类型现有参数处理。

不新增 Wails RPC，因此无需重新生成 binding；只在前端复用已经生成的 `GetAllCustomStrategies`。

## 7. 校验与错误矩阵

| 场景 | 创建/更新 | 执行结果 |
| --- | --- | --- |
| 未选择策略 | 拒绝 | 不应产生 |
| `pushLimit` 字段缺失 | 规范化为 20 | 使用 20 |
| `pushLimit == 0` | 保存为“全部” | 尽量展示全部，超长时明确标注实际展示数量 |
| `pushLimit` 非 0/10/20/50 | 拒绝 | 安全失败 |
| 策略被删除 | 任务仍保留 | 失败并提示策略不存在或已删除 |
| 策略 Query 为空 | 拒绝有效执行 | 失败并提示策略条件为空 |
| 未配置 `qgqp_b_id` | 可保存 | 失败并提示先完成设置 |
| 外部接口失败/畸形响应 | 可保存 | 安全失败，不暴露上游正文 |
| 无命中 | 可保存 | 成功，推送无符合条件股票 |
| 命中超过 N | 可保存 | 成功，展示总数并只列前 N |

## 8. 测试与验证

- `backend/agent/cron_task_api_test.go`：参数默认值/非法值、Create/Update 强制通知、策略最新 Query、10/20/50 截断、“全部”完整与超长降级、空结果、删除策略、配置错误和畸形响应。
- `backend/data` 定点测试：`CustomStrategyApi.GetByID` 的成功与不存在语义；若逻辑足够薄，可由 agent 的内存 SQLite 集成覆盖。
- 测试数据库增加 `CustomStrategy` migration；外部搜索使用注入函数，不访问 live API。
- 运行 `go test ./backend/agent`、`go test .`、`npm run build --prefix frontend`、`git diff --check`。
- 仅当本地外部浏览器已经连接且应用可访问时，人工验证新建、编辑、无策略提示、强制推送开关和立即执行；不回退到 in-app Browser 或 `playwright-cli`。

## 9. 兼容性与回滚

- 不增加数据库列，旧任务和旧策略数据不迁移。
- 新类型是 switch 和任务类型列表的追加分支，不改变现有值。
- 删除功能代码后，数据库中该类型任务会成为未知类型并安全失败；回滚前可先停用或删除这些任务。
- 外部选股失败不会绕过 `ExecuteTask` 的运行信息持久化和统一失败通知。
