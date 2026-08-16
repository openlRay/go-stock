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
- `/app/data`、`/app/skills`、`/app/logs`、`/app/memory` 在镜像内创建并归该用户写入。
- `WORKDIR /app` 是 Web runtime root，与 `backend/runtimepath.RootDir()` 的 Web 语义一致。
- 不通过改回 root 掩盖 NAS/volume ownership 错误；部署前修复挂载目录权限。
- `CHROME_BIN=/usr/bin/chromium` 供 backend chromedp 使用，不代表用户 UI 浏览器。

## 持久化

必须挂载完整目录：

```text
/app/data
/app/skills
/app/logs
/app/memory
```

- 不只挂载 `/app/data/stock.db`；SQLite WAL/SHM 必须与数据库文件位于同一 volume。
- `/app/memory` 的完整向量记忆持久化与升级迁移契约见下方场景。
- 容器重建不能删除配置、缓存、导入 skill 和日志标记。
- Web 模式是单用户、无应用登录、单进程写 SQLite；不得让多个容器同时写同一 data volume。
- 需要备份时对整个 data 目录使用 SQLite 一致性方案，不复制正在写入的单个 db 文件冒充完整备份。

## Scenario: Docker 向量记忆持久化

### 1. Scope / Trigger

当 Web runtime、Agent memory、知识库、Docker volume 或运行用户发生变化时使用本契约。
目标是让 `/app/memory/.vectorstore` 在容器重建后继续可用，并避免首次启用命名卷时覆盖
旧容器可写层中的已有数据。

### 2. Signatures

```yaml
services:
  go-stock:
    volumes:
      - go-stock-memory:/app/memory
volumes:
  go-stock-memory:
    name: go-stock-memory
```

Dockerfile 必须在 `USER go-stock:go-stock` 之前执行：

```dockerfile
install -d -o go-stock -g go-stock /app/memory
```

### 3. Contracts

- Web `runtimepath.RootDir()` 在容器中解析为 `WORKDIR /app`，chromem-go、KB 元数据和
  知识图谱统一落到 `/app/memory/.vectorstore`。
- `go-stock-memory` 是独立命名卷，不与 SQLite `/app/data` 混用；运行进程以 UID/GID
  `10001` 读写整个 `/app/memory`。
- 新空卷不会自动复制旧容器可写层的数据。已有部署首次 recreate 前必须把旧容器的
  `/app/memory` 复制到命名卷或外部备份，验证后再删除旧容器。

### 4. Validation & Error Matrix

| 条件 | 要求 |
| --- | --- |
| Compose 未挂载 `/app/memory` | 阻断发布，容器重建会丢失向量记忆 |
| 镜像内目录不是 UID/GID 10001 可写 | 阻断发布，应用初始化向量库会失败 |
| 新部署、无历史 memory | 创建空命名卷并验证 `.vectorstore` 可写 |
| 旧部署、历史数据在容器层 | 先复制和校验数据，再 recreate |
| recreate 后 collection/KB 元数据缺失 | 保留旧容器/备份，不继续清理，恢复后排查挂载目标 |

### 5. Good / Base / Bad Cases

- Good：四个持久化目录均使用命名卷，memory 经过写入、recreate、读取验证。
- Base：全新部署创建空 `go-stock-memory`，应用首次启动创建 `.vectorstore`。
- Bad：只在 Dockerfile 创建 `/app/memory` 却不挂卷；直接 recreate 含历史向量数据的旧
  容器，期望 Docker 自动迁移容器层文件。

### 6. Tests Required

- `docker compose config`：断言 `go-stock-memory` 的 target 为 `/app/memory`。
- 镜像 smoke：以 UID/GID 10001 在 `/app/memory` 创建和读取标记。
- 持久化 smoke：写入标记后 recreate（不得 `down -v`），断言标记仍存在。
- 升级 smoke：有旧容器数据时先复制到命名卷，校验 `.vectorstore` 文件数或 checksum 后
  再切换。

### 7. Wrong vs Correct

```yaml
# Wrong：数据只写入容器层。
volumes:
  - go-stock-data:/app/data

# Correct：向量记忆拥有独立持久化卷。
volumes:
  - go-stock-data:/app/data
  - go-stock-memory:/app/memory
```

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
- Docker daemon 可用且本次确实修改部署时，执行 build/smoke：等待 healthy，断言 UID `10001` 与四个目录可写。
- 写入 data/skills/logs/memory 标记后 recreate container（不删除 volume），断言标记仍存在。
- 发送 SIGTERM，断言进程在 grace period 内退出且没有强制 kill。
- 仅修改文档时不运行 Docker smoke，只检查文档与当前 Dockerfile/Compose 一致。
