# 后端全局规范重构设计

## 设计原则

规范按“未来修改代码时开发者会查什么”划分，而不是按本次功能或当前文件数量划分：

- `.trellis/spec/backend/`：跨功能、可重复执行的编码和运行约束。
- `.trellis/tasks/<task>/design.md`：单个需求的字段、RPC、状态机、校验矩阵和兼容决策。
- `docs/integrations/`：稳定但只属于一个外部系统的协议、签名和排障说明。
- Go 注释与测试：距离实现最近的非显然约束和可执行断言。

真实源码可以作为 spec 的证据，但不能因为引用了某个符号，就把该符号的全部实现复制进 spec。

## 目标结构

| 文件 | 所有权 | 主要源码证据 |
| --- | --- | --- |
| `directory-structure.md` | package 职责和依赖方向 | `app*.go`、`backend/{data,agent,db,models,runtimepath,logger,util}` |
| `database-guidelines.md` | SQLite/GORM 初始化、迁移、事务和测试隔离 | `backend/db/db.go`、`backend/db/chat_memory.go`、`backend/db/*_test.go` |
| `error-handling.md` | error 传播、包装、取消、用户安全消息和边界转换 | `main_web.go`、`backend/data/announcement_ai_analysis.go`、`backend/data/ai_config_service.go` |
| `logging-guidelines.md` | zap 入口、级别、上下文字段和敏感数据边界 | `backend/logger/lgo.go` 及现有调用点 |
| `quality-guidelines.md` | Go 测试 package、注释、并发/生命周期检查和风险验证 | 各 package 的 `_test.go`、build-tag tests |
| `http-integration-guidelines.md` | 共享 transport、结构化参数、校验、超时、重试、下载和 secret | `backend/data/httpclient.go`、`request_validation_test.go`、`feishu_api.go` |
| `ai-integration-guidelines.md` | 配置解析、能力校验、request-local 状态、结构化输出、stream/cancel/persist | `ai_config_service.go`、`ai_model_capabilities.go`、`announcement_ai_analysis.go`、`cron_schedule_ai.go` |
| `runtime-guidelines.md` | Desktop/Web build tags、RPC allowlist、事件桥、runtime root 和 platform 隔离 | `main_{desktop,web}.go`、`app_events.go`、`web_server.go`、`backend/runtimepath` |
| `deployment-guidelines.md` | Docker build/runtime、非 root、卷、SQLite WAL、health 与 graceful shutdown | `Dockerfile`、`compose.yaml`、`main_web.go` |

## 依赖与目录规则

全局依赖方向描述当前可执行边界：

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

现有 `main` 和 `backend/data` 文件较大是历史事实。规范不声称它们已经理想分层，也不要求本任务重构；新代码应按职责选择既有 package，避免继续把平台桥接、持久化和外部请求混在同一个函数中。

## 内容迁移表

| 旧文件 | 迁移目标 | 处理方式 |
| --- | --- | --- |
| `ai-model-configuration.md` | AI 配置 task design + `ai-integration-guidelines.md` | 保留功能字段和 CRUD 决策到 task；提炼通用能力/secret/request-local 规则；删除旧 spec。 |
| `announcement-ai-analysis.md` | 公告 task design + AI/错误/质量规范 | 合并 working tree 中 challenge 决策；提炼取消、过期响应和完整成功后持久化；删除旧 spec。 |
| `feishu-webhook.md` | `docs/integrations/feishu-webhook.md` + HTTP/质量规范 | 协议原样保真迁移；提炼共享签名边界与副作用测试规则；删除旧 spec。 |
| `web-runtime.md` | runtime、HTTP、deployment 三份规范 | 去除重复叙述，保留跨模式不变量、环境变量和验证入口；删除旧 spec。 |

## 写作约束

- 每份 spec 以“适用范围 → 项目规则 → 证据 → 禁止模式 → 验证”组织。
- 真实文件路径和符号只作为证据，不复制大段源码。
- 只把已有模式写成“当前规则”；发现明显历史反例时，说明“新代码必须”并把旧代码列为迁移债务，不能谎称全仓已经满足。
- 测试命令按影响范围给出，文档改动本身只做文档检查。
- backend spec 统一中文，不翻译代码、路径、命令和环境变量。

## 并行 WIP 处理

- `announcement-ai-analysis.md` 和对应 task 当前有未提交修改；迁移时读取 working tree 版本，并通过增量 patch 把缺失决策合入 task design。
- 不使用 checkout/reset 恢复任何文件，不格式化无关内容。
- 删除旧 spec 前先用 `rg` 找全引用；迁移后再次检查断链。

## 回滚

本任务只修改 Markdown。若结构不合适，可以从 Git 历史或迁入后的 task/docs 恢复旧文档，不影响生产代码、数据库或构建产物。
