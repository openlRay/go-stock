# 飞书自定义机器人 Webhook 签名协议

本文记录 go-stock 对飞书自定义机器人 Webhook 的特定协议实现。它只适用于飞书集成，不属于 `.trellis/spec/backend/` 中的全局编码规范。

实现与回归证据：

- `backend/data/feishu_api.go`
- `backend/data/feishu_api_test.go`
- `backend/agent/feishu_bot.go`
- `backend/agent/feishu_bot_test.go`

## 配置语义

`FeishuSecret` 是自定义机器人的“签名校验 Secret”，不是飞书应用的 App Secret。

- Secret 为空：发送体不包含 `timestamp` 和 `sign`。
- Secret 非空：每次发送都必须在 POST 前即时生成新的 `timestamp` 和 `sign`。
- 设置页测试动作读取已持久化配置；修改 Webhook 或 Secret 后必须先保存再测试。

## 签名算法

```go
func generateFeishuSign(secret string, timestamp int64) string
func addFeishuSignature(message, secret string, now time.Time) (string, error)
```

签名字段满足以下协议：

```json
{
  "timestamp": "1599360473",
  "sign": "base64-hmac-sha256",
  "msg_type": "interactive"
}
```

1. `timestamp` 使用当前 Unix 秒并序列化为十进制字符串。
2. HMAC key 为 `timestamp + "\n" + secret`。
3. 待签名消息为空字节串。
4. 摘要算法为 SHA-256，结果使用标准 Base64。

签名只能在后端发送边界生成。浏览器不得生成、缓存或复用签名和时间戳。

## 两条发送路径

- `SendFeishuMessage` 接收 UI 测试、价格提醒等调用方提供的 JSON object。
- `SendToFeishu` 发送后端生成的 interactive card，并保留历史默认行为：在正文开头追加 `@所有人`。
- `SendToFeishuWithOptions` 供需要控制展示语义的后端调用方使用：`HeaderTemplate` 设置卡片头部颜色，`MentionAll` 决定是否追加 `@所有人`。Cron 完成通知必须显式使用 `MentionAll=false`，成功与失败分别使用绿色、红色 header template。

两条路径必须复用同一签名逻辑。Raw JSON 必须解析为 object；`null`、数组或非法 JSON 在联网前拒绝。调用方传入的旧 `timestamp` 和 `sign` 必须被当前值覆盖，其他消息字段保持不变。

卡片选项只属于飞书发送边界。策略筛选、定时任务等领域层继续生成渠道无关的 Markdown/PlainText，不得依赖 `FeishuCardOptions`。

## 错误与安全

| 场景 | 行为 |
| --- | --- |
| 未启用签名 | 原消息不增加签名字段。 |
| Secret 配置错误 | 保留飞书错误码和安全错误文案，不输出 Secret。 |
| 飞书返回 `19021` | 检查 Secret 来源和系统时钟；不要复用旧 timestamp。 |
| 本机或容器时间偏差超过飞书允许范围 | 修复时间同步，不通过延长复用时间规避。 |
| Raw JSON 不是 object | 本地拒绝，不发送请求。 |

日志不得包含 Webhook Secret、完整签名请求体或可复用的鉴权材料。

## 验证要求

- 固定时间和 Secret 必须得到确定的 timestamp/sign。
- 注入签名后原消息字段保持不变，旧签名被替换。
- 非法 JSON、`null` 和数组在联网前失败。
- 卡片构造测试必须覆盖 JSON 2.0 schema、header template、默认 mention-all 兼容行为和 `MentionAll=false`。
- `19021` 返回可执行的配置与时钟排查提示。
- 常规自动化测试不得运行会真实发送外部消息的 `TestSendToFeishu`；只有用户明确允许该副作用时才能运行。
