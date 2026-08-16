# 重构后端全局规范

## 目标

重新梳理 `.trellis/spec/backend/`，使其只沉淀适用于多个 package 或功能的全局可执行规则，并与单个功能的实现契约、第三方协议说明分离。最终规范统一使用简体中文，由当前 Go/Wails 代码和测试提供证据，不保留模板占位内容。

## 背景

当前 backend spec 同时存在三类问题：

- `directory-structure.md`、`database-guidelines.md`、`error-handling.md`、`logging-guidelines.md` 仍是空模板。
- `ai-model-configuration.md`、`announcement-ai-analysis.md`、`feishu-webhook.md` 记录单个功能或单个第三方协议，不属于全局编码规范。
- `web-runtime.md` 超过 300 行，把 Wails/Web 构建边界、RPC、事件、运行路径、外部 HTTP、Docker 和部署参数混在一个文件中。

代码库的实际后端边界为：根目录 `main` package 负责 Wails/Web 入口和前端桥接；`backend/data` 负责外部数据源、设置与业务 service；`backend/agent` 负责 AI Agent、模型和定时任务；`backend/db` 与 `backend/models` 负责 SQLite/GORM；`backend/runtimepath` 负责构建模式相关运行根目录；`backend/logger`、`backend/util`、`backend/machineid` 提供基础能力。

## 需求

### R1：规范内容边界

- backend spec 只记录能够跨 package 或跨功能复用的约束。
- 可以引用真实文件、符号和测试作为规则证据，但不得把某个功能的完整 RPC、字段列表、校验矩阵或 UI 流程写成全局规范。
- 单功能契约进入对应 `.trellis/tasks/<task>/design.md`；稳定的第三方协议说明进入 `docs/integrations/`。

### R2：目标规范文件集

保留并重写：

- `index.md`
- `directory-structure.md`
- `database-guidelines.md`
- `error-handling.md`
- `logging-guidelines.md`
- `quality-guidelines.md`

新增：

- `http-integration-guidelines.md`：共享 HTTP client、参数校验、超时、重试、响应边界和 secret 保护。
- `ai-integration-guidelines.md`：模型配置解析、request-local 覆盖、能力校验、结构化输出、流式与取消边界。
- `runtime-guidelines.md`：Wails/Web build tags、App/RPC、事件、运行目录和平台能力边界。
- `deployment-guidelines.md`：Docker 构建、非 root 运行、SQLite 目录持久化、health/shutdown 和可替换镜像源。

### R3：旧文件迁移

- `ai-model-configuration.md` 的功能契约迁入 `08-12-ai-model-config-redesign/design.md`，全局 AI 经验提炼到 AI 规范，然后删除旧 spec。
- `announcement-ai-analysis.md` 的当前有效内容迁入 `08-12-announcement-ai-analysis/design.md`，保留工作树中已有的 challenge/WIP 决策；全局取消、latest-wins、完整成功后持久化等规则提炼到 AI、错误和质量规范，然后删除旧 spec。
- `feishu-webhook.md` 的签名协议迁入 `docs/integrations/feishu-webhook.md`；共享发送边界、secret 脱敏和有副作用集成测试规则提炼到 HTTP 与质量规范，然后删除旧 spec。
- `web-runtime.md` 的内容按运行时、HTTP 与部署职责拆分后删除。

### R4：语言与可执行性

- 所有新增或修改的 backend spec 使用简体中文；技术术语、代码标识符、环境变量和命令保持原文。
- 每份规范说明适用范围、项目内规则、代码证据、禁止模式和最小验证方式。
- 删除 `To be filled`、`TBD`、占位标题和没有项目证据的通用模板文本。

### R5：修改边界

- 本任务只修改 `.trellis/spec/backend/`、对应既有 task 设计文档、当前任务文档和 `docs/integrations/feishu-webhook.md`。
- 不修改 Go、Vue、Docker、Compose、CI 或生成绑定。
- 不覆盖公告分析、市场新闻等并行工作树改动；迁移时以当前 working tree 内容为准。
- 不 stage、不 commit、不清理无关 WIP。

## 验收标准

- [x] backend spec 索引与最终文件集完全一致，不再列出已迁出的功能契约。
- [x] 4 个模板规范已由真实代码模式重写，且不存在占位文本。
- [x] 新增 HTTP、AI、运行时和部署规范，规则分别引用至少一个真实源码或测试证据。
- [x] 4 个旧 spec 已完成内容迁移并删除，迁移后没有内部链接指向不存在文件。
- [x] 全局 spec 不包含单功能完整 RPC/字段/校验矩阵；实现细节只存在 task 或 integration doc。
- [x] 当前公告分析 spec 的未提交 challenge 决策没有丢失。
- [x] 所有新增或修改的 backend spec 使用简体中文。
- [x] `git diff --check`、占位文本扫描、Markdown 相对链接检查和引用文件存在性检查通过。
- [x] 最终报告明确这是文档重构，未把静态检查描述成代码测试。

## 不在范围内

- 修复或统一现有后端代码中的历史不一致。
- 调整 package、移动 Go 文件或修改对外 API。
- 为已有功能补写新的产品需求。
- 运行全量 Go、前端或浏览器自动化测试。
