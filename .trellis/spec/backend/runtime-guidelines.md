# Wails 与 Web 运行时规范

## 适用范围

修改入口、`App` 方法、Wails/Web RPC、事件、build tag、运行目录、浏览器可执行文件解析或平台能力时使用本规范。

## 双运行时边界

项目共享同一套业务实现，但有两个入口：

- Desktop：`main_desktop.go` 和操作系统实现，build 条件为 `!web`。
- Web：`main_web.go`、`web_server.go`、`app_web.go`，build 条件为 `web`。

平台差异使用互斥 build tag：

```go
//go:build !web
//go:build web
//go:build windows && !web
```

Web 不得编译或模拟窗口隐藏、系统通知、文件对话框、自更新、管理员重启和退出客户端等 Desktop 能力。共享签名需要 Web 实现时，提供明确的 Web adapter，并让 UI 隐藏不可用入口；不能返回成功空实现。

参考：`main_{desktop,web}.go`、`app_web.go`、`update_helper_*`、`backend/agent/tools/streaming_shell_*`。

## App 与 RPC

- `App` 方法是前端桥接层，负责输入边界和 service 编排；可复用业务逻辑进入 backend package。
- Web RPC allowlist 从 `frontend/wailsjs/go/main/App.js` 的生成 binding 读取，并与 `*App` 实际方法交叉验证。
- Desktop-only 方法必须显式从 Web allowlist 排除。未知、未绑定或被排除的方法返回 `404`，不得反射调用。
- 参数按 Go 方法顺序解码；数量/类型错误返回 `400`，非 JSON media type 返回 `415`。
- 反射 panic 只在 transport 边界 recover 并返回安全错误，不能让 process 崩溃，也不能在 service 层吞 panic。
- 新增/修改 App 方法后同步生成 binding，并验证 Desktop 与 Web 都能编译。

Web 接口边界：

```text
GET  /api/health
POST /api/rpc/{method}
POST /api/rpc        # legacy compatibility
GET  /api/events
POST /api/skills/import
POST /api/trading-records/import
POST /api/knowledge-base/file/import
POST /api/knowledge-base/files/import
GET  /*              # embedded SPA fallback
```

参考：`web_server.go`、`web_server_test.go`。

## 浏览器文件能力替代契约

### 1. Scope / Trigger

新增或修改 Desktop 文件选择、上传、保存、下载、打开链接或剪贴板调用时，必须先判断
浏览器是否有安全的原生等价能力。能等价实现时不得仅通过 `isWebMode` 隐藏入口；窗口、
托盘、管理员重启、客户端退出和桌面自更新等确无浏览器等价能力时才隐藏并排除 RPC。

### 2. Signatures

- 业务组件继续调用生成的 Wails binding；Web 由 `frontend/src/web-bridge.js` 提供同签名
  special method，例如 `ImportTradingRecordsFromExcel()`、`PickKBFilePaths()`、
  `UploadKBFiles(kbName, tokens)`。
- 客户端文件内容只能进入受限同源 endpoint；服务器路径型 App 方法继续留在
  `webDesktopOnlyMethods`，不得开放通用 `/api/rpc`。
- 异步批量上传使用 `StartBatchImportWithCleanup(kbName, filePaths, cleanup)`，cleanup
  ownership 必须转移给真正读取临时文件的后台任务。

### 3. Contracts

- 浏览器选择器返回页面内 opaque token，token 只用于 Web Bridge 查找 `File`，不能被
  服务端当作路径解析；上传成功消费 token，失败保留以便重试。
- multipart 请求使用相对 `/api/*`、受同源中间件保护，并限制 media type、扩展名、数量、
  单文件大小和请求总量；文件名经过 `filepath.Base` 等价清理后写入独立临时目录。
- 同步 handler 在返回前清理临时文件；异步 handler 只在启动失败时清理，启动成功后由
  后台任务在成功、失败或 panic 时清理。
- Web 响应、轮询状态和文档元数据不得包含服务器临时路径；错误详情可写服务端日志，
  浏览器只接收稳定安全文案。

### 4. Validation & Error Matrix

| 条件 | 边界行为 |
| --- | --- |
| 非 `multipart/form-data` | `415`，不创建临时目录 |
| 缺少文件/字段、扩展名不支持、文件数量超限 | `400`，清理已创建临时资源 |
| 单文件或总大小超限 | `413`，清理已创建临时资源 |
| `Origin` 与 `Host` 不匹配 | `403`，不得进入业务 handler |
| 同步业务导入失败 | 安全文案 `4xx`，返回前清理 |
| 异步任务启动失败 | 安全文案 `4xx`，handler 立即清理 |
| 异步任务运行结束或 panic | 后台 defer cleanup；轮询状态不暴露 panic 值或临时路径 |

### 5. Good / Base / Bad Cases

- Good：交易记录和知识库文本文件使用浏览器选择器、opaque token、multipart endpoint 和
  lifecycle 测试，Desktop App 方法签名保持不变。
