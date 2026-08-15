# 日志规范

## 适用范围

新增后端日志、修改错误链、后台任务、外部集成或启动/关闭流程时使用本规范。

## 统一入口

运行期日志统一使用 `backend/logger` 暴露的 `Logger` 或 `SugaredLogger`。`backend/logger/lgo.go` 使用 zap，并把低于 Error 的日志写入 `logs/info.log`、Error 及以上写入 `logs/error.log`，同时输出控制台，文件由 lumberjack 轮转。

- package 初始化和正常 service 不自行创建 logger。
- 标准库 `log` 只允许在统一 logger 尚未可用的早期启动失败边界使用。
- `fmt.Printf` 不作为运行期日志；返回给 CLI 的显式输出除外。
- library/service 不调用 `Fatal`/`Panic`。只有 process entrypoint 在无法继续启动时可以终止进程。

## 级别选择

| 级别 | 使用场景 |
| --- | --- |
| Debug | 高频诊断、分支选择、仅开发时需要的细节 |
| Info | 启动完成、模式选择、长期任务关键阶段、成功的配置切换 |
| Warn | 已回退或可继续运行的异常，如能力降级、可恢复数据缺失 |
| Error | 当前操作失败、需要用户或运维介入，但进程仍可继续 |
| Fatal | entrypoint 无法建立数据库、监听端口或核心运行环境 |

不要为了“方便看见”把正常请求逐条记为 Error，也不要把真实失败降为 Info。

## 上下文与去重

- 日志说明动作、阶段和稳定标识，例如 request ID、业务 ID、HTTP status、model name、耗时或响应大小。
- 错误只在最能补充上下文且拥有处理决策的层记录一次。上层只是原样返回时不要重复记录。
- 并发/异步流程必须包含 request ID 或等价关联标识，避免多条 stream/任务日志无法区分。
- 高频循环和轮询只记录状态变化、聚合结果或限频后的异常，避免日志洪水。
- 不记录无法执行的泛化提示；Warn/Error 应让维护者知道失败在哪个边界。

## 敏感信息

任何级别都不得记录：

- API Key、token、password、Webhook Secret、Authorization header；
- 代理 URL 中的用户名密码；
- 完整 prompt、模型原始响应、公告全文、cookie 或签名请求体；
- 数据库中可能包含用户隐私的完整记录。

允许记录经过校验的 host、model name、配置 ID、body size、token estimate 和脱敏错误摘要。第三方响应仅截取有上限且确认不含 secret 的片段。

## 路径与生命周期

- 当前 logger 使用 `./logs`，实际相对 process cwd。Web 通过 `WORKDIR /app` 和 `/app/logs` volume 保证位置稳定；Desktop 尚未统一改用 `runtimepath.RootDir()`，这是现有迁移债务，规范不得把它描述为已经解决。
- 新增独立进程或命令时，先明确 cwd/运行根目录并创建日志目录，再初始化会写文件的 logger。
- 正常 shutdown 应 flush/sync 可控日志资源；不要因为 `Sync` 在某些终端返回无害错误而掩盖主流程错误。
- 测试不得写入生产 `logs` volume；需要断言日志时注入 test core 或捕获输出。

## 禁止模式

- `logger.Errorf("request: %s", rawBody)` 输出完整第三方 body。
- 在多个调用层重复记录同一个 stack/error。
- 用日志代替向调用方返回 error。
- 在 package init 中记录依赖尚未建立的业务状态。
- 后台 goroutine 既不返回错误、也不发事件或日志。

## 代码证据

- Logger 配置与轮转：`backend/logger/lgo.go`。
- Web 入口启动/关闭日志：`main_web.go`。
- request-ID 异步流程：`app_announcement_ai.go`、`backend/data/announcement_ai_analysis.go`。

## 验证

- diff review 搜索新增 `fmt.Print*`、`log.Fatal*` 和包含 secret 字段的日志。
- 外部请求/AI 失败测试检查返回值，不依赖完整日志文案。
- 修改日志路径、cwd 或 Docker volume 时执行运行时路径、部署配置和写权限检查。
