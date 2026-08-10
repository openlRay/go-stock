# Web Runtime and Docker Contract

## Scenario: Full Web mode alongside Wails desktop builds

### 1. Scope / Trigger

Use this contract when changing the Web Server entrypoint, an `App` method used by the browser, backend-to-frontend events, or Docker runtime paths. The goal is to keep one business implementation while preventing Web builds from invoking Wails window, tray, dialog, or self-update behavior.

### 2. Signatures

Build boundaries:

```go
// Desktop entrypoint
//go:build !web

// Web entrypoint and Web-only platform adapters
//go:build web

// Desktop platform implementation
//go:build darwin && !web // or windows/linux
```

Docker build arguments:

```text
NODE_IMAGE=node:22-bookworm-slim
GO_IMAGE=golang:1.26-bookworm
GOPROXY=https://proxy.golang.org,direct
GOSUMDB=sum.golang.org
RUNTIME_IMAGE=debian:bookworm-slim
DEBIAN_MIRROR=https://mirrors.aliyun.com
```

These are the Dockerfile defaults. `compose.yaml` defaults the three base images to their version-equivalent DaoCloud paths and `GOPROXY` to `https://goproxy.cn` for domestic builds. It exposes all six values through environment-variable interpolation so users can switch registries and Go module proxies without editing the file.

The frontend build must keep `NODE_OPTIONS=--max-old-space-size=4096`; the current bundle transforms more than 36,000 modules and exceeds Node's default heap in a clean Docker build.

Shared event boundary:

```go
type EventEmitter interface {
	Emit(name string, data ...any)
}

func (a *App) emit(name string, data ...any)
func data.SetAppEventEmitter(func(name string, data ...any))
func data.EmitAppEvent(name string, data ...any)
```

HTTP surface:

```text
GET  /api/health
POST /api/rpc/{method}
POST /api/rpc             (legacy compatibility)
GET  /api/events
POST /api/skills/import
GET  /*                 (embedded SPA with index.html fallback)
```

Local Web development:

```text
http://127.0.0.1:5173  Vite frontend with HMR
http://127.0.0.1:18888 Go Web backend
/api/*                 Vite proxy to the Go Web backend
```

Backend browser executable resolution:

```text
Settings.BrowserPath > CHROME_BIN > platform CheckBrowser()
```

Application runtime root resolution:

```text
Web build     backend/runtimepath.RootDir() -> os.Getwd() -> executable directory -> "."
Desktop build backend/runtimepath.RootDir() -> executable directory -> os.Getwd() -> "."
skillsDir() and deepAgentRootDir() must both derive from runtimepath.RootDir()
```

Web 与桌面能力的构建边界：

```go
// Windows 桌面窗口/通知实现
//go:build windows && !web

// Web 仅保留共享调用需要的空平台钩子，不得引用 HideWindow、系统通知或自更新实现
//go:build web
```

### 3. Contracts

RPC request and response:

```text
POST /api/rpc/GetStockList
{"args":["keyword"]}
{"result":{},"error":""}
```

