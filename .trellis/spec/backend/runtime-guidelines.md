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
GET  /*              # embedded SPA fallback
```

参考：`web_server.go`、`web_server_test.go`。

## 事件桥

- 业务代码通过 `App.emit` 或 `data.EmitAppEvent` 发送事件，不直接依赖 `runtime.EventsEmit`。
- Desktop 使用 Wails event，Web 使用同 payload 的 SSE；事件 owner 在 `backend/models` 定义 typed payload 和稳定 name/phase。
- emitter 未设置的测试环境可以安全跳过，不得 panic。
- Web event hub 的慢或已断开 client 不能阻塞 emitter 和其他 client；关闭 hub 后 stream 正常退出。
- 注册事件监听的消费者必须只注销自己的 callback，不能按事件名清除其他订阅者。

证据：`app_events.go`、`backend/data/app_ctx.go`、`web_server_test.go`。

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
