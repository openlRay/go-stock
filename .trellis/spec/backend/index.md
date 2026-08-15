# 后端开发规范

## 规范边界

本目录只沉淀跨 package、跨功能复用的后端规则。真实源码和测试可以作为证据，但单个功能的 RPC、字段、状态机和校验矩阵应进入对应 task 的 `design.md`；单一第三方协议进入 `docs/integrations/`。

所有新增或修改的规范使用简体中文，技术术语、代码标识符、环境变量和命令保持原文。

## 规范索引

| 规范 | 适用主题 |
| --- | --- |
| [目录与依赖](./directory-structure.md) | package 职责、依赖方向、build tag 文件放置 |
| [数据库与迁移](./database-guidelines.md) | SQLite/GORM、migration、transaction、测试隔离 |
| [错误处理](./error-handling.md) | error chain、typed error、取消、RPC/HTTP 边界 |
| [日志](./logging-guidelines.md) | zap 入口、级别、上下文、敏感信息 |
| [质量与测试](./quality-guidelines.md) | Go 测试 package、注释、并发与按风险验证 |
| [外部 HTTP 集成](./http-integration-guidelines.md) | shared client、参数校验、timeout、retry、下载和 secret |
| [AI 集成](./ai-integration-guidelines.md) | capability、request-local config、结构化输出、stream/cancel |
| [Wails 与 Web 运行时](./runtime-guidelines.md) | App/RPC、事件、build tag、runtime root、平台能力 |
| [Web 部署与持久化](./deployment-guidelines.md) | Docker、非 root、volume、health 和 shutdown |

## 开发前检查

根据修改范围读取对应规范，并至少确认：

1. 新代码属于哪个 package，是否遵守既有依赖方向？
2. 输入在哪个边界完成 validation，error 由哪一层转换为用户消息？
3. 是否涉及数据库、外部 HTTP、AI、Desktop/Web 或部署约束？
4. 是否有 request-local 状态、cancel、transaction 或 latest-wins 等生命周期要求？
5. 最小验证范围能否覆盖当前 diff 的真实风险？

## 质量检查

- 规则引用真实源码或测试，不保留模板占位内容。
- 不把历史偶然写法描述成推荐模式；存在债务时明确说明“新代码必须”。
- 不复制单功能实现契约到全局 spec。
- 不运行无关全量测试，不把 build 或静态检查夸大为运行时回归。
- 共享工作树中保留无关 staged/unstaged/untracked WIP。
