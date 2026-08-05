# Technical Design

## Architecture

采用同一仓库、两种运行模式：

```text
Desktop build
Vue -> generated Wails bindings -> Wails App -> backend services -> SQLite

Web build
Vue -> Web compatibility bridge -> JSON RPC / SSE -> App/service layer -> SQLite
```

Web build 通过独立 build tag/entrypoint 启动 HTTP Server，并排除 Linux Wails 平台实现，避免当前 `app.go`/`app_linux.go` 重复定义。共享的 embeds、版本变量和初始化逻辑从桌面 `main` 生命周期中拆到平台无关文件；桌面行为保持原入口。

## Frontend compatibility bridge

### RPC proxy

现有生成 binding 只在函数执行时读取 `window.go.main.App.<Method>`，因此不修改生成文件。Vue 启动前：

1. 检测真实 Wails runtime；存在时保持现状。
2. 浏览器模式下安装 `window.go.main.App` Proxy。
3. 方法调用发送 `POST /api/rpc`：

```json
{
  "method": "GetStockList",
  "args": ["keyword"]
}
```

响应统一为：

```json
{"result": {}, "error": null}
```

服务端根据公开方法签名解码参数、捕获 panic 并序列化返回值。允许列表来自当前 Wails binding 的导出方法清单，避免任意反射调用非前端 API。

### Runtime adapter

- `EventsOnMultiple`、`EventsOn`、`EventsOff`：维护浏览器本地 subscriber registry。
- 后端推送：使用单向 SSE `/api/events`；消息为 `{name, data}`。
- `EventsEmit`：先派发浏览器本地事件；确需通知后端的事件由现有 RPC 保存路径承接，不创建通用匿名远端事件注入。
- `BrowserOpenURL`、Clipboard、reload、Environment：映射浏览器 API。
- 窗口尺寸、托盘、Quit、管理员重启：Web 模式 no-op 或返回明确的 Web 环境结果，不能影响服务进程。
- 文件导入导出：需要客户端内容的调用逐项切换为 upload/download，不把浏览器本地路径传给容器。

## Backend event boundary

新增最小事件发送边界：

```go
type EventEmitter interface {
    Emit(name string, data ...any)
}
```

- Desktop emitter 调用 `runtime.EventsEmit`。
- Web emitter 向 SSE hub 广播。
- `App` 流式 AI、行情、新闻、加载状态等活动路径改用该边界。
- 仍属于窗口生命周期的调用留在桌面平台文件，不进入 Web 初始化。

不为单次调用创建通用 service abstraction；仅提取 Web/desktop 必须共享或隔离的边界。

## Web lifecycle

Web Server 启动顺序：

1. 创建 `data`、`logs`、`skills` 运行目录。
2. 初始化 logger、machine ID/build metadata、SQLite、sentiment analyzer 和 settings。
3. 创建共享 App/service 状态以及 scheduler/cron。
4. 注册 `/api/health`、`/api/rpc`、`/api/events`。
5. 从 embedded `frontend/dist` 提供 SPA；未知非 API 路径回退 `index.html`。
6. 监听配置地址，处理 SIGTERM，停止 cron、HTTP server 和数据库相关后台任务。

## VIP removal

权限删除按“删除门禁而非伪造 VIP”等级处理：

- 前端删除 VIP state、加载、判断、modal、tooltip 和 disabled 条件。
- 后端删除 `requireVip2`、关注数量限制和功能入口前的 `EffectiveSponsorVipLevel` 判断。
- Sponsor 解密、字段和历史数据可暂时保留以兼容数据库，但不参与权限。
- 关于页只保留静态支持信息；更新逻辑始终具有公开 fallback，不再把赞助状态当成功能开关。

## Persistence

容器工作目录固定，挂载目录：

```text
/app/data    -> go-stock-data
/app/skills  -> go-stock-skills
/app/logs    -> go-stock-logs
```

挂载目录而非单文件，保证 SQLite WAL sidecar 和后续运行文件一起持久化。首期仅运行一个 application replica；不支持多个容器同时写同一 SQLite volume。

数据库备份文档使用 SQLite online backup/VACUUM INTO 语义，禁止把运行中的单个 `stock.db` 复制作为一致性备份。

## Docker image

Multi-stage：

1. Node 22 builder 安装依赖并构建 Vue。
2. Go 1.26 builder 以 Web build tag 编译 Linux binary。
3. Debian slim runtime 安装 `ca-certificates`、`tzdata`、Chromium 和中文字体，使用非 root 用户启动。

`.dockerignore` 排除 `data/*.db*`、`logs`、`node_modules`、构建产物、`.env` 和 Git/Trellis 工作文件。

## Compatibility and risks

- 无登录意味着任何能访问端口的人都能修改配置、调用 AI 和运行 Agent 能力；默认文档要求仅绑定可信网络，不通过 CORS 开放跨站调用。
- Agent shell/文件系统运行在容器内；Compose 不挂载 Docker socket、宿主机根目录或个人目录。
- 桌面 OS 能力在 Web 中只能适配或降级，不作为业务功能门禁。
- 通用 RPC 的最大风险是少数 App 方法隐含 Wails context；实现时用测试和调用清单识别，必要时为这些方法提供 Web handler override。
- 回滚方式是停止 Web 容器并继续使用原桌面入口；数据 schema 不做破坏性迁移。