- The primary Web bridge must put the RPC method in the URL path so browser Network tooling identifies requests by method name. `POST /api/rpc` with `{"method":"...","args":[]}` remains a compatibility endpoint for older Web bundles.
- `method` must be exported by `frontend/wailsjs/go/main/App.js` and implemented by `*App`.
- Web RPC 必须排除 `CheckUpdate`、`QuitApp`、`RestartAsAdmin`、`OpenURL` 以及桌面文件对话框方法。浏览器可原生完成的打开链接、选文件和下载由 `web-bridge.js` 实现；不能原生完成的桌面能力直接隐藏，不能以 Web 空实现伪装成功。
- `args` are decoded in method parameter order using the Go method signature.
- An SSE message is JSON in a `data:` frame: `{"name":"eventName","data":[...]}`.
- Browser-only file selection/download belongs in `frontend/src/web-bridge.js`; never pass a browser-local path to the container.
- `GO_STOCK_WEB_ADDR` is optional and defaults to `:18888`.
- Local development binds Vite to `127.0.0.1:5173` and the Go backend to `127.0.0.1:18888`; browser traffic must use the Vite URL so Vue HMR remains active.
- The Vite `/api` proxy must keep `changeOrigin: false`. This preserves the browser-facing `Host` header so it matches `Origin: http://127.0.0.1:5173` during the backend same-origin check.
- The backend browser executable is required for chromedp-based search, crawling, and market-site cookies in both Desktop and Web modes. It is not the browser rendering the Web UI.
- An empty `Settings.BrowserPath` resolves through `CHROME_BIN` and then platform detection. The automatic result, including no browser found, is cached for the process lifetime; restart the process to detect a newly installed browser.
- A Web build uses its current working directory as the application root. `scripts/dev-web.sh` changes to the repository root before `go run -tags web .`, while the Docker image uses `WORKDIR /app`; this keeps `data`, `skills`, and `logs` on the intended local or mounted paths instead of Go's temporary executable directory.
- A Desktop build uses the executable directory as the application root so a packaged application can be launched from any working directory. Shared code must call `backend/runtimepath.RootDir()` rather than choosing `os.Getwd()` or `os.Executable()` independently.
- Web 构建不得编译 Windows 的 `HideWindow`/`CREATE_NO_WINDOW`、系统托盘通知或桌面自更新辅助文件；共享代码需要同名钩子时，由 `//go:build web` 文件提供不含桌面行为的实现。
- 浏览器发往本服务的请求必须使用同源相对 `/api/*` 地址。后端新增第三方请求必须使用共享 HTTP client、HTTPS（上游支持时）、结构化 query 参数，并在发出请求前对白名单、日期、页码和代码格式做校验；不得把 RPC 入参直接拼接进 URL。
- `GO_STOCK_WEB_DISABLE_JOBS=1` is for isolated tests and smoke checks; normal deployments run background jobs.
- Docker persists whole directories at `/app/data`, `/app/skills`, and `/app/logs`. Persist the complete data directory so SQLite WAL and SHM files stay with `stock.db`.
- The runtime image runs as `go-stock:go-stock` (UID/GID `10001`) and all three mounted directories must be writable by that identity.
- The image build arguments may point to compatible registry or Debian mirrors when the official upstream is unavailable; changing them must not change the Node 22, Go 1.26, or Debian bookworm runtime contract.
- The Compose `GOPROXY` default intentionally omits `direct`; a missing module must fail through the proxy instead of falling back to an unreachable GitHub connection until the deployment timeout. Keep `GOSUMDB` enabled.
- Web mode is single-user, has no application login, and supports one process writing the SQLite volume.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| Non-JSON RPC body | `415` with a JSON error |
| Unknown or non-bound method | `404`; do not reflectively call it |
| Wrong RPC argument count/type | `400` with a method/argument error |
| Cross-origin request with a mismatched `Origin` | `403`; do not add wildcard CORS |
| Vite proxy rewrites `Host` with `changeOrigin: true` | Browser RPC/upload requests fail with `403`; restore `changeOrigin: false` |
| A file under `frontend/src` changes during local development | Vite pushes an HMR update; no production build or Go restart is required |
| `Settings.BrowserPath` is empty and `CHROME_BIN` names an existing file | Use `CHROME_BIN`; do not run platform detection |
| `Settings.BrowserPath` and `CHROME_BIN` are empty or invalid | Run platform detection once per process; repeated settings reads reuse the cached result |
| A Web test or `go run -tags web .` resolves the runtime root from `os.Executable()` | Reject the temporary Go build directory; use the current working directory so skill imports and Agent logs remain under the application root |
| A packaged Desktop app starts with an unrelated working directory | Use the executable directory; do not move skills or Agent files into the caller's directory |
| Skill upload is not multipart ZIP or exceeds limits | `4xx`; do not modify an existing installed skill |
| Web calls `CheckUpdate` or `QuitApp` | Do not replace the running binary or stop the server |
| Web 请求桌面专属 RPC 方法 | 返回 `404`；前端对应入口不可见，不以成功的空响应兼容 |
| Web/Windows 交叉编译遇到 `syscall.SysProcAttr.HideWindow` | 说明桌面文件越过构建边界；用 `windows && !web` 排除，而不是在 Web 中模拟窗口行为 |
| 第三方请求参数包含路径、查询串注入或非法日期 | 在发出网络请求前返回空结果/参数错误；不得拼接或转发非法值 |
| One SSE client is slow or disconnects | Do not block emitters or panic; other clients continue |
| Docker process receives SIGTERM | Stop HTTP/cron resources within the shutdown timeout |
| Docker frontend build uses the default Node heap | May OOM while transforming the production bundle; build with the declared 4 GB heap option |
| `go mod download` cannot reach the configured proxy | Override `GOPROXY` with a reachable Go module proxy; do not disable checksum verification |
| A restricted build uses `GOPROXY=...,direct` and the proxy misses | The Go command may stall on direct GitHub access; omit `direct` in that environment |
| Mounted volume is not writable by UID 10001 | Fail deployment validation before relying on persistence; do not run the application as root to mask ownership errors |

