package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-stock/backend/data"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type fakeAIConfigTestModel struct {
	response *schema.Message
	err      error
	messages []*schema.Message
}

func (f *fakeAIConfigTestModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	f.messages = messages
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func (f *fakeAIConfigTestModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeAIConfigTestModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

func validAIConfigTestConfig(modelType string) *data.AIConfig {
	return &data.AIConfig{
		Name:          "draft",
		BaseUrl:       "http://127.0.0.1:8317/v1",
		ApiKey:        "sk-test-secret",
		ModelName:     "test-model",
		ModelType:     modelType,
		MaxTokens:     32,
		TimeOut:       2,
		ReasoningMode: data.ReasoningModeOn,
		Thinking:      true,
	}
}

func TestAIConfigTestServiceChatUsesDraftCopyAndFinalContentOnly(t *testing.T) {
	config := validAIConfigTestConfig("chat")
	config.ExtraHeaders = `{"X-Team-Id":"team-a"}`
	config.HttpProxyEnabled = true
	config.HttpProxy = "http://127.0.0.1:7890"
	fake := &fakeAIConfigTestModel{response: &schema.Message{
		Role:             schema.Assistant,
		Content:          "  OK sk-test-secret  ",
		ReasoningContent: "private reasoning must not leak",
	}}
	var captured data.AIConfig
	service := NewAIConfigTestService()
	service.modelFactory = func(_ context.Context, requestConfig data.AIConfig) (model.ToolCallingChatModel, error) {
		captured = requestConfig
		return fake, nil
	}

	result := service.Test(context.Background(), config)
	if !result.Success || result.Message != "对话模型服务可用" || result.ResponsePreview != "OK [已隐藏]" {
		t.Fatalf("Test() result = %+v", result)
	}
	if len(fake.messages) != 1 || fake.messages[0].Content != aiConfigTestPrompt {
		t.Fatalf("chat messages = %+v", fake.messages)
	}
	if captured.Thinking || captured.ReasoningMode != data.ReasoningModeOff {
		t.Fatalf("request-local reasoning was not disabled: %+v", captured)
	}
	if captured.ExtraHeaders != config.ExtraHeaders || !captured.HttpProxyEnabled || captured.HttpProxy != config.HttpProxy || captured.TimeOut != config.TimeOut {
		t.Fatalf("request-local transport config was not preserved: %+v", captured)
	}
	if !config.Thinking || config.ReasoningMode != data.ReasoningModeOn {
		t.Fatalf("source draft was mutated: %+v", config)
	}
	if strings.Contains(result.ResponsePreview, "reasoning") {
		t.Fatalf("reasoning leaked into preview: %+v", result)
	}
}

func TestAIConfigTestServiceReturnsSafeChatFailures(t *testing.T) {
	config := validAIConfigTestConfig("chat")
	service := NewAIConfigTestService()
	service.modelFactory = func(context.Context, data.AIConfig) (model.ToolCallingChatModel, error) {
		return &fakeAIConfigTestModel{err: errors.New("provider body sk-test-secret private reasoning")}, nil
	}

	result := service.Test(context.Background(), config)
	if result.Success {
		t.Fatalf("provider failure result = %+v", result)
	}
	serialized := result.Message + result.ResponsePreview
	for _, forbidden := range []string{"sk-test-secret", "provider body", "private reasoning"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("unsafe provider detail %q leaked in %+v", forbidden, result)
		}
	}

	service.modelFactory = func(context.Context, data.AIConfig) (model.ToolCallingChatModel, error) {
		return &fakeAIConfigTestModel{err: context.DeadlineExceeded}, nil
	}
	result = service.Test(context.Background(), config)
	if result.Success || !strings.Contains(result.Message, "超时") {
		t.Fatalf("timeout result = %+v", result)
	}
}

func TestAIConfigTestServiceEmbeddingUsesCompatibleEndpointAndHeaders(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test-secret" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Team-Id"); got != "team-a" {
			t.Errorf("X-Team-Id = %q", got)
		}
		var body struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Model != "test-embedding" || body.Input != aiConfigTestEmbedding {
			t.Errorf("request body = %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer server.Close()

	config := validAIConfigTestConfig("embedding")
	config.BaseUrl = server.URL + "/v1/"
	config.ModelName = "test-embedding"
	config.ExtraHeaders = `{"X-Team-Id":"team-a"}`
	result := NewAIConfigTestService().Test(context.Background(), config)
	if !result.Success || result.Message != "向量模型服务可用" || result.ResponsePreview != "向量维度：3" {
		t.Fatalf("embedding result = %+v", result)
	}
	if requestCount != 1 {
		t.Fatalf("request count = %d", requestCount)
	}
}

func TestAIConfigTestServiceEmbeddingFailureDoesNotExposeProviderBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("sk-test-secret full provider error body"))
	}))
	defer server.Close()

	config := validAIConfigTestConfig("embedding")
	config.BaseUrl = server.URL + "/v1"
	result := NewAIConfigTestService().Test(context.Background(), config)
	if result.Success || !strings.Contains(result.Message, "HTTP 401") {
		t.Fatalf("embedding failure result = %+v", result)
	}
	if strings.Contains(result.Message+result.ResponsePreview, "sk-test-secret") || strings.Contains(result.Message, "provider error") {
		t.Fatalf("provider body leaked in %+v", result)
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	result = NewAIConfigTestService().Test(ctx, config)
	if result.Success || !strings.Contains(result.Message, "超时") {
		t.Fatalf("embedding timeout result = %+v", result)
	}
}

func TestSafeAIEndpointHostDropsCredentialsAndPath(t *testing.T) {
	endpoint := "https://user:secret@example.com:8443/v1/chat/completions?api_key=hidden"
	if got := safeAIEndpointHost(endpoint); got != "example.com" {
		t.Fatalf("safeAIEndpointHost() = %q", got)
	}
}

func TestBuildChatModelHTTPClientUsesRequestLocalProxyAndTimeout(t *testing.T) {
	config := validAIConfigTestConfig("chat")
	config.HttpProxyEnabled = true
	config.HttpProxy = "http://127.0.0.1:7890"
	client := buildChatModelHTTPClient(7*time.Second, *config)
	if client == nil || client.Timeout != 7*time.Second {
		t.Fatalf("HTTP client = %+v", client)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("HTTP transport type = %T", client.Transport)
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.com/v1/models", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("resolve proxy: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != config.HttpProxy {
		t.Fatalf("proxy URL = %v", proxyURL)
	}
}
