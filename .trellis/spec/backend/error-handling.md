# 错误处理规范

## 适用范围

新增或修改 service、RPC、后台 goroutine、外部请求、数据库操作、流式处理和 shutdown 流程时使用本规范。

## 分层职责

- 底层 helper 返回原始 error 或使用 `%w` 增加动作上下文，不决定 UI 文案。
- data/agent service 把依赖错误归类为领域可理解的失败，并保留 `errors.Is`/`errors.As` 链。
- `App`、HTTP/RPC handler 和事件边界将内部错误转换为稳定、用户安全的中文消息或 typed error code。
- 日志记录诊断上下文；返回值描述调用方下一步。两者不得通过暴露 secret 或完整上游 body 互相替代。

```go
// 正确：保留 cause，边界仍可识别取消或特定错误。
return fmt.Errorf("下载公告失败: %w", err)
```

不要使用 `fmt.Errorf("... %s", err)` 或重新 `errors.New(err.Error())` 破坏错误链。

## 可分类错误

以下情况使用 `errors.Is`/`errors.As` 或 typed error：

- `context.Canceled`、`context.DeadlineExceeded`；
- `gorm.ErrRecordNotFound`；
- 需要通过事件/RPC 暴露稳定 error code 的异步流程；
- 调用方需要区分“配置不存在”“输入非法”“外部失败”“保存失败”等恢复动作。

Typed error 包含稳定 code、用户消息和不序列化的 cause。不要让调用方解析可变中文字符串来判断类别。参考：`backend/models/announcement_ai_analysis.go` 的 error type。

## 取消、超时与 goroutine

- 所有可能阻塞的网络、AI、浏览器和长任务都应接收或派生 context。
- 取消是独立结果，不记录为普通 provider failure，也不得继续写数据库或发送 completed 事件。
- latest-request-wins 流程必须在最终副作用前再次核对 request ID/active state；仅在启动时检查不够。
- goroutine 内的 error 不能被静默丢弃。由 owner 选择返回 channel、typed event 或带上下文日志。
- shutdown 使用有界 context；超时错误向入口返回，不能无限等待。

参考：`main_web.go` 的 graceful shutdown、`app_announcement_ai.go` 的 cancel/request-ID 检查。

## RPC 与 HTTP 边界

- 输入格式、方法不存在、参数数量/类型错误分别使用稳定状态码或 RPC error，不统一伪装成成功空结果。
- Web 反射调用只允许 binding allowlist 中的方法，并在 transport 边界 recover panic；普通 service 不使用 recover 隐藏 bug。
- 未找到数据如果是正常业务状态，可以返回 nil/空集合；如果调用方必须区分配置错误或数据源失败，必须返回 error。
- 上游非 2xx、解析失败和业务 code 失败分别保留诊断上下文，不能只返回“请求失败”。

参考：`web_server.go`、`web_server_test.go`、`backend/data/iwencai_api.go`。

## 安全错误信息

错误和日志不得包含：

- API Key、token、Webhook Secret、Authorization header；
- 完整模型响应、完整公告正文或可能含隐私的 prompt；
- 可复用签名、cookie 或代理凭据；
- 未清理的第三方 HTML/脚本。

可以记录经过验证的业务 ID、HTTP status、响应大小、阶段、耗时和 model name。需要排障的上游 body 只记录受限、脱敏摘要。

## 禁止模式

- `if err != nil { return nil }` 把失败伪装成空数据。
- 在 library/service 中调用 `log.Fatal`、`panic` 或 `os.Exit`。
- 每一层都记录同一个 error，产生多条无新信息日志。
- 取消后继续 upsert，或先发 completed 再执行可能失败的保存。
- 通过字符串包含关系判断 error 类别。
- 将底层 GORM/HTTP error 原文直接展示给前端。

## 验证

- 错误分支测试同时断言可观察类别和“没有发生不应发生的副作用”。
- 取消测试断言网络/stream 停止、旧数据保留且不发送 completed。
- transport 测试覆盖非法 Content-Type、未知方法、参数错误、panic recovery 和同源拒绝。
- 安全测试或 diff review 检查 secret 不进入 error、日志和事件 payload。
