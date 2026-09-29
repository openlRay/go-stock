package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-stock/backend/data"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
)

func TestDeepSeekVisionPreservesRequestParameters(t *testing.T) {
	var payload map[string]any
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Hostname() != "api.deepseek.com" || r.Header.Get("X-Vision-Test") != "present" {
			t.Error("视觉请求未保留目标地址或自定义 Header")
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer proxy.Close()
	config := data.AIConfig{
		Name: "deepseek-vision", BaseUrl: "http://api.deepseek.com/v1", ModelName: "deepseek-chat", ApiKey: "test-key",
		SupportVision: true, MaxTokens: 1024, TimeOut: 5, ReasoningMode: data.ReasoningModeOn,
		HttpProxyEnabled: true, HttpProxy: proxy.URL, ExtraHeaders: `{"X-Vision-Test":"present"}`,
	}
	model, err := createChatModel(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := buildVisionImageParts([]string{"data:image/png;base64,aGVsbG8="}, &config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.Generate(context.Background(), []*schema.Message{{Role: schema.User, UserInputMultiContent: parts}}); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(payload)
	if !strings.Contains(string(encoded), `"image_url"`) || payload["max_tokens"] != float64(1024) {
		t.Fatalf("视觉输入或输出上限丢失: %s", encoded)
	}
	thinking, ok := payload["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("视觉请求丢失推理配置: %s", encoded)
	}
}

// TestOpenAIComponentSendsImageURL 端到端验证：带 UserInputMultiContent 的消息经
// eino OpenAI 兼容组件（DeepSeek vision 实际走的路径）发出的 HTTP 请求体中
// 必须包含标准 image_url 内容块（type=image_url, image_url.url=...）。
func TestOpenAIComponentSendsImageURL(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = body
		w.Header().Set("Content-Type", "application/json")
		// 最小合法非流式响应
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"test",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer server.Close()

	cfg := &einoopenai.ChatModelConfig{
		BaseURL:     server.URL,
		Model:       "deepseek-v4-flash-vision-exp",
		APIKey:      "test-key",
		Timeout:     10 * time.Second,
		Temperature: ptrFloat32ForTest(0.7),
	}
	cm, err := einoopenai.NewChatModel(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewChatModel: %v", err)
	}

	u := "https://img.cdn1.vip/i/test.webp"
	msgs := []*schema.Message{
		// Content 与 UserInputMultiContent 互斥（openai SDK 序列化约束），文本在 parts 中
		{Role: schema.User, UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "图片里面是啥"},
			{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{
				MessagePartCommon: schema.MessagePartCommon{URL: &u},
			}},
		}},
	}
	out, err := cm.Generate(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Content != "ok" {
		t.Fatalf("unexpected output: %q", out.Content)
	}

	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				ImageURL *struct {
					URL string `json:"url"`
				} `json:"image_url,omitempty"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(capturedBody, &req); err != nil {
		t.Fatalf("unmarshal request body: %v\nbody: %s", err, string(capturedBody))
	}
	if len(req.Messages) == 0 {
		t.Fatal("no messages in request")
	}
	last := req.Messages[len(req.Messages)-1]
	if len(last.Content) != 2 {
		t.Fatalf("expected 2 content parts, got %d: %s", len(last.Content), string(capturedBody))
	}
	if last.Content[0].Type != "text" || last.Content[0].Text != "图片里面是啥" {
		t.Fatalf("text part mismatch: %+v", last.Content[0])
	}
	if last.Content[1].Type != "image_url" || last.Content[1].ImageURL == nil || last.Content[1].ImageURL.URL != u {
		t.Fatalf("image_url part mismatch: %+v\nbody: %s", last.Content[1], string(capturedBody))
	}
}

// TestOpenAIComponentSendsDataURL 验证 data URL（base64 直传回退模式）也能正确下发。
func TestOpenAIComponentSendsDataURL(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"test",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer server.Close()

	cfg := &einoopenai.ChatModelConfig{
		BaseURL: server.URL,
		Model:   "test-vision",
		APIKey:  "test-key",
		Timeout: 10 * time.Second,
	}
	cm, err := einoopenai.NewChatModel(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewChatModel: %v", err)
	}

	dataURL := "data:image/png;base64,aGVsbG8="
	msgs := []*schema.Message{
		{Role: schema.User, UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "看图"},
			{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{
				MessagePartCommon: schema.MessagePartCommon{URL: &dataURL},
			}},
		}},
	}
	if _, err := cm.Generate(context.Background(), msgs); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	body := string(capturedBody)
	if !strings.Contains(body, `"image_url"`) || !strings.Contains(body, dataURL) {
		t.Fatalf("request body missing image_url with data URL: %s", body)
	}
}

func ptrFloat32ForTest(v float32) *float32 { return &v }
