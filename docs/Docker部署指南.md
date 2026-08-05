# Docker 部署指南

Docker 模式启动完整的 go-stock Web 页面，不会启动 Wails 窗口或托盘。该模式没有应用登录，所有浏览器连接共享同一份设置、任务、会话、自选股和 SQLite 数据。

## 启动

需要 Docker Engine 和 Docker Compose v2。首次构建前请确保 Docker daemon 可用，并为 Docker 分配至少 4 GB 内存；主前端构建包含较多模块，内存过低会导致 Node.js heap OOM。在仓库根目录执行：

```bash
docker compose up -d --build
docker compose ps
curl -fsS http://127.0.0.1:18888/api/health
```

默认访问地址是 <http://127.0.0.1:18888>。可通过 `GO_STOCK_WEB_PORT` 修改宿主机端口：

```bash
GO_STOCK_WEB_PORT=28888 docker compose up -d
```

容器以非 root 用户运行，默认时区为 `Asia/Shanghai`。镜像内包含 Chromium 和中文字体，供 Web 抓取功能使用。

通过 Compose 构建时，默认使用 DaoCloud 的 Node、Go、Debian 基础镜像、Aliyun 的 Debian HTTPS mirror 和 `https://goproxy.cn` Go modules 代理，并继续校验 Debian 软件包和 Go modules。国内 Go proxy 默认不回退 `direct`，避免受限网络再次连接 GitHub 并长时间阻塞。需要全部切换回官方源时可执行：

```bash
NODE_IMAGE=node:22-bookworm-slim \
GO_IMAGE=golang:1.26-bookworm \
GOPROXY=https://proxy.golang.org,direct \
GOSUMDB=sum.golang.org \
RUNTIME_IMAGE=debian:bookworm-slim \
DEBIAN_MIRROR=https://deb.debian.org \
docker compose build
```

也可以通过同名环境变量只替换其中一个构建源；例如只替换 Go builder 基础镜像：

```bash
GO_IMAGE=golang:1.26-bookworm docker compose build
```

如果构建环境有自己的 Go modules proxy，可以单独覆盖：

```bash
GOPROXY=https://your-go-proxy.example.com docker compose build
```

这些参数也可以写入仓库根目录的本地 `.env` 文件。无论使用哪个 registry，基础镜像版本仍应保持 Node 22、Go 1.26 和 Debian bookworm slim；不要把 `GOSUMDB` 设置为 `off` 来绕过依赖校验。

## 数据持久化

Compose 创建并挂载以下 named volumes：

| Volume | 容器目录 | 内容 |
| --- | --- | --- |
| `go-stock-data` | `/app/data` | SQLite 数据库及 WAL/SHM 文件、配置和业务数据 |
| `go-stock-skills` | `/app/skills` | 文件系统 skills |
| `go-stock-logs` | `/app/logs` | 运行日志和 Agent transcript |

`docker compose down` 不会删除这些 volumes。不要执行 `docker compose down -v`，除非确认要删除全部持久化数据。该部署只支持单个 go-stock 容器；不要扩容多个副本同时写入 `go-stock-data`。

## 网络安全边界

应用没有登录、session、用户隔离或 RBAC。任何能访问端口的人都可以修改共享配置、调用 AI 和运行 Agent 能力，因此不要将端口直接暴露到公网。

Compose 默认只绑定宿主机回环地址。需要在可信局域网中访问时，可显式指定宿主机内网地址：

```bash
GO_STOCK_WEB_HOST=192.168.1.10 docker compose up -d
```

远程访问优先使用 VPN，或在上游反向代理/防火墙中配置访问控制和 TLS。Compose 不挂载 Docker socket、宿主机根目录或个人目录。

## 升级与回滚

升级前先按下一节创建数据库备份，再获取新版本源码并执行：

```bash
docker compose build --pull
docker compose up -d --remove-orphans
docker compose ps
curl -fsS http://127.0.0.1:18888/api/health
```

重新构建或重建容器不会删除 named volumes。需要回滚时，切换回之前的源码或镜像版本并重新执行 `docker compose up -d`；不要删除 volumes。

## 备份与恢复

SQLite 使用 WAL 模式，不能在运行中只复制 `stock.db` 作为一致性备份。在线备份使用 SQLite `.backup`：

```bash
docker compose exec go-stock sqlite3 /app/data/stock.db ".backup '/app/data/stock-backup.db'"
docker compose cp go-stock:/app/data/stock-backup.db ./stock-backup.db
docker compose exec go-stock rm /app/data/stock-backup.db
```

也可以将第一条命令替换为 `VACUUM INTO '/app/data/stock-backup.db'`。每次备份应使用新的目标文件名。

恢复会替换当前数据库。先停止应用，并将备份文件放在当前目录，再执行：

```bash
docker compose down
docker run --rm --user 0 \
  -v go-stock-data:/data \
  -v "$PWD:/backup:ro" \
  alpine:3.22 sh -eu -c '
    test -s /backup/stock-backup.db
    stamp=$(date +%Y%m%d%H%M%S)
    cp /backup/stock-backup.db /data/stock.db.restore
    chown 10001:10001 /data/stock.db.restore
    mkdir -p "/data/pre-restore-$stamp"
    for file in stock.db stock.db-wal stock.db-shm; do
      if [ -e "/data/$file" ]; then
        mv "/data/$file" "/data/pre-restore-$stamp/"
      fi
    done
    mv /data/stock.db.restore /data/stock.db
  '
docker compose up -d
curl -fsS http://127.0.0.1:18888/api/health
```

恢复前的数据库和 WAL/SHM 文件会保留在 `go-stock-data` 的 `pre-restore-<时间>` 目录中。确认恢复正确后再自行清理。

## 日常排查

```bash
docker compose ps
docker compose logs --tail=200 go-stock
docker compose restart go-stock
```

健康检查为 `GET /api/health`。若容器持续处于 `unhealthy`，先检查日志、端口占用和 named volume 写权限。
