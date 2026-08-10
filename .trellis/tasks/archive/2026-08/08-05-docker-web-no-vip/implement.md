# Implementation Plan

## 1. Establish baselines and build boundaries

- [ ] 记录当前 Go tests、Vue build 和 Web 子服务编译基线。
- [ ] 增加 Web build entrypoint/tag，隔离 Wails Linux platform files。
- [ ] 将桌面/Web 共同使用的 embeds、版本变量和初始化 helper 移到共享文件。
- [ ] 验证桌面默认 build 不进入 Web main，Web tagged build 不启动 Wails。

Verification:

```bash
go test ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -tags web .
```

## 2. Remove VIP gates from Web and desktop

- [ ] 删除 `ai-assistant-web` 的 VIP middleware 和前端 VIP gate。
- [ ] 删除主 Vue 前端全部功能级 VIP state、判断、弹窗、倒计时和 disabled 条件。
- [ ] 删除后端关注数量等 VIP 限制。
- [ ] 解耦更新下载选择与 VIP 权限；保留公开更新 fallback。
- [ ] 保留数据库字段兼容，不做 destructive migration。
- [ ] 增加无/过期/历史有效赞助状态功能一致性的测试或静态断言。

Verification:

```bash
rg -n "VIP2_REQUIRED|requireVip2|vipLevel.*[<>]=?|EffectiveSponsorVipLevel" frontend/src ai-assistant-web backend app*.go
go test ./...
npm --prefix frontend run build
```

## 3. Implement Web RPC and lifecycle

- [ ] 新增 Web HTTP server、health route、SPA static fallback 和 graceful shutdown。
- [ ] 新增 RPC request/response contract、method allowlist、typed argument decoding、panic recovery 和错误响应。
- [ ] 初始化数据库、settings、App/service state 和定时任务所需的 Web lifecycle。
- [ ] 对依赖 Wails context 的公开方法增加 Web override 或明确降级。
- [ ] 为 RPC 解码、拒绝未知 method、panic recovery 和关键调用添加 tests。

Verification:

```bash
go test ./...
go test -tags web .
curl -fsS http://127.0.0.1:18888/api/health
```

## 4. Implement events and browser runtime bridge

- [ ] 新增 EventEmitter boundary 和 Web SSE hub。
- [ ] 将 AI stream、行情、新闻、加载状态等活动事件迁移到 emitter。
- [ ] 新增浏览器 `window.go.main.App` RPC proxy。
- [ ] 新增 `window.runtime` Events/browser adapter，并确保 Wails 环境不被覆盖。
- [ ] 适配链接、Clipboard、reload、Environment 和前端本地事件。
- [ ] 为文件上传/下载和少量桌面专属调用提供 Web handler。
- [ ] 验证 SSE 断线重连、取消订阅和流式 AI 完成事件。

Verification:

```bash
npm --prefix frontend run build
go test -tags web ./...
```

Browser checks:

- 设置读取/保存
- 自选股增加、删除和超过 63 只
- 行情/K 线查询
- AI 配置和流式消息
- Agent/cron/skills 页面基础读取
- 无 VIP 提示或禁用

## 5. Add Docker deployment

- [ ] 添加 multi-stage `Dockerfile` 和 `.dockerignore`。
- [ ] 添加 `compose.yaml`，挂载 `data`、`skills`、`logs` named volumes。
- [ ] 安装 runtime CA、timezone、Chromium 和中文字体，以非 root 用户运行。
- [ ] 添加 Docker healthcheck、时区和单实例说明。
- [ ] 添加 Docker 部署、升级、备份、恢复和网络边界文档。
- [ ] 确认 build context 不包含本地 SQLite、WAL、日志、密钥或 `.env`。

Verification:

```bash
docker compose config
docker compose build
docker compose up -d
docker compose ps
curl -fsS http://127.0.0.1:18888/api/health
```

Persistence smoke test:

1. 写入一项配置、自选股和 skill。
2. `docker compose down` 后重新 `up`。
3. 确认三类数据仍存在。

## 6. Full quality gate

- [ ] 审查 diff 只包含 Web mode、全端去 VIP 和 Docker 部署相关改动。
- [ ] 运行 Go tests、frontend builds、Web build、Docker smoke test。
- [ ] 检查桌面默认入口和关键页面无回归。
- [ ] 检查无登录风险说明、单实例 SQLite 限制和 Volume 路径。
- [ ] 不 commit、不 push，交由用户检查本地 diff。

## Rollback points

- Web build 与桌面入口通过 build mode 隔离，可删除 Web entry/bridge 回退，不影响数据。
- VIP 删除若发现遗漏，回滚对应门禁补丁不会要求数据库迁移。
- Docker 部署失败时停止容器，Volume 保留，桌面版继续使用原数据目录副本。

