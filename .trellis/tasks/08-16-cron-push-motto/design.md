# 定时任务推送与我的格言：技术设计

## 1. 设计目标

在不改变现有 Cron 编辑器和全局通知设置的前提下，增加可持久化的格言库、AI 润色、任务完成通知开关，以及“推送格言”任务类型。实现必须同时适配 Desktop Wails 与 Web RPC/SSE，并让调度执行和立即执行共享同一结果/通知路径。

## 2. 现有边界与复用点

- `backend/models.CronTask` 是任务跨层 DTO 和 GORM model。
- `backend/agent.CronTaskApi` 拥有任务 CRUD、任务类型分派、运行结果更新和 Cron 时间计算。
- `App.InitCronTasks`、`CreateCronTask`、`UpdateCronTask`、`EnableCronTask`、`ExecuteCronTaskNow` 是所有任务触发入口。
- `frontend/src/components/cron-task-manager.vue` 已拥有任务表单、任务类型参数面板和立即执行入口。
- `backend/agent/createChatModel`、`data.ResolveAIConfig`、`data.WithSessionThinkingOverride` 是现有直接 AI 请求的统一模型选择链路。
- `data.NewAlertWindowsApi`、`data.NewFeishuAPI`、`data.NewDingDingAPI` 和 `App.emit("newsPush")` 是当前通知能力；全局开关位于 `data.SettingConfig`。
- `frontend/src/components/common/AppModalShell.vue` 是表单弹框的应用级稳定骨架。
- `frontend/src/router/router.js` 与 `frontend/src/App.vue` 分别拥有动态路由和顶级导航。

## 3. 数据模型与迁移

### 3.1 CronTask additive 字段

在 `backend/models.CronTask` 增加：

```go
NotifyOnCompletion bool `json:"notifyOnCompletion" gorm:"column:notify_on_completion;not null;default:false"`
```

- 旧任务经 `AutoMigrate` 后为 `false`，不会产生新增通知。
- `CronTaskApi.Update` 必须用显式字段 map 更新该布尔值，确保 `false` 能写回。
- 任务类型为 `motto_push` 时，后端在 Create/Update 边界强制设为 `true`；前端同时自动打开并锁定开关，后端约束负责防止绕过 UI。

### 3.2 Motto 表

在 `backend/models` 增加：

```go
type Motto struct {
    ID        uint      `json:"id" gorm:"primarykey"`
    CreatedAt time.Time `json:"createdAt"`
    UpdatedAt time.Time `json:"updatedAt"`
    Content   string    `json:"content" gorm:"type:text;not null"`
}
```

表名固定为 `mottos`。迁移使用 additive `AutoMigrate(&models.Motto{})`，不修改或导入 `App.vue` 现有的内置投资格言数组；该数组仍只服务窗口标题，避免本期扩大兼容范围。

## 4. 后端服务设计

### 4.1 MottoApi

在 `backend/agent/motto_api.go` 增加 `MottoApi`，原因是该能力既需要 SQLite CRUD，又需要复用同 package 的 `createChatModel`。

公开能力：

```go
List() ([]models.Motto, error)
Create(content string) (*models.Motto, error)
Update(id uint, content string) (*models.Motto, error)
Delete(id uint) error
Random(limit int) ([]models.Motto, error)
Polish(ctx context.Context, req MottoPolishRequest) (*MottoPolishResult, error)
```

契约：

- 正文统一 `TrimSpace`，空正文拒绝；正文长度限制为 2000 个 rune。
- 列表按 `updated_at DESC, id DESC` 稳定排序。
- Update/Delete 对不存在 ID 返回明确错误。
- `Random(3)` 使用 SQLite `ORDER BY RANDOM() LIMIT 3`；结果天然不重复，记录少于三条时返回全部，空表返回空 slice，由 Cron 任务转换为业务错误。
- 数据库 error 保留 cause，在 App/RPC 边界返回安全中文错误。

### 4.2 AI 润色

请求/响应：

```go
type MottoPolishRequest struct {
    Content    string `json:"content"`
    AIConfigID int    `json:"aiConfigId"`
}

type MottoPolishResult struct {
    Content string `json:"content"`
}
```

数据流：

```text
前端草稿
  → App.PolishMotto
  → MottoApi 输入校验
  → ResolveAIConfig
  → request-local 关闭 thinking
  → createChatModel.Generate
  → 规范化纯文本结果
  → 返回前端草稿（不写数据库）
```

- System prompt 只允许润色当前原文，要求保留核心含义、输出单段纯文本，不调用工具、不返回 Markdown 或解释。
- 选中的配置不存在、没有可用 chat 模型、模型创建失败、请求失败、空响应或超长响应均返回安全错误。
- AI 成功只更新前端编辑草稿；只有用户随后点击保存才写入数据库。
- 测试通过可注入 model factory/fake model，不访问真实付费模型。

### 4.3 聚焦的 App RPC

