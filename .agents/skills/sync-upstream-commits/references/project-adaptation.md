# go-stock 上游合并适配参考

每次 merge 后必须执行“强制 Web 兼容审查”。其他部分仅在上游或 fork 改动触及
相关路径时加载。先比较共同祖先到目标 SHA 和上游 SHA 的变化，再决定额外适配。

## 强制 Web 兼容审查

对本轮完整 merge diff 及其调用链逐项确认：

1. 检查所有新增/修改 Go 文件的 build tags 和入口可达性，确认 `web` 构建不会引用
   Wails 窗口、托盘、对话框、退出、自更新或仅桌面可用的实现。
2. 检查新增/修改的 `App` 方法、`frontend/wailsjs/go/main/App.js`、
   `frontend/src/web-bridge.js` 和 Web RPC 允许列表，保证参数、返回、错误和事件一致。
3. 检查业务事件通过共享 emitter 同时到达 Wails 与 SSE；Web 请求继续遵守同源检查，
   Vite `/api` 代理保持 `changeOrigin: false`。
4. 检查 Docker 仍以 Web 模式启动，浏览器解析顺序保持
   `Settings.BrowserPath > CHROME_BIN > CheckBrowser()`，数据/技能/日志目录继续持久化。
5. 从合并后的源重新生成绑定或静态资源，不手改生成物。

每次至少运行：

```bash
go test ./...
go test -tags web ./...
go build .
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags web .
npm --prefix frontend run build
```

任何新增失败、Web 路径进入桌面能力、RPC/事件不一致或无法重生成的产物都必须阻断。

## 检查矩阵

| 子系统 | 重点路径 | 必须保护的本地契约 | 建议验证 | 阻断条件 |
| --- | --- | --- | --- | --- |
| Web/桌面双模式 | `main*.go`、`app*.go`、`web_server*.go` | 桌面默认入口保持可用；`web` build tag 只启用无 GUI 服务；RPC、SSE、同源检查和事件边界不回归 | `go build .`；`go test -tags web .`；`GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags web .` | Web 路径调用 Wails 窗口、托盘、对话框、退出或更新能力；无法同时编译两种模式 |
| Docker 与运行时 | `Dockerfile`、`compose.yaml`、`scripts/dev-web.sh`、`frontend/src/runtime-env.js` | 容器继续以 Web 模式运行；Node 22、Go 1.26、Debian bookworm、4 GB Node heap、UID/GID 10001 和持久化目录契约不丢失 | `docker compose config`；静态核对启动命令；Docker 可用时执行 smoke | 上游改动覆盖本地 Web 启动方式、代理配置、端口、用户身份或数据持久化策略且无法兼容 |
| Agent 与沙箱工具 | `backend/agent/**`、`backend/agent/tools/**`、`backend/data/tool_prompt_template.go` | 保留工具分组、沙箱根目录、流式 shell、跨平台 build-tag 文件和错误边界 | 运行相关 Go tests；核对 Unix/Windows 文件配对和路径根 | 工具可以越过沙箱根目录、平台实现缺失、上游 Agent 协议与本地工具契约无法同时成立 |
| Web/Wails 前端桥接 | `frontend/src/web-bridge.js`、`frontend/src/App.vue`、`frontend/src/main.js`、`frontend/wailsjs/**` | Web 与 Wails 调用保持等价；事件和错误能传回 UI；生成绑定来自 Go 接口 | `npm --prefix frontend run build`；比较 Go 方法、桥接层与生成绑定 | 需要手工修改生成绑定、Web/Wails 只能保留一条路径或事件语义无法确定 |
| AI Web 前端 | `ai-assistant-web/frontend/**`、`ai-assistant-web/static/**` | TypeScript/Vite 源码可构建；静态产物只能由源码生成 | `npm --prefix ai-assistant-web/frontend run build`；核对源与静态产物 | 只有压缩产物发生冲突，找不到对应源码或可重复构建命令 |
| 依赖与生成链路 | `go.mod`、`go.sum`、`frontend/package*.json`、`ai-assistant-web/frontend/package*.json`、`frontend/wailsjs/**` | Go/Wails/Node 版本约束兼容；声明与 lockfile 一致；生成物可追溯 | `go mod verify`；受影响前端 build；项目支持的 Wails 生成/构建命令 | 依赖要求互斥、校验失败、lockfile 无法由声明重建或生成命令未知 |
| Trellis 与 AI 平台 | `.trellis/**`、`.agents/**`、`.claude/**`、`.codex/**`、`AGENTS.md` | 保留当前 Trellis 生命周期、Codex/Claude 技能发现和用户已有配置；上游不能覆盖本地工作流 | 校验任务/spec 文件；`quick_validate.py`；`readlink`/`realpath`；精确 pathspec 的 Git diff | 上游文件会覆盖本地 AI 工作流、符号链接失效或需要改动用户未授权的 `.codex/config.toml` |

## 跨层审查方法

遇到上述路径时，不要只看冲突文件：

1. 从共同祖先分别读取 fork 与上游的提交说明和路径差异。
2. 沿调用链检查配置输入、后端行为、桥接/序列化和前端消费是否仍一致。
3. 将冲突解决放入 merge commit；只有非冲突兼容工作才使用后续适配 commit。
4. 记录每个重要选择保留了哪一侧意图、为何兼容，以及用什么命令验证。

## 生成物与二进制

出现生成物或二进制冲突时：

1. 找到权威源文件；
2. 找到仓库内已有生成命令和所需版本；
3. 先合并源文件，再重新生成；
4. 审查生成 diff 是否只反映源变化。

任一步无法满足就停止，不得直接选择某一侧产物或手工编辑压缩内容。
