# 后端目录与依赖规范

## 适用范围

新增 Go 文件、拆分 service、增加 Wails/Web RPC、后台任务或跨平台实现时使用本规范。目录选择以职责和依赖方向为准，不以文件名相似或“先放进去再说”为准。

## 当前目录职责

| 位置 | 主要职责 | 新代码不得承担 |
| --- | --- | --- |
| 根目录 `package main` | Desktop/Web 入口、`App` 生命周期、Wails/Web 桥接、平台适配与调用编排 | 可脱离 UI 入口复用的持久化、外部数据源或 AI 核心逻辑 |
| `backend/data` | 外部数据源、共享 HTTP client、设置、领域 service 与持久化访问编排 | Wails window/dialog、Web route 或前端组件状态 |
| `backend/agent` | AI model/Agent 编排、对话、定时任务与 Agent 生命周期 | 浏览器 UI 适配和平台窗口行为 |
| `backend/agent/tools` | Agent tool schema、参数解析及对 data/service 的适配 | 重复实现 `backend/data` 已有的数据获取逻辑 |
| `backend/db` | SQLite/GORM 初始化、迁移和数据库级 helper | 外部 HTTP、UI 事件或模型调用 |
| `backend/models` | 跨层 DTO、持久化 model、事件 payload 和稳定枚举 | 数据库查询、网络请求和运行时副作用 |
| `backend/runtimepath` | Desktop/Web 不同运行根目录策略 | 业务目录名称和功能级路径拼装 |
| `backend/logger`、`backend/util`、`backend/machineid` | 无业务状态的基础能力 | 反向依赖 `main` 或具体业务入口 |
| `ai-assistant-web` | 独立 Web server package 与其命令入口 | Wails Desktop 生命周期 |
| `backend/cmd/*` | 独立命令行程序 | 隐式启动主应用或复用 `package main` 内部状态 |

源码证据：`go list ./...` 的实际 import 关系、`app_events.go`、`backend/data/app_ctx.go`、`backend/runtimepath/root_*.go`。

## 依赖方向

```text
main(App / Wails / Web bridge)
  -> backend/agent, backend/data, backend/db, backend/models
backend/agent
  -> backend/data, backend/db, backend/models, backend/runtimepath
backend/data
  -> backend/db, backend/models, backend/logger, backend/util
backend/db
  -> backend/models
基础 package
  -> 不反向依赖 main 或业务入口
```

- `package main` 是入口适配层。新增 RPC 应放在聚焦的 `app_<feature>.go`，只做输入边界、生命周期和 service 调用编排。
- 跨 RPC、Agent tool 或后台任务复用的逻辑应进入 `backend/data`、`backend/agent` 或更小的基础 package，不复制到多个 `app_*.go`。
- 跨持久化、事件和前端桥接共享的数据结构放入 `backend/models`；仅在单个 package 内使用的内部结构留在拥有者 package。
- `backend/agent/tools` 优先包装已有 service。只有 tool 专属的 schema、参数解释和流式输出属于 tool 层。
- 基础 package 不得为了方便引用根目录 `main`；需要事件或路径能力时使用已定义的窄接口或基础 package。

## 平台与 build tag 文件

- Desktop/Web 或操作系统行为不同时，使用同名函数加互斥 build tags，而不是在共享文件中到处判断 runtime。
- Desktop 通用实现使用 `//go:build !web`；Web 实现使用 `//go:build web`；Windows 桌面能力使用 `windows && !web`。
- 平台文件只包含平台差异，共享业务规则留在无 build tag 文件。
- 新增平台文件后必须验证目标 tag 能独立编译，并确认没有桌面 API 泄漏到 Web 构建。

参考：`main_desktop.go`、`main_web.go`、`app_web.go`、`update_helper_*`、`backend/agent/tools/streaming_shell_*`。

## 新功能放置顺序

1. 确定稳定输入、输出和持久化结构是否需要进入 `backend/models`。
2. 把外部请求、数据库或 AI 核心逻辑放入拥有该副作用的 backend package。
3. 需要 Agent 使用时，在 `backend/agent/tools` 增加薄适配层。
4. 最后在聚焦的 `app_<feature>.go` 暴露 RPC 并同步生成 binding。

历史 `app.go` 和 `backend/data` 较大是现状，不代表新逻辑应继续集中堆叠。本规范不要求为了目录整齐一次性搬迁历史代码；只在功能发生实质修改时沿依赖方向逐步收敛。

## 禁止模式

- 在 `App` 方法中同时完成参数解析、外部 HTTP、数据库事务和事件状态机。
- 为 Agent tool 复制一套已有行情或配置查询实现。
- `backend/models` 导入 GORM service、Resty client 或 Wails runtime。
- 基础 package 通过全局变量反向读取 `main.App`。
- 仅为缩短 import 路径创建没有清晰所有权的 `common`/`helper` 大杂烩 package。

## 验证

- 使用 `go list -deps` 或 `go list ./...` 检查新依赖方向。
- 新增/修改 build tag 时分别运行对应 tag 的定点 build/test。
- 移动文件时搜索原 package 符号、生成 binding 和所有平台实现，防止遗漏。