- Base：确无浏览器替代的窗口或托盘能力在 Web UI 隐藏，并由 allowlist 拒绝直接 RPC。
- Bad：只隐藏浏览器可实现的文件入口；把客户端路径作为 JSON RPC 参数；上传成功后由
  handler 提前删除后台仍需读取的文件；返回“成功”但不执行任何动作。

### 6. Tests Required

- Web Bridge/静态检查：生成 binding 方法有对应 special method，取消选择保持原 Promise
  语义，重复点击有 loading guard，成功/失败分别消费或保留 token。
- HTTP tests：合法上传、Content-Type、扩展名、缺少字段、数量、单文件/总大小、同源拒绝、
  服务器路径型 RPC 排除和同步 cleanup。
- Agent lifecycle tests：启动失败 cleanup、后台完成 cleanup、panic cleanup，以及批量结果、
  状态和元数据均不包含临时路径。
- 构建：`go build .`、`go test -tags web .`、Linux Web 交叉编译和前端 build。

### 7. Wrong vs Correct

```js
// Wrong：浏览器有原生文件选择能力却直接隐藏。
const visible = !isWebMode

// Correct：组件继续调用同一 binding，Web Bridge 负责选择 File 并上传到受限 endpoint。
await UploadKBFiles(kbName, opaqueFileTokens)
```

## 事件桥

- 业务代码通过 `App.emit` 或 `data.EmitAppEvent` 发送事件，不直接依赖 `runtime.EventsEmit`。
- Desktop 使用 Wails event，Web 使用同 payload 的 SSE；事件 owner 在 `backend/models` 定义 typed payload 和稳定 name/phase。
- 表示“完成”的事件必须对应已持久化状态：数据库写回成功后才能 emit；写回失败时不得发送完成事件或触发依赖该事件的通知、刷新等旁路副作用。
- emitter 未设置的测试环境可以安全跳过，不得 panic。
- Web event hub 的慢或已断开 client 不能阻塞 emitter 和其他 client；关闭 hub 后 stream 正常退出。
- 注册事件监听的消费者必须只注销自己的 callback，不能按事件名清除其他订阅者。

证据：`app_events.go`、`backend/data/app_ctx.go`、`web_server_test.go`。

## 场景：持久化完成后刷新任务列表

### 1. Scope / Trigger

后台任务执行会更新运行次数、最近执行时间或结果，而多个运行时前端需要在完成后刷新列表时适用。

### 2. Signatures

- Event name：`cronTaskExecuted`。
- Payload：`CronTaskExecutedEvent{TaskID uint, Success bool, CompletedAt time.Time}`。
- 前端 listener：接收 typed payload 后重新查询当前筛选条件下的任务列表。

### 3. Contracts

- 只有运行信息成功写回数据库后才能发送完成事件和完成通知；写回失败时执行入口返回失败，且不产生任何“已完成”旁路副作用。
- Desktop Wails 与 Web SSE 共享同一 event name 和 payload；字段由 `backend/models` 定义，前端不得通过解析日志或错误字符串判断完成状态。
- 立即执行入口等待完成事件刷新，不在 RPC 返回前后额外发起一次可能读到旧数据的查询。
- listener 卸载时只调用注册返回的 stop callback，不按事件名清空其他消费者。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 业务执行成功且运行信息写回成功 | 发一次事件；通知开关开启时发一次完成通知 |
| 业务执行失败但失败结果写回成功 | 发一次 `success=false` 事件，并展示安全失败摘要 |
| 运行信息写回失败 | 不发事件、不通知、不触发列表刷新 |
| 页面存在多个 listener | 卸载当前页面不影响其他 listener |

### 5. Good / Base / Bad Cases

- Good：统一执行入口完成业务与持久化后发 typed event，页面收到后刷新当前列表。
- Base：定时触发和“立即执行”复用同一完成边界。
- Bad：先 emit 再写库；RPC 返回后立即刷新一次，同时完成事件又刷新一次；使用全局 `EventsOff(name)`。

### 6. Tests Required

- emitter 回调内重新读取数据库，断言运行次数和最近执行时间已经更新。
- 强制制造写回失败，断言事件数和通知数都为零。
- 成功和业务失败各断言只发一个事件，payload 的任务 ID、状态和完成时间正确。
- 前端定点验证“立即执行”后列表自动刷新，筛选条件保持不变。

### 7. Wrong vs Correct

```go
// Wrong：消费者可能在数据库提交前读到旧状态。
emit(CronTaskExecutedEventName, payload)
saveRunInfo(task)

// Correct：持久化是完成事件的前置条件。
if err := saveRunInfo(task); err != nil {
    return nil, err
}
emit(CronTaskExecutedEventName, payload)
```

## 同源与本地开发

- 浏览器使用同源相对 `/api/*`，不把 backend host 硬编码进业务代码。
- 本地开发从 Vite `127.0.0.1:5173` 访问，Vite 将 `/api` 代理到 `127.0.0.1:18888`。
- Vite proxy 保持 `changeOrigin: false`，使浏览器 `Origin` 与转发后的 `Host` 匹配 backend same-origin 校验。
- backend 对不匹配的 `Origin` 返回 `403`，不增加通配 CORS。
- Vue 源码变化使用 HMR，不需要反复生成 production dist 或重启 Go backend。

