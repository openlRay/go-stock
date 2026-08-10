# Feishu Custom Bot Webhook Signing

## Scenario: Signed custom-bot notifications

### 1. Scope / Trigger

Use this contract whenever code sends a message to `Settings.FeishuRobot`, changes `Settings.FeishuSecret`, adds a notification entry point, or exposes a Feishu test action. It prevents Feishu error `19021` caused by missing, stale, mismatched, or incorrectly sourced signatures.

### 2. Signatures

```go
func (FeishuAPI) SendFeishuMessage(message string) string
func (FeishuAPI) SendToFeishu(title, message string) string
func genFeishuSign(secret string, timestamp int64) string
func newFeishuSignature(secret string, now time.Time) (timestamp, sign string)
func addFeishuSignature(message, secret string, now time.Time) (string, error)
```

`SendFeishuMessage` owns arbitrary JSON payloads used by UI tests and price alerts. `SendToFeishu` owns generated interactive cards. Both paths must apply the same signing contract.

### 3. Contracts

- `FeishuSecret` is the custom bot's **signature verification Secret**, not the Feishu application App Secret.
- An empty `FeishuSecret` omits `timestamp` and `sign`.
- A non-empty `FeishuSecret` requires both fields on every outbound message:

```json
{"timestamp":"1599360473","sign":"base64-hmac-sha256","msg_type":"interactive"}
```

- `timestamp` is the current Unix time in seconds, serialized as a decimal string.
- The HMAC key is `timestamp + "\n" + secret`; the signed message is empty; the digest is SHA-256 and the result is standard Base64.
- The backend generates the signature immediately before the POST. Frontend code must not generate or cache it.
- Raw JSON messages must be objects. The backend overwrites any supplied `timestamp` or `sign` so stale caller values cannot escape.
- Settings-page test actions use persisted settings. After changing the webhook or Secret, save settings before sending a test.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| Signing disabled | Send the message without `timestamp` or `sign` |
| Signing enabled for a generated card | Add a fresh timestamp and signature before POST |
| Signing enabled for a raw JSON message | Parse the object, preserve message fields, and overwrite timestamp/sign |
| Raw message is invalid JSON, `null`, or an array | Reject locally; do not contact Feishu |
| Feishu returns `19021` | Preserve the upstream code/message and explain Secret source and system-clock checks |
| Local time differs from Feishu by more than one hour | Fix host/container time synchronization; do not reuse an older timestamp |
| Secret belongs to another bot or is an App Secret | Treat as configuration mismatch; never log the Secret |

### 5. Good / Base / Bad Cases

- Good: every signed send path calls the shared signature helper with `time.Now()` immediately before POST.
- Good: a raw payload containing stale signature fields is rewritten with fresh values.
- Base: a robot without signature verification sends the existing JSON unchanged.
- Bad: only generated cards are signed while UI tests and alerts post unsigned raw JSON.
- Bad: the browser generates a signature or stores a timestamp for reuse.
- Bad: logs include the webhook Secret or the complete signed request body.

### 6. Tests Required

- Unit-test that a fixed time and Secret produce the expected timestamp/sign fields.
- Unit-test that existing raw payload content survives signature injection.
- Unit-test that stale `timestamp` and `sign` fields are replaced.
- Unit-test that invalid JSON, `null`, and arrays are rejected before networking.
- Unit-test that error `19021` includes actionable configuration guidance.
- Do not run the live `TestSendToFeishu` integration test as part of routine checks; it sends an external message. Run it only when that side effect is explicitly intended.

### 7. Wrong vs Correct

#### Wrong

```go
SharedHTTPClient.R().SetBody(message).Post(cfg.FeishuRobot)
```

This bypasses signing for raw-message callers even when the robot requires signature verification.

#### Correct

```go
requestBody := message
if secret := strings.TrimSpace(cfg.FeishuSecret); secret != "" {
    requestBody, err = addFeishuSignature(message, secret, time.Now())
    if err != nil {
        return "发送飞书消息失败: " + err.Error()
    }
}
SharedHTTPClient.R().SetBody(requestBody).Post(cfg.FeishuRobot)
```

Keep signature generation at the backend integration boundary so desktop, Web, tests, alerts, and agent tools share the same behavior.
