# AI 交互式生成选股条件：实施计划

## 1. 后端生成服务

- [x] 新增 `backend/agent/strategy_condition_ai.go`，定义 request/result/output DTO、输入校验、模型调用、JSON 解码和结果规范化。
- [x] 复用统一 AI 配置 resolver、request-local thinking override、无工具 chat model 和安全错误语义。
- [x] 为服务增加 fake model 注入点，避免测试访问真实付费模型。
- [x] 新增 `backend/agent/strategy_condition_ai_test.go`，覆盖成功、迭代上下文、配置、非法/空/超长输出、模型错误、取消和配置不变性。

## 2. App RPC 与取消

- [x] 在 `App` 增加策略条件 AI 专用 mutex、cancel、request ID 和可测试生成函数入口。
- [x] 新增 `app_strategy_condition_ai.go`，实现 `GenerateStrategyCondition` 与 `AbortStrategyConditionGeneration`。
- [x] 新增 `app_strategy_condition_ai_test.go`，覆盖 matching abort、non-matching abort、latest-wins、相同 request ID 的 generation 隔离和安全清理。
- [x] 同步 Wails 生成绑定：`frontend/wailsjs/go/main/App.js`、`App.d.ts`、`models.ts`。

## 3. 前端交互组件

- [x] 新增 `frontend/src/components/StrategyConditionAssistant.vue`。
- [x] 加载 chat AI 配置并监听 `aiConfigsChanged`；设置默认模型和无配置提示。
- [x] 实现折叠说明、示例、指令输入、生成/继续调整、loading/取消、建议编辑、摘要/警告、应用和放弃。
- [x] 使用 request ID gate；关闭、切换或卸载时取消活动请求并清空会话状态。

## 4. 策略弹框集成

- [x] 将策略表单迁移到 `AppModalShell`，保留新增/编辑初始化、字段校验、取消和保存行为。
- [x] 在选股条件区域集成助手；`apply` 只更新 `saveForm.query` 并提示“尚未保存”。
- [x] 增加保存 loading/重复提交保护，确保 AI 生成状态不会触发自动保存或搜索。
- [x] 核对热门策略、我的策略、手工搜索、编辑、删除和定时选股依赖的 `Query` 契约未变化。

## 5. 验证与质量门禁

- [x] 对修改的 Go 文件运行 `gofmt`。
- [x] 运行 `go test ./backend/agent`。
- [x] 运行根 package 定点测试；策略条件相关测试及 race 测试通过。全量 `go test .` 已尝试，但被既有 `TestSummaryStockNews` 的真实外部模型响应异常阻塞。
- [x] 运行 `npm run build --prefix frontend`。
- [x] 运行 Web binding 定点测试，确认新增 RPC 已进入反射绑定。
- [x] 运行 `git diff --check` 并人工检查生成绑定与 DTO 字段一致。
- [x] 复用本地外部 Chrome 完成弹框、折叠助手、模型选择、示例填充、状态清理和取消不保存等交互验收；未调用真实付费模型，生成链路由 fake model 单测覆盖。

## 6. 风险与回滚点

- [x] 完成后端和 RPC 测试后再接入前端，避免 UI 建立在不稳定契约上。
- [x] 弹框骨架迁移后先验证原策略 CRUD，再验证 AI 助手，便于定位回归。
- [x] 若 AI 交互出现问题，可移除助手入口和新增 RPC；数据库与既有策略无需回滚。