新增根目录 `app_motto.go`，只做边界编排：

```go
GetMottos() ([]models.Motto, error)
CreateMotto(content string) (*models.Motto, error)
UpdateMotto(id uint, content string) (*models.Motto, error)
DeleteMotto(id uint) error
PolishMotto(req agent.MottoPolishRequest) (*agent.MottoPolishResult, error)
```

新增/修改方法后使用 Wails v2.11 重新生成 `App.js`、`App.d.ts` 和 `models.ts`。Web RPC allowlist 从生成 binding 自动派生，不手工放宽 allowlist。

## 5. Cron 执行结果与通知

### 5.1 统一执行结果

将任务类型执行从“只返回 error”收敛为内部结果：

```go
type CronTaskExecutionResult struct {
    Success       bool
    Summary       string
    Markdown      string
    PlainText     string
    CompletedAt   time.Time
    LastRunResult string
}
```

- `CronTaskApi.ExecuteTask` 仍负责执行任务、计算下次时间和更新 `LastRunAt/NextRunAt/RunCount/LastRunResult`，并把结果返回给 App 层。
- `LastRunResult` 只保存适合列表展示的短摘要，按 model 字段上限截断；通知可使用更完整但有安全长度上限的 Markdown/纯文本。
- 现有任务类型返回可用的实际摘要：股票/市场分析可返回截断后的分析内容，缓存/异动保存返回数量或完成说明，失败返回安全错误。
- 通知发送发生在业务执行和运行信息更新之后；通知发送结果不改变任务业务成功/失败，也不重复增加运行次数。

### 5.2 App 层唯一执行入口

新增 `app_cron_notification.go` 中的：

```go
func (a *App) executeCronTask(task *models.CronTask) error
```

所有调度和立即执行闭包统一调用它：

```text
Scheduler / ExecuteNow
  → App.executeCronTask
  → CronTaskApi.ExecuteTask
  → 更新运行信息
  → 若 NotifyOnCompletion=true，分发配置内已启用渠道
```

这样不会在 `InitCronTasks`、Create、Update、Enable 和 ExecuteNow 五个入口分别复制通知判断。

### 5.3 通知分发规则

`App.pushCronTaskResult` 根据全局设置分发：

- `LocalPushEnable`：系统/浏览器通知，并发出 `newsPush` 事件。
- `FeishuPushEnable`：飞书卡片。
- `DingPushEnable`：钉钉 Markdown。
- 未启用的渠道不调用；没有任何渠道启用时静默跳过并记录 debug/info 日志。
- 每个渠道独立异步发送，外部失败只记录受限日志，不回写任务状态。

成功标题为“定时任务成功：任务名”，失败标题为“定时任务失败：任务名”；正文包含状态、完成时间、短摘要和可用详情，不记录 API Key、完整 prompt 或 reasoning。

## 6. “推送格言”任务

任务类型常量为 `motto_push`，显示名为“推送格言”。无额外 JSON 参数。

执行规则：

1. 调用 `MottoApi.Random(3)`。
2. 空表返回“格言库为空，请先在我的 → 格言中添加格言”的业务错误。
3. 将最多三条格言编号格式化为一份 Markdown/纯文本结果。
4. 结果交回统一 Cron 完成通知链路；不在任务 handler 内直接调用飞书、钉钉或本地通知。

由于后端强制 `NotifyOnCompletion=true`，该任务不会出现“执行成功但不推送”的无效配置。

## 7. 前端设计

### 7.1 “我的”页面

- 新增 `/my` 动态路由，名称 `my`。
- 在“关于”之后新增顶级“我的”导航，菜单 key 与 route name 都为 `my`。
- 新增 `frontend/src/components/my.vue`，使用 `n-tabs` 作为可扩展容器，首个 `n-tab-pane` 为“格言”。

### 7.2 格言 CRUD UI

- 页面顶部提供“新增格言”。
- 使用 `n-data-table` 展示正文、创建时间、更新时间和操作。
- 新增/编辑复用 `AppModalShell`，正文使用 textarea；关闭或取消不会写库。
- 删除使用 `n-popconfirm`。
- 保存、删除后重新加载列表；loading、saving、polishing 分离，避免重复请求。
- AI 配置来自 `GetAiConfigs()`，默认选中首个可用 chat 配置；“AI 润色”按钮只调用 `PolishMotto` 并把返回内容写入当前草稿。

### 7.3 Cron UI

- 表单增加“执行完成后推送”开关，默认关闭。
- 编辑时回显 `notifyOnCompletion`，列表增加“结果推送”列。
- 选择 `motto_push` 时自动开启并禁用开关，显示说明；切换到其他类型后恢复可编辑，但保留当前开关值供用户决定。
- `generatedParamsJson` 对无专属参数的任务返回当前 `params` 或空字符串，避免写入 `undefined`。

### 7.4 Cron 列表完成刷新与筛选语义

