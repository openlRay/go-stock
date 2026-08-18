# 策略定时选股推送：实施计划

## 1. 后端参数与策略读取

- [x] 在 `backend/agent/cron_strategy_screening.go` 增加 `CronTaskTypeStrategyScreening`、`StrategyScreeningTaskParams`、默认/允许推送数量常量和统一参数解析校验。
- [x] 在 Create/Update 边界规范化策略任务参数并强制 `NotifyOnCompletion=true`，保持其他任务类型行为不变。
- [x] 在 `backend/data/custom_strategy_api.go` 增加按 ID 读取能力，正确区分不存在与数据库错误。
- [x] 将新类型追加到 `GetTaskTypes` 和执行 switch。

## 2. 策略执行与通知正文

- [x] 给 `CronTaskApi` 注入可替换的策略搜索函数，生产默认复用 `SearchStockApi.SearchStock(5000)`。
- [x] 实现策略最新内容读取、外部响应校验、股票代码/名称安全提取和错误映射。
- [x] 实现有命中、无命中、固定数量截断和“全部”长度降级的 Markdown/PlainText 内容，保留上游顺序并明确实际展示数量。
- [x] 确认 handler 只返回 `cronTaskContent`，通知仍由 `App.executeCronTask` 唯一分发。

## 3. 前端 Cron 表单

- [x] 复用 `GetAllCustomStrategies` 加载策略选项，增加刷新、空状态和错误反馈。
- [x] 新增 `strategy_screening` 参数面板：策略下拉和 10/20/50/全部推送数量下拉，默认 20，全部使用值 `0`。
- [x] 扩展 `generatedParamsJson`、编辑回显、reset 和提交前校验。
- [x] 将策略任务纳入强制推送开关和说明文案，保证切换其他任务类型后恢复可编辑。

## 4. 后端定点测试

- [x] 扩展内存 SQLite migration，包含 `CustomStrategy`。
- [x] 覆盖参数默认值、非法策略 ID、非法 pushLimit、Create/Update 强制通知。
- [x] 注入搜索响应，覆盖最新 Query、有命中、无命中、10/20/50 截断、“全部”完整/超长降级和上游顺序。
- [x] 覆盖策略删除、Query 为空、缺少配置提示、外部失败和畸形响应，确保通知错误安全且运行次数正常更新。

## 5. 验证顺序

```bash
gofmt -w backend/agent/cron_task_api.go backend/agent/cron_strategy_screening.go backend/agent/cron_task_api_test.go backend/agent/cron_strategy_screening_test.go backend/data/custom_strategy_api.go
go test ./backend/agent
go test .
npm run build --prefix frontend
git diff --check
```

- [x] 搜索新任务类型的所有 switch/条件分支，确认 Create、Update、GetTaskTypes、Execute、前端参数和强制通知均已覆盖。
- [x] 检查 Wails binding 无需更新：前端只调用既有 `GetAllCustomStrategies`。
- [x] 检查本任务 diff 仅包含子任务文档及产品改动，不纳入父任务已暂存文档、`moneyTrend.vue` 或 `stock.vue`。
- [ ] 浏览器验收未执行：当前会话没有已连接的本地外部浏览器，不回退到 in-app Browser 或 `playwright-cli`。

验证结果：

- `go test ./backend/agent`：通过。
- `go test . -run 'TestExecuteCronTaskStrategyScreeningUsesUnifiedNotificationBoundary|TestExecuteCronTaskNotificationBoundary' -count=1`：通过，覆盖真实 App 完成通知边界。
- `npm run build --prefix frontend`：通过，仅输出既有 chunk-size warning。
- `git diff --check`、`git diff --cached --check` 及两个新增 Go 文件的 no-index whitespace 检查：通过。
- `go test .`：未通过；既有 `TestSummaryStockNews` 在 `app_test.go:113` 依赖实时外部 AI 返回并发生 `interface conversion: interface {} is nil, not string`，与本任务改动无关。
- 最终审阅补充了 Markdown 最坏转义长度回归、策略失败的统一通知链路测试，并将可复用的最终载荷预算规则同步到后端质量规范。

## 6. 风险与回滚点

- [x] 若外部响应结构与预期不一致，优先收紧响应适配测试，不在通知层散落 map cast。
- [x] 若前端表单切换造成参数串扰，回滚新增面板分支并保持后端任务类型不可创建，避免保存半有效任务。
- [x] 若策略任务推送失败，停用该任务类型即可，不回滚现有通用通知链路。