### 5. Good / Base / Bad Cases

- Good: a shared service emits through `data.EmitAppEvent`, and desktop receives Wails events while Web receives the same payload over SSE.
- Good: local development opens port `5173`, and RPC/SSE/skill upload use relative `/api` URLs through the Vite proxy.
- Good: Docker sets `CHROME_BIN=/usr/bin/chromium`, and all backend chromedp consumers reuse the same resolved executable path.
- Good: Web skill import and DeepAgent use `/app/skills` and `/app/logs` in Docker because `WORKDIR /app` is the Web runtime root.
- Good: Web 菜单不展示隐藏到托盘、退出客户端、检查更新或管理员重启；对应 Go 方法也不进入 RPC allowlist。
- Good: 第三方查询通过 `SetQueryParam(s)` 传参，板块代码和日期先按明确格式验证，支持 HTTPS 的数据源只使用 HTTPS。
- Base: a pure query `App` method is automatically available through the binding-derived RPC allowlist.
- Base: a Desktop release stores skills next to the executable even when launched from another directory.
- Bad: calling `CheckBrowser()` from every `GetSettingConfig()` or cookie request, which repeatedly scans the filesystem and floods logs.
- Bad: shared code calls `os.Executable()` directly for Web data paths; `go run` and Web tests then write into a temporary build directory.
- Bad: opening port `18888` for daily frontend development and rebuilding `frontend/dist` after every Vue change.
- Bad: enabling `changeOrigin` on the Vite proxy, which makes the backend compare the browser `Origin` with a rewritten backend `Host`.
- Bad: calling `runtime.EventsEmit`, `runtime.SaveFileDialog`, `runtime.Quit`, or the desktop updater from a Web execution path.
- Bad: 为让 Web 编译通过而把 `HideWindow`、系统通知或管理员重启改成跨平台模拟；这些能力应在构建和 UI 两层隐藏。
- Bad: 使用 `fmt.Sprintf` 或字符串相加把用户可控的代码、日期、排序字段拼入第三方 URL。
- Bad: mounting only `/app/data/stock.db` or running multiple containers against the same SQLite volume.

### 6. Tests Required

