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
POST /api/rpc
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

### 3. Contracts

RPC request and response:

```json
{"method":"GetStockList","args":["keyword"]}
{"result":{},"error":""}
```

- `method` must be exported by `frontend/wailsjs/go/main/App.js` and implemented by `*App`.
- `args` are decoded in method parameter order using the Go method signature.
- An SSE message is JSON in a `data:` frame: `{"name":"eventName","data":[...]}`.
- Browser-only file selection/download belongs in `frontend/src/web-bridge.js`; never pass a browser-local path to the container.
- `GO_STOCK_WEB_ADDR` is optional and defaults to `:18888`.
- Local development binds Vite to `127.0.0.1:5173` and the Go backend to `127.0.0.1:18888`; browser traffic must use the Vite URL so Vue HMR remains active.
- The Vite `/api` proxy must keep `changeOrigin: false`. This preserves the browser-facing `Host` header so it matches `Origin: http://127.0.0.1:5173` during the backend same-origin check.
- The backend browser executable is required for chromedp-based search, crawling, and market-site cookies in both Desktop and Web modes. It is not the browser rendering the Web UI.
- An empty `Settings.BrowserPath` resolves through `CHROME_BIN` and then platform detection. The automatic result, including no browser found, is cached for the process lifetime; restart the process to detect a newly installed browser.
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
| Skill upload is not multipart ZIP or exceeds limits | `4xx`; do not modify an existing installed skill |
| Web calls `CheckUpdate` or `QuitApp` | Do not replace the running binary or stop the server |
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
- Base: a pure query `App` method is automatically available through the binding-derived RPC allowlist.
- Bad: calling `CheckBrowser()` from every `GetSettingConfig()` or cookie request, which repeatedly scans the filesystem and floods logs.
- Bad: opening port `18888` for daily frontend development and rebuilding `frontend/dist` after every Vue change.
- Bad: enabling `changeOrigin` on the Vite proxy, which makes the backend compare the browser `Origin` with a rewritten backend `Host`.
- Bad: calling `runtime.EventsEmit`, `runtime.SaveFileDialog`, `runtime.Quit`, or the desktop updater from a Web execution path.
- Bad: mounting only `/app/data/stock.db` or running multiple containers against the same SQLite volume.

### 6. Tests Required

- `go test -tags web .`: assert the binding allowlist is implemented, invalid RPC is rejected, SSE stops cleanly, cross-origin requests fail, skill import is atomic, and Web update checks cannot run the desktop updater.
- `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags web .`: assert the headless Linux artifact compiles without desktop platform files.
- `go build .`: assert the current desktop entrypoint still compiles.
- `npm --prefix frontend run build`: assert the bridge is bundled before Vue mounts.
- `./scripts/dev-web.sh`: assert ports `5173` and `18888` become ready, `/api/health`, RPC, and SSE work through port `5173`, a Vue source edit is applied by HMR, and `Ctrl+C` releases both ports.
- Browser path resolver tests: assert configured path wins over the environment, an existing `CHROME_BIN` wins over platform detection, and repeated empty-path resolution invokes platform detection once.
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