## 运行根目录

需要在 Desktop/Web 间保持稳定的应用根目录消费者应通过 `backend/runtimepath.RootDir()` 解析：

- Desktop 优先可执行文件目录，确保打包应用从任意 cwd 启动时仍找到数据。
- Web 优先当前工作目录，避免 `go run` 临时 executable 目录吞掉 data、skills 和 logs。
- Web Docker 使用 `WORKDIR /app`，因此 runtime root 为 `/app`。
- 业务 helper 不自行选择 `os.Getwd()` 或 `os.Executable()`；skills、Agent sandbox 和新增的应用目录从同一 root 派生。

当前 SQLite 默认路径和 logger 仍使用相对 cwd；Web 依靠 `WORKDIR /app` 保持稳定，Desktop 尚未全部迁移到 `runtimepath`。这是明确的历史债务，新规范不得把它们描述成已经统一。

参考：`backend/runtimepath/root.go`、`root_desktop.go`、`root_web.go` 及对应测试。

## 后端浏览器解析

chromedp 使用的浏览器与渲染 Web UI 的用户浏览器不是同一概念。可执行文件解析顺序为：

```text
Settings.BrowserPath > CHROME_BIN > platform detection
```

自动解析结果按进程缓存，包括“未找到”；安装新浏览器后重启进程再检测。所有 crawler/cookie 路径复用 resolver，不能在高频 getter 中重复扫描文件系统。

## 生命周期

- 入口创建可取消 root context，并响应 SIGTERM/interrupt。
- shutdown 先停止后台 bot/cron/stream，再用有界 context 关闭 HTTP server。
- `GO_STOCK_WEB_DISABLE_JOBS=1` 只用于隔离测试和 smoke，正常运行不得静默关闭后台任务。
- Desktop/Web startup 必须分别配置事件 emitter、HTTP 设置和平台资源，测试 teardown 恢复全局 emitter。

## 场景：可编辑后台任务的运行时注册表

### 1. Scope / Trigger

数据库任务可改名、启停或删除，运行时使用 map 保存 scheduler/timer entry 时适用。

### 2. Signatures

- 稳定键：`task:<immutable-id>`。
- 注册：`schedule(task) error`；注销：`unschedule(taskID)`。

### 3. Contracts

注册键只能使用不可变 ID，不能拼接名称等可编辑字段。更新前先按 ID 注销旧 entry；停用和删除同时删除 scheduler entry 与注册表键；注册失败不得留下零值或失效 entry。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 任务改名 | 旧 entry 被替换，运行时仍只有一个 entry |
| 任务停用或删除 | scheduler 与注册表均无该 ID |
| 新 Cron 表达式注册失败 | 返回 error，不登记无效 entry |
| 创建时 `enable=false` | 只持久化，不注册 scheduler |

### 5. Good / Base / Bad Cases

- Good：所有创建、更新、启停、启动恢复入口复用同一 `schedule/unschedule` helper。
- Base：注册表仅用任务 ID，名称只用于展示和日志。
- Bad：使用 `id + name` 作为 key，改名后无法找到旧闭包，造成重复执行和重复通知。

### 6. Tests Required

- 创建启用任务后 entry 数为 1；改名更新后仍为 1 且 entry ID 已替换。
- 删除或停用后 entry 数为 0 且注册表键不存在。
- 创建停用任务不产生 entry。

### 7. Wrong vs Correct

```go
// Wrong：name 可变，更新后旧 entry 会失联。
key := fmt.Sprintf("%d_%s", task.ID, task.Name)

// Correct：只用不可变 ID 建立运行时身份。
key := fmt.Sprintf("cron_task:%d", task.ID)
```

## 禁止模式

- Web 路径调用 `runtime.Quit`、Desktop dialog 或 updater。
- 共享代码直接 `runtime.EventsEmit`，绕开 Web SSE。
- 新 App 方法未生成 binding，或手工加入 Web allowlist 绕过生成契约。
- Web 使用 `os.Executable()` 作为 data root。
- Vite proxy 设置 `changeOrigin: true` 后通过放宽 CORS 掩盖同源错误。
- 反复扫描浏览器路径并在日志中洪泛相同错误。

## 验证

- `go test -tags web .`：RPC allowlist、参数/状态码、SSE、同源、Desktop-only 排除和 shutdown。
- `go test ./backend/runtimepath` 与 `go test -tags web ./backend/runtimepath`：两种 root 语义。
- `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags web .`：无 Desktop API 泄漏。
- `go build .`：当前 Desktop 入口仍可编译。
- 修改 binding 时运行生成文件一致性检查和前端 build。
- 完成事件测试需在 emitter 内读取持久化状态，并强制制造一次写回失败，分别证明“先写库再发事件”和“写库失败无完成副作用”。