- `go test -tags web .`: assert the binding allowlist is implemented, method-named and legacy RPC routes work, invalid RPC is rejected, SSE stops cleanly, cross-origin requests fail, skill import is atomic, and Web update checks cannot run the desktop updater.
- `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags web .`: assert the headless Linux artifact compiles without desktop platform files.
- `go build .`: assert the current desktop entrypoint still compiles.
- `npm --prefix frontend run build`: assert the bridge is bundled before Vue mounts.
- `./scripts/dev-web.sh`: assert ports `5173` and `18888` become ready, `/api/health`, RPC, and SSE work through port `5173`, a Vue source edit is applied by HMR, and `Ctrl+C` releases both ports.
- Browser path resolver tests: assert configured path wins over the environment, an existing `CHROME_BIN` wins over platform detection, and repeated empty-path resolution invokes platform detection once.
- Runtime root tests: without the `web` tag, assert `RootDir()` equals the executable directory; with the `web` tag, assert it equals the working directory. `TestWebImportSkill` must prove an imported skill lands under the Web working directory.
- Web 桌面能力边界测试：断言桌面专属方法不在 allowlist 中，直接 RPC 调用返回 `404`，Windows Web 交叉编译不包含 `HideWindow`。
- 第三方请求校验测试：非法板块代码、日期、融资融券类型/排序/偏移量必须在联网前被拒绝；合法 query 使用 Resty 的结构化参数编码。
- HTTP smoke: assert health, one RPC call, SPA fallback, cross-origin `403`, and graceful SIGTERM.
- Docker smoke when the daemon is available: assert `docker compose config` resolves `GOPROXY`/`GOSUMDB`, build, wait for health, assert UID `10001` and all three mount paths are writable, write SQLite config/skill/log markers, recreate the container without deleting volumes, and assert all markers remain.

### 7. Wrong vs Correct

#### Wrong

```go
runtime.EventsEmit(a.ctx, "stock_price", stock)
runtime.Quit(a.ctx)
```

This assumes a Wails runtime context and can panic or terminate the wrong process in Web mode.

#### Correct

```go
a.emit("stock_price", stock)
if !a.webMode {
	runtime.Quit(a.ctx)
}
```

Use the shared emitter for business events, and keep desktop-only lifecycle behavior behind the explicit runtime boundary.

#### Wrong

```js
proxy: {
  '/api': {
    target: 'http://127.0.0.1:18888',
    changeOrigin: true,
  },
}
```

This rewrites `Host` to the backend address while the browser still sends the Vite address in `Origin`, so the same-origin middleware rejects the request.

#### Correct

```js
proxy: {
  '/api': {
    target: 'http://127.0.0.1:18888',
    changeOrigin: false,
  },
}
```

Keep relative browser API URLs and preserve the Vite-facing `Host` header during local development.

#### Wrong

```go
if settings.BrowserPath == "" {
	settings.BrowserPath, _ = CheckBrowser()
}
```

`GetSettingConfig()` is called frequently, so this performs and logs the same platform scan repeatedly.

#### Correct

```go
settings.BrowserPath = resolveBrowserPath(settings.BrowserPath)
```

Use the shared process-level resolver for settings, browser pools, and chromedp cookie acquisition.

#### Wrong

```bash
GOPROXY=https://goproxy.cn,direct docker compose build
```

In a build environment that cannot reach GitHub, a proxy miss falls back to a direct VCS connection and can consume the entire deployment timeout.

#### Correct

```bash
GOPROXY=https://goproxy.cn docker compose build
```

Use a reachable proxy without `direct` in restricted networks, and keep `GOSUMDB` enabled.

#### Wrong

```go
func skillsDir() string {
	executable, _ := os.Executable()
	return filepath.Join(filepath.Dir(executable), "skills")
}
```

In a Web `go run` or test process, `os.Executable()` points into Go's temporary build directory, so imported skills disappear from the application root.

#### Correct

```go
func skillsDir() string {
	return filepath.Join(runtimepath.RootDir(), "skills")
}
```

Keep the build-tag-specific root decision in `backend/runtimepath`, and reuse it for skills, Agent sandbox files, and other application-root consumers.

#### Wrong

```go
//go:build windows

cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
```

`-tags web` 与 `GOOS=windows` 同时使用时仍会编译桌面窗口实现。

#### Correct

```go
//go:build windows && !web

cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
```

Web 构建通过独立 `//go:build web` 文件满足共享函数签名，但不携带任何窗口行为。
