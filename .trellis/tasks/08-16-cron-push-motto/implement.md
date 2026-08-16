# 定时任务推送与我的格言：实施计划

## 1. 后端模型与迁移

- [x] 在 `backend/models` 增加 `Motto`、固定表名和 JSON/GORM 字段。
- [x] 在 `CronTask` 增加 `notifyOnCompletion` additive 字段。
- [x] 在 `main.AutoMigrate` 注册 `Motto`，确认 CronTask 新列由既有 migration 覆盖。
- [x] 增加 migration/round-trip 定点测试，证明旧任务默认关闭、`false` 可写回。

## 2. 格言服务与 AI 润色

- [x] 新增 `backend/agent/motto_api.go`，实现正文 normalize/validate、CRUD、稳定列表和随机最多三条。
- [x] 实现 `MottoPolishRequest/Result`、AI 配置解析、request-local thinking override、纯文本 prompt 和响应校验。
- [x] 增加内存 SQLite CRUD、空值/超长、不存在 ID、随机唯一/不足三条/空表测试。
- [x] 使用 fake `ToolCallingChatModel` 覆盖润色成功、空响应和模型错误，不访问真实服务。
- [x] 新增 `app_motto.go` RPC 薄包装。

## 3. Cron 结果与推送链路

- [x] 为任务执行定义统一结果结构，将 `executeTaskByType` 各分支改为返回摘要/详情/error。
- [x] 为股票分析、市场分析、指数缓存、异动保存等现有分支返回有意义且有长度上限的结果。
- [x] 增加 `motto_push` 任务类型，随机格式化最多三条；空库返回安全错误。
- [x] Create/Update 边界对 `motto_push` 强制 `NotifyOnCompletion=true`，Update map 显式写布尔值。
- [x] 新增 `app_cron_notification.go`，集中 `executeCronTask` 和全局已启用渠道分发。
- [x] 将 Init/Create/Update/Enable/ExecuteNow 的闭包全部切换到统一入口。
- [x] 增加 Cron 定点测试：普通旧任务不开通知、格言任务强制开关、成功/失败结果持久化、随机格言结果和通知格式。

## 4. 前端“我的/格言”

- [x] 在 router 增加 `/my` 动态路由。
- [x] 在 `App.vue` 的“关于”之后增加“我的”顶级菜单和图标。
- [x] 新增 `my.vue`，以 `n-tabs` 提供“格言”功能容器。
- [x] 使用 `AppModalShell` 实现新增/编辑格言；实现列表、空状态、删除确认、loading 和错误反馈。
- [x] 接入 AI 配置列表和 `PolishMotto`，确保润色只更新草稿，取消/失败不写库。

## 5. 前端 Cron 表单

- [x] 增加 `notifyOnCompletion` 表单字段、列表列、编辑回显和 reset 默认值。
- [x] 增加“推送格言”类型专属提示；选择该类型时自动开启并锁定结果推送。
- [x] 修复无专属参数任务的 `generatedParamsJson` fallback，避免提交 `undefined`。
- [x] 修复“执行时间设置”子弹框被任务编辑父弹框覆盖的层级问题，并验证关闭后父弹框状态保留。

## 6. Cron 管理回归修复

- [x] 将任务类型/状态筛选的未选择值改为 `null`，显示“全部任务类型”“全部状态”，查询时转换为空字符串。
- [x] 定义 typed `cronTaskExecuted` 事件并在统一 `executeCronTask` 完成持久化后发送。
- [x] Cron 页面订阅完成事件并刷新当前列表，卸载时只注销当前 callback；立即执行入口不做过早刷新。
- [x] 修复“执行完成后推送”开关和说明的业务布局，保持内容区宽度与左对齐。

## 7. AI 模型可用性测试

- [x] 新增安全的模型测试 DTO/service/RPC，输入完整配置副本且不写数据库。
- [x] 对话配置复用统一 chat model factory 发起最小生成测试；向量配置复用 embeddings 语义并校验非空向量。
- [x] 使用 fake model/`httptest.Server` 覆盖成功、失败、超时、类型分派、额外 Header 和安全错误，不访问真实模型。
- [x] AI 配置列表增加行级“测试”按钮，编辑抽屉 footer 增加“测试当前配置”按钮，分别维护独立 loading。
- [x] 重新生成 Wails v2.11 binding，并更新 Web RPC allowlist 定点测试。

## 8. Binding 与静态一致性

- [x] 使用 `go run github.com/wailsapp/wails/v2/cmd/wails@v2.11.0 generate module` 重新生成 Wails binding。
- [x] 检查 `App.js`、`App.d.ts`、`models.ts` 包含所有 Motto RPC/type 和 `CronTask.notifyOnCompletion`。
- [x] 检查 Web RPC allowlist 可从生成 binding 发现新方法，且未修改 desktop-only allowlist。

## 9. 验证顺序

按修改风险运行最小相关证据集：

```bash
gofmt -w <本任务修改的 Go 文件>
go test ./backend/agent
go test .
go test -tags web .
npm run build --prefix frontend
git diff --check
```

必要的定点断言：

- [x] CRUD 数据重启语义由 SQLite round-trip 测试覆盖。
- [x] 随机查询单次不重复，1/2/3+ 条分别返回 1/2/3 条，0 条转为任务失败。
- [x] 普通任务开关关闭不分发；开启后成功和失败各只构造一次完成通知。
- [x] 通知失败不覆盖任务执行结果（通过分发边界不返回到业务 error 的测试/结构断言覆盖）。
- [x] Web tag 测试覆盖新 RPC allowlist 和反射调用。
- [x] 前端 build 覆盖动态路由、图标、组件 import 和生成 binding 类型。
- [x] Cron 完成事件测试证明执行数据先持久化再发事件，前端只在完成后刷新。
- [x] AI 模型测试证明 chat/embedding 分派正确、草稿零写入且错误信息不泄露 secret/provider body。

浏览器验收仅在本地外部浏览器已连接时执行；至少检查“我的 → 格言”CRUD/润色草稿、Cron 推送开关和嵌套执行时间弹框层级。若浏览器未连接，如实报告阻塞，不回退到 in-app Browser 或 `playwright-cli`。

## 10. 审查与回滚点

- [x] 完成实现后加载 `trellis-check`，按 spec、跨层数据流、测试和生成 binding 做质量检查。
- [ ] 若 Cron 重构失败，优先回滚统一结果/通知接线，同时保留独立 Motto CRUD；不得用复制五份通知逻辑临时绕过。
- [ ] 提交前只暂存本任务文件，确认无无关 WIP。
