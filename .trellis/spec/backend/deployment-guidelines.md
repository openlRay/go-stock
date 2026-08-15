# Web 部署与持久化规范

## 适用范围

修改 `Dockerfile`、`compose.yaml`、Web server 启动、镜像源、运行用户、volume、healthcheck 或 graceful shutdown 时使用本规范。

## 构建契约

Docker 使用多阶段构建：

1. Node 22 bookworm 构建 `frontend/dist`，保留 `NODE_OPTIONS=--max-old-space-size=4096`；当前依赖规模可能超过 Node 默认 heap。
2. Go 1.26 bookworm 使用 `CGO_ENABLED=0 GOOS=linux -tags web` 构建 headless binary。
3. Debian bookworm runtime 安装 CA、Chromium、CJK 字体、SQLite CLI、curl 和 tzdata。

`NODE_IMAGE`、`GO_IMAGE`、`RUNTIME_IMAGE`、`GOPROXY`、`GOSUMDB`、`DEBIAN_MIRROR` 是可替换供应源，不得改变 Node 22、Go 1.26、Debian bookworm 和 checksum verification 语义。

- 受限网络的 Compose 默认 `GOPROXY` 不带 `direct`，避免 proxy miss 后长时间连接不可达 GitHub。
- 保持 `GOSUMDB` 开启；不能通过关闭 checksum 解决依赖下载问题。
- 修改镜像/代理参数后使用 `docker compose config` 检查最终展开值。

## 运行用户与目录

- runtime 以非 root `go-stock:go-stock`（UID/GID `10001`）运行。
- `/app/data`、`/app/skills`、`/app/logs` 在镜像内创建并归该用户写入。
- `WORKDIR /app` 是 Web runtime root，与 `backend/runtimepath.RootDir()` 的 Web 语义一致。
- 不通过改回 root 掩盖 NAS/volume ownership 错误；部署前修复挂载目录权限。
- `CHROME_BIN=/usr/bin/chromium` 供 backend chromedp 使用，不代表用户 UI 浏览器。

## 持久化

必须挂载完整目录：

```text
/app/data
/app/skills
/app/logs
```

- 不只挂载 `/app/data/stock.db`；SQLite WAL/SHM 必须与数据库文件位于同一 volume。
- 容器重建不能删除配置、缓存、导入 skill 和日志标记。
- Web 模式是单用户、无应用登录、单进程写 SQLite；不得让多个容器同时写同一 data volume。
- 需要备份时对整个 data 目录使用 SQLite 一致性方案，不复制正在写入的单个 db 文件冒充完整备份。

## 网络与安全

- backend 默认监听 `0.0.0.0:18888`，由 `GO_STOCK_WEB_ADDR` 覆盖。
- Compose 默认映射所有网卡以支持 NAS/局域网；只本机访问时使用 `GO_STOCK_WEB_HOST=127.0.0.1`。
- Web 版没有登录，不得直接暴露公网；公网访问必须由任务范围外的受控认证/reverse proxy 方案保护。
- 浏览器请求仍受 backend same-origin 校验，不能因为部署方便启用 wildcard CORS。

## Health 与 shutdown

- healthcheck 使用 `GET /api/health`，不依赖第三方网站、模型或数据库写入。
- SIGTERM 触发 root context cancel，停止后台资源，并在 10 秒 server shutdown timeout 与 Compose `stop_grace_period` 内退出。
- `init: true` 保留，用于正确转发信号和回收子进程。
- healthcheck 失败应暴露实际启动/监听问题，不能通过无条件返回 healthy 掩盖不可用服务。

## 禁止模式

- runtime 使用 root 用户运行。
- 只挂载 SQLite 主文件或多个实例共享一个 SQLite volume。
- 为下载依赖关闭 `GOSUMDB`，或在不可访问 GitHub 的环境加入 `,direct`。
- Web server 对公网开放却假设应用自带登录。
- healthcheck 调用外部行情/模型服务。
- 进程收到 SIGTERM 后立即 `os.Exit`，跳过 cron、bot、SSE 和 HTTP shutdown。

## 验证

- `docker compose config`：镜像、proxy、端口、volume 和 healthcheck 展开正确。
- Docker daemon 可用且本次确实修改部署时，执行 build/smoke：等待 healthy，断言 UID `10001` 与三个目录可写。
- 写入 data/skills/logs 标记后 recreate container（不删除 volume），断言标记仍存在。
- 发送 SIGTERM，断言进程在 grace period 内退出且没有强制 kill。
- 仅修改文档时不运行 Docker smoke，只检查文档与当前 Dockerfile/Compose 一致。
