# 外部 HTTP 集成规范

## 适用范围

新增或修改行情源、搜索、文件下载、Webhook、模型 HTTP 调用、代理设置和其他第三方网络请求时使用本规范。浏览器入站 RPC/SSE 规则由[运行时规范](./runtime-guidelines.md)负责。

## Client 所有权

`backend/data/httpclient.go` 是共享 transport、代理和默认超时的唯一配置入口：

- 普通请求复用 `data.SharedHTTPClient`，共享连接池和用户代理设置。
- 设置变化通过 `ConfigureFromSettings`、`UpdateHTTPClientProxy` 和 `DisableHTTPClientProxy` 更新共享 transport。
- 需要独立超时时使用 `CreateHTTPClientWithTimeout`；下载使用 `CreateDownloadClient` 或同等的专用 client。
- 新代码不得调用 `SharedHTTPClient.SetTimeout(...)` 做 request-local 超时，因为它会修改全局 Resty client 并影响并发请求。现有调用属于迁移债务，不作为范例复制。
- 不为每个请求创建默认 `http.Client`，否则会丢失共享 transport、代理、连接池和 TLS 配置。

## 输入与 URL 构造

- 外部输入在发出请求前完成 trim、格式校验和范围限制。
- query 使用 Resty `SetQueryParam(s)` 或 `url.Values`，不得用字符串拼接用户可控值。
- 路径 segment 使用 `url.PathEscape`，并先验证允许字符；对固定 host 下载不得接受调用方传入完整 URL。
- 排序字段、市场类型等枚举使用 allowlist；页码、page size、offset 和时间范围设置上限。
- 上游支持 HTTPS 时必须使用 HTTPS。保留 HTTP 只能基于已确认的上游限制，不能由用户输入降级协议。
- 接受可配置 base URL 时只允许明确支持的 scheme，并防止 path/query 注入；涉及敏感内网资源时额外执行 SSRF 边界设计。

参考：`backend/data/request_validation_test.go`、`backend/data/concept_detail_api.go`、`backend/data/announcement_ai_analysis.go`。

## 超时、重试与取消

- 每次请求必须有总超时或可取消 context；长下载可以关闭 client 总超时，但必须用 context、响应大小和阶段超时建立上界。
- 默认普通请求不自动重试。只有幂等操作且产品语义允许重复请求时才配置有限重试。
- POST、模型调用、消息发送和其他可能产生费用/副作用的请求不得隐式重试。
- redirect 策略属于安全边界。固定 host 或签名下载默认禁止 redirect；允许时必须限制次数并重新验证目标。
- retry 不能覆盖 context cancel，也不能把 4xx、格式错误或业务校验失败当作瞬时错误。

## 响应验证

- 网络成功不等于业务成功：分别检查 HTTP status、Content-Type/签名、body size、JSON 解码和上游业务 code。
- 下载在读取前建立最大字节数；不能先 `io.ReadAll` 无界 body 再检查长度。
- JSON 必须解码到 typed struct 或经过明确校验的 map；`null` 与预期 object/array 分开处理。
- 未知 HTML、脚本或 challenge 不得自动执行。只允许产品设计中明确识别、受限且可测试的协议分支。
- 错误 body 只保留有长度上限且脱敏的诊断摘要，不直接进入日志或用户消息。

## 鉴权、签名与 secret

- API Key、token、cookie、Webhook Secret 和签名只在后端集成边界使用，不返回前端生成或缓存。
- 多条发送路径必须复用同一个鉴权/签名 helper，不能只有其中一条带签名。
- timestamp、nonce 等一次性字段在发送前即时生成，不能跨请求复用。
- 日志和 error 不输出 Authorization header、完整带签名 body、代理凭据或 secret。
- 单一第三方的精确协议放入 `docs/integrations/`；spec 只保留跨集成通用规则。

飞书协议说明见[飞书自定义机器人 Webhook 签名协议](../../../docs/integrations/feishu-webhook.md)。

## 禁止模式

```go
// 错误：用户输入直接拼 query，且修改共享 client 的全局超时。
url := baseURL + "?code=" + code + "&sort=" + sort
resp, err := data.SharedHTTPClient.SetTimeout(timeout).R().Get(url)
```

```go
// 正确：先校验，再使用 request-local client 和结构化参数。
if !validCode(code) || !validSort(sort) {
    return nil, ErrInvalidRequest
}
client := data.CreateHTTPClientWithTimeout(timeout)
resp, err := client.R().SetQueryParams(map[string]string{
    "code": code,
    "sort": sort,
}).Get(baseURL)
```

## 测试与验证

- 使用 `httptest.Server` 断言 method、path、query、header、body 和请求次数。
- 非法输入测试必须证明 server 零请求，而不只断言空返回值。
- timeout/cancel 测试证明请求停止；retry 测试断言精确请求次数。
- 下载测试覆盖 redirect、非 2xx、Content-Type、签名、大小上限和截断 body。
- routine test 不访问 live 服务或发送真实消息；副作用测试需用户明确授权。