- `filterTaskType`、`filterStatus` 使用 `null` 表示未选择，placeholder 分别显示“全部任务类型”“全部状态”；RPC 查询边界统一转换为空字符串，保持后端现有筛选契约。
- `executeCronTask` 在任务结果和运行信息持久化完成后发出 `cronTaskExecuted` 事件，payload 使用 `backend/models` 中的 typed struct，至少包含任务 ID、成功状态和完成时间。
- `cron-task-manager.vue` 订阅该事件并刷新当前分页/筛选条件下的列表；组件卸载时只注销自己的 callback。
- 表单的通知设置使用宽度 `100%`、左对齐的纵向业务容器，避免 `n-space` 默认收缩/对齐影响说明文字布局。

## 8. AI 模型可用性测试

### 8.1 RPC 契约

新增聚焦的测试结果 DTO 与 `App.TestAIConfig(config *data.AIConfig)` RPC。输入是完整配置副本，列表入口传已保存配置，编辑抽屉传 `buildSavePayload()` 生成的未保存草稿；RPC 只校验和执行测试，不创建或更新数据库记录。

返回结构至少包含：

```go
type AIConfigTestResult struct {
    Success         bool   `json:"success"`
    Message         string `json:"message"`
    LatencyMillis   int64  `json:"latencyMillis"`
    ResponsePreview string `json:"responsePreview,omitempty"`
}
```

错误通过安全中文消息返回，结果和日志不包含 secret、完整 provider body、完整 prompt 或 reasoning。

### 8.2 对话与向量测试路径

- 对话配置复用 `backend/agent/createChatModel` 及统一 capability/request builder，发送固定的最小测试消息，并只截取 final content 的短预览。
- 向量配置复用现有 OpenAI-compatible embeddings 配置语义，向量化固定短文本并验证返回维度非零。
- 两条路径都使用 request-local 配置副本，遵循配置内 timeout、proxy 和 extra headers，不修改保存配置或全局设置。
- 生产方法通过窄的可替换 factory/runner 测试；常规测试使用 fake model 或 `httptest.Server`，断言路径、请求次数、安全错误和零数据库写入。

### 8.3 前端交互

- 列表操作列增加“测试”按钮，并用独立的 `testingConfigId` 维护行级 loading，避免与复制、删除、设默认互相污染。
- 编辑抽屉 footer 增加“测试当前配置”按钮，使用独立 `testingDraft` loading；测试前同步自定义 Header、停止序列和当前草稿字段。
- 成功消息展示配置类型、耗时和可选短预览；失败消息只展示后端安全文案。
- 测试按钮不关闭抽屉、不刷新配置列表、不改变默认模型，也不保存草稿。

### 8.4 Docker Web 动态路由资源

- `main.go` 使用 `//go:embed all:frontend/dist`，确保 Vite 生成的 `_commonjsHelpers-*` 等下划线 chunk 被编译进 Web 二进制。
- `serveSPA` 对存在的哈希资源设置 immutable cache；对 index/history fallback 设置 no-store。
- `/assets/*` 缺失时直接返回 404，不允许 SPA fallback 用 `index.html` 伪装静态资源成功。
- Web-tag 回归测试同时验证嵌入文件集和 HTTP handler 契约；Linux Web build 对应 Docker 的最终编译形态。

## 9. 兼容性、风险与回滚

### 9.1 兼容性

- 旧任务字段默认 `false`，运行行为不变。
- `motto_push` 是新增枚举分支，不改变现有任务类型值。
- 现有内置窗口标题格言不迁移、不删除。
- Web 继续通过生成 binding 和通用 RPC 代理调用，不新增专用 HTTP endpoint。
- 新增完成事件对未打开 Cron 页面无副作用；旧前端不监听时任务执行行为不变。
- 模型测试是显式用户操作，不在页面加载、保存或后台任务中自动调用。

### 9.2 主要风险

- Cron 执行返回值重构可能遗漏某个任务类型或调用入口：用集中 switch、全入口搜索和定点测试防止。
- AI 或分析结果可能过长：数据库摘要、通知正文和格言正文分别设置明确长度上限。
- 外部通知慢或失败：渠道异步、相互隔离，任务状态在分发前已经确定。
- SQLite 随机查询依赖 SQLite：项目 Desktop/Web 均固定使用 SQLite，属于已确认运行约束。
- 立即执行是异步的，过早刷新无法看到最新运行信息：只在后端完成事件之后刷新。
- 模型测试会产生一次真实请求：使用最小输入、无隐式重试，并在 UI 明确由用户点击触发。

### 9.3 回滚

- 产品代码回滚后数据库中新增表和列可安全保留；旧版本会忽略它们。
- 不执行删除列或删表迁移。
- 若通知链路出现问题，可先在 UI 关闭普通任务开关；`motto_push` 可暂停或删除任务。
- 若模型测试出现 provider 兼容问题，可独立回滚测试 RPC/按钮，不影响 AI 配置 CRUD 和已有模型调用。
