package data

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go-stock/backend/logger"

	"github.com/duke-git/lancet/v2/strutil"
	"github.com/go-resty/resty/v2"
	"github.com/tidwall/gjson"
)

// @Author spark
// @Date 2026/07/05
// @Desc 飞书自定义机器人 webhook 推送
// 文档：https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot
//-----------------------------------------------------------------------------------

type FeishuAPI struct {
	client *resty.Client
}

func NewFeishuAPI() *FeishuAPI {
	return &FeishuAPI{
		client: SharedHTTPClient,
	}
}

// SendFeishuMessage POST 原始 message 体到飞书 webhook（对齐 SendDingDingMessage，供前端测试/原始发送）。
// 启用签名校验时必须在后端注入当前 timestamp/sign，避免调用方复用过期签名或遗漏签名字段。
func (FeishuAPI) SendFeishuMessage(message string) string {
	cfg := GetSettingConfig()
	if cfg == nil || !cfg.FeishuPushEnable {
		return "飞书推送未开启"
	}
	if strings.TrimSpace(cfg.FeishuRobot) == "" {
		return "飞书推送未配置机器人地址"
	}
	requestBody := message
	if secret := strings.TrimSpace(cfg.FeishuSecret); secret != "" {
		var err error
		requestBody, err = addFeishuSignature(message, secret, time.Now())
		if err != nil {
			logger.SugaredLogger.Errorf("sign feishu message error: %v", err)
			return "发送飞书消息失败: " + err.Error()
		}
	}
	resp, err := SharedHTTPClient.R().
		SetHeader("Content-Type", "application/json").
		SetBody(requestBody).
		Post(cfg.FeishuRobot)
	if err != nil {
		logger.SugaredLogger.Error(err.Error())
		return "发送飞书消息失败"
	}
	logger.SugaredLogger.Infof("send feishu message: %s", resp.String())
	return parseFeishuResponse(resp.String())
}

// FeishuCardOptions 控制 interactive 卡片的渠道专属展示能力。
type FeishuCardOptions struct {
	HeaderTemplate string
	MentionAll     bool
}

// SendToFeishu 保留历史行为：构造 interactive 卡片并 @所有人。
func (f FeishuAPI) SendToFeishu(title, message string) string {
	return f.SendToFeishuWithOptions(title, message, FeishuCardOptions{MentionAll: true})
}

// SendToFeishuWithOptions 构造可配置 header 颜色和 @所有人行为的 interactive 卡片。
func (f FeishuAPI) SendToFeishuWithOptions(title, message string, options FeishuCardOptions) string {
	cfg := GetSettingConfig()
	if cfg == nil || !cfg.FeishuPushEnable {
		return "飞书推送未开启"
	}
	if strings.TrimSpace(cfg.FeishuRobot) == "" {
		return "飞书推送未配置机器人地址"
	}

	body := buildFeishuCardMessage(title, message, options)

	// 可选签名校验：FeishuSecret 非空时启用
	if secret := strings.TrimSpace(cfg.FeishuSecret); secret != "" {
		body.Timestamp, body.Sign = newFeishuSignature(secret, time.Now())
	}

	resp, err := SharedHTTPClient.R().
		SetHeader("Content-Type", "application/json").
		SetBody(&body).
		Post(cfg.FeishuRobot)
	if err != nil {
		logger.SugaredLogger.Error(err.Error())
		return "发送飞书消息失败"
	}
	logger.SugaredLogger.Infof("send feishu message: %s", resp.String())
	return parseFeishuResponse(resp.String())
}

func buildFeishuCardMessage(title, message string, options FeishuCardOptions) FeishuCardMessage {
	message = strutil.ReplaceWithMap(message, map[string]string{
		"\\n":   "\n",
		"\\r":   "\r",
		"\\t":   "\t",
		"\\\\n": "\n",
		"\\\\r": "\r",
		"\\\\t": "\t",
	})
	if options.MentionAll {
		// JSON 2.0 的 @所有人语法与旧卡片协议不同，仅在调用方明确需要时追加。
		message = "<at id=all></at>\n" + message
	}

	return FeishuCardMessage{
		MsgType: "interactive",
		Card: FeishuCard{
			Schema: "2.0",
			Header: &FeishuHeader{
				Template: options.HeaderTemplate,
				Title: FeishuHeaderText{
					Tag:     "plain_text",
					Content: "go-stock " + title,
				},
			},
			Body: FeishuCardBody{
				Elements: []FeishuElement{{
					Tag:     "markdown",
					Content: message,
				}},
			},
		},
	}
}

// genFeishuSign 飞书自定义机器人签名计算
// 规则：以 timestamp + "\n" + secret 作为签名串，用 HmacSHA256 计算空串的签名，再 Base64 编码
func genFeishuSign(secret string, timestamp int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	h := hmac.New(sha256.New, []byte(stringToSign))
	_, _ = h.Write([]byte{})
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func newFeishuSignature(secret string, now time.Time) (timestamp, sign string) {
	ts := now.Unix()
	return fmt.Sprintf("%d", ts), genFeishuSign(secret, ts)
}

func addFeishuSignature(message, secret string, now time.Time) (string, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(message), &body); err != nil {
		return "", fmt.Errorf("飞书消息必须是 JSON 对象: %w", err)
	}
	if body == nil {
		return "", fmt.Errorf("飞书消息必须是 JSON 对象")
	}
	timestamp, sign := newFeishuSignature(secret, now)
	timestampJSON, _ := json.Marshal(timestamp)
	signJSON, _ := json.Marshal(sign)
	body["timestamp"] = timestampJSON
	body["sign"] = signJSON
	signedBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("生成飞书签名消息失败: %w", err)
	}
	return string(signedBody), nil
}

// parseFeishuResponse 解析飞书返回体，code==0 为成功
func parseFeishuResponse(body string) string {
	code := int(gjson.Get(body, "code").Int())
	if code == 0 {
		return "发送飞书消息成功"
	}
	msg := gjson.Get(body, "msg").String()
	if msg == "" {
		msg = body
	}
	if code == 19021 {
		msg += "；请确认填写的是该自定义机器人的签名校验 Secret（不是应用 App Secret），并检查系统时间是否准确"
	}
	return fmt.Sprintf("发送飞书消息失败: code=%d msg=%s", code, msg)
}

// FeishuCardMessage 飞书自定义机器人消息体（支持 timestamp/sign 签名字段）
type FeishuCardMessage struct {
	MsgType   string     `json:"msg_type"`
	Card      FeishuCard `json:"card"`
	Timestamp string     `json:"timestamp,omitempty"`
	Sign      string     `json:"sign,omitempty"`
}

// FeishuCard 飞书卡片 JSON 2.0 结构
// 文档：https://open.feishu.cn/document/feishu-cards/card-json-v2-structure
type FeishuCard struct {
	Schema string         `json:"schema"` // 必须显式声明 "2.0"
	Header *FeishuHeader  `json:"header,omitempty"`
	Body   FeishuCardBody `json:"body"`
}

// FeishuCardBody 卡片正文容器
type FeishuCardBody struct {
	Elements []FeishuElement `json:"elements"`
}

type FeishuHeader struct {
	Template string           `json:"template,omitempty"`
	Title    FeishuHeaderText `json:"title"`
}

type FeishuHeaderText struct {
	Tag     string `json:"tag"` // plain_text 或 lark_md
	Content string `json:"content"`
}

// FeishuElement 2.0 markdown 元素
type FeishuElement struct {
	Tag     string `json:"tag"`     // "markdown"
	Content string `json:"content"` // markdown 内容
}

type FeishuResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}
