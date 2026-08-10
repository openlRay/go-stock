# Docker Web 部署与全端去 VIP

## Goal

在保留现有 Wails 桌面应用和 Vue 业务页面的基础上，新增可在 Docker 中无 GUI 运行、通过浏览器访问的完整 Web Server 模式；Web 与桌面端均取消全部 VIP 功能门禁，使用同一套单用户业务数据，并通过 Docker Volume 持久化运行状态。

## Background and Confirmed Facts

- 当前主应用是 Wails 桌面程序，前端通过 `frontend/wailsjs/go/main/App.js` 调用 Go `App` 方法，并通过 `window.runtime` 订阅事件。
- 前端约 50 个源码文件依赖 Wails bindings；直接逐页改写为 REST 会制造大量重复改动。
- 主要业务和数据库能力位于 `backend/data`、`backend/agent`、`backend/db`，可以由桌面和 Web 入口复用。
- SQLite 默认位于 `data/stock.db`，启用了 WAL，因此持久化目标必须是整个 `data` 目录，而不是单个数据库文件。
- `ai-assistant-web` 已证明无 GUI Go HTTP 服务可以在 Linux/CGO disabled 环境编译，但它只覆盖 AI 助手子集。
- 当前 Linux 主包存在 `app.go` 与 `app_linux.go` 的 `App`/方法重复定义，Web 构建必须通过独立 build mode 隔离平台文件，不能直接复用现有 Linux 桌面入口。

## Requirements

### R1. Full Web Server mode

- 新增独立的 Web build/run mode，不启动 Wails 窗口、托盘或桌面生命周期。
- Web Server 同源提供 Vue 静态资源、RPC API、事件流和健康检查。
- 保留桌面入口，桌面版与 Web 版复用业务实现，不复制行情、AI、数据库等核心逻辑。

### R2. Wails-compatible browser bridge

- 保留现有 `frontend/wailsjs/go/main/App.js` 导入形式。
- 浏览器中缺少 Wails runtime 时，安装兼容的 `window.go.main.App` RPC proxy 和 `window.runtime` adapter。
- 普通查询/写入通过 JSON RPC 调用 Go `App` 公开方法；服务端只允许前端绑定清单中的方法。
- Wails `EventsOn`/`EventsOff`/后端 `EventsEmit` 语义由 Web 事件流承接。
- 浏览器链接、Clipboard、刷新等能力使用 Web API；窗口、托盘、管理员重启等纯桌面能力在 Web 模式下安全降级。

### R3. Remove all VIP gates globally

- Web 和桌面端都不再根据赞助码、VIP 等级或有效期限制任何本地功能。
- 删除 VIP 弹窗、倒计时关闭、按钮禁用、条件渲染和功能警告。
- 删除 `ai-assistant-web` 的 `requireVip2` 门禁。
- 删除后端非 VIP 自选股数量限制及其他本地 VIP 分支。
- 自动更新不再因 VIP 状态决定功能可用性；统一保证公开更新路径可用。
- 保留现有数据库字段以避免无必要迁移，但权限逻辑不再读取 `sponsor_code`。
- “关于/赞助”可以保留为纯支持信息，不展示 VIP 等级或权益，不影响任何功能。
- 不绕过 AI 平台、提示词广场、飞书、MCP 等第三方服务自身要求的 token/API key/账号。

### R4. Single-user and no application login

- 不新增用户、租户、角色、登录、session 或权限模型。
- 所有浏览器连接共享同一套配置、任务、会话、自选股和数据库状态。
- Web Server 默认按单实例运行，不支持多副本并发写同一个 SQLite 数据库。

### R5. Persistent runtime data

- SQLite 数据目录、文件系统 skills 和需要保留的运行日志使用 Docker named volumes。
- Volume 挂载整个目录，必须包含 SQLite 的 `stock.db`、`stock.db-wal` 和 `stock.db-shm`。
- 容器重建、升级或重启后，配置、AI 会话、自选股、交易记录、任务和 skills 不丢失。
- 镜像不得包含开发机当前的 `data/stock.db`、日志或密钥。

### R6. Docker operation

- 提供 multi-stage `Dockerfile`、`.dockerignore` 和 `compose.yaml`。
- 使用 Go 1.26 和 Node 22 构建；运行镜像提供 CA、时区、Chromium 及中文字体等 Web 抓取运行依赖。
- 容器以非 root 用户运行，提供 `/api/health` healthcheck，默认时区为 `Asia/Shanghai`。
- Web Server 与浏览器同源通信，不启用宽泛的 `Access-Control-Allow-Origin: *`。
- 文档明确无登录部署不应直接裸露公网；安全边界由本机、内网、VPN 或上游网络策略提供。

## Acceptance Criteria

- [ ] `docker compose up --build` 能在无 GUI Linux 环境启动，浏览器可以打开完整 go-stock Vue 页面。
- [ ] 页面不依赖 Wails WebView 也能完成至少以下关键链路：读取/保存设置、自选股查询和修改、行情/K 线查询、AI 配置、AI 流式对话、定时任务/Agent 相关页面读取。
- [ ] Web 后端事件能驱动现有前端订阅，断开连接后可以重新连接，不造成服务端 panic。
- [ ] Web 页面不出现登录页面或登录要求；所有连接共享同一份状态。
- [ ] 无赞助码、过期赞助码和历史有效赞助码三种状态的功能可用性完全一致。
- [ ] Web 与桌面页面均不再出现 VIP 专属弹窗、VIP 功能警告、倒计时关闭或 VIP 导致的禁用状态。
- [ ] 自选股超过 63 只不再被 VIP 逻辑阻止。
- [ ] `ai-assistant-web` API 不再返回 `VIP2_REQUIRED`。
- [ ] 容器重启和重新创建后，SQLite 数据、配置、AI 会话和 skills 仍然存在。
- [ ] 镜像不包含仓库工作区当前数据库、日志、`.env` 或其他运行密钥。
- [ ] Web 模式不启动 Wails 窗口/托盘；桌面构建仍能通过原入口启动。
- [ ] Go tests、前端 build、Web tagged build 和 Docker health smoke test 通过；任何既有基线失败单独报告。

## Out of Scope

- 多用户、多租户、登录、RBAC 和不同用户的数据隔离。
- SQLite 多副本部署、PostgreSQL 迁移、Kubernetes 或横向扩容。
- 在浏览器中复刻托盘、窗口置顶、管理员重启等操作系统 UI。
- 绕过第三方服务的账号、付费、token 或 API key 限制。
- 自动 commit、push、发布镜像或修改现有 CI。

