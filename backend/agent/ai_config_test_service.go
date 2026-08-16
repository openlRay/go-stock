package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/models"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	aiConfigTestPrompt       = "仅回复 OK"
	aiConfigTestEmbedding    = "ping"
	aiConfigTestPreviewRunes = 120
	aiConfigTestMaxBodyBytes = 1 << 20
)

type aiConfigTestChatModelFactory func(context.Context, data.AIConfig) (model.ToolCallingChatModel, error)
type aiConfigTestHTTPClientFactory func(time.Duration, data.AIConfig) *http.Client

// AIConfigTestService 对请求携带的配置副本执行一次最小真实请求，不读取或写入配置数据库。
type AIConfigTestService struct {
	modelFactory      aiConfigTestChatModelFactory
	httpClientFactory aiConfigTestHTTPClientFactory
}

func NewAIConfigTestService() *AIConfigTestService {
	return &AIConfigTestService{
		modelFactory:      createChatModel,
		httpClientFactory: buildChatModelHTTPClient,
	}
}

func (s *AIConfigTestService) Test(ctx context.Context, config *data.AIConfig) *models.AIConfigTestResult {
	startedAt := time.Now()
	finish := func(result *models.AIConfigTestResult) *models.AIConfigTestResult {
		result.LatencyMillis = time.Since(startedAt).Milliseconds()
		return result
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if config == nil {
		return finish(aiConfigTestFailure("AI 配置不能为空"))
	}

	// 测试只使用请求级副本，并关闭 reasoning，避免污染已保存配置或返回推理内容。
	requestConfig := cloneAIConfigForTest(config)
	requestConfig = data.WithSessionThinkingOverride(requestConfig, false)
	if err := data.ValidateAIConfig(&requestConfig); err != nil {
		return finish(aiConfigTestFailure("配置校验失败：" + err.Error()))
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(requestConfig.TimeOut)*time.Second)
	defer cancel()

	var result *models.AIConfigTestResult
	if requestConfig.ModelType == "embedding" {
		result = s.testEmbedding(requestCtx, requestConfig)
	} else {
		result = s.testChat(requestCtx, requestConfig)
	}
	return finish(result)
}

func (s *AIConfigTestService) testChat(ctx context.Context, config data.AIConfig) *models.AIConfigTestResult {
	factory := s.modelFactory
	if factory == nil {
		factory = createChatModel
	}
	chatModel, err := factory(ctx, config)
	if err != nil {
		return aiConfigTestFailure("无法初始化对话模型，请检查接口地址、模型名称和配置参数")
	}
	response, err := chatModel.Generate(ctx, []*schema.Message{schema.UserMessage(aiConfigTestPrompt)})
	if err != nil {
		return aiConfigTestRequestFailure(err)
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return aiConfigTestFailure("对话模型未返回有效文本")
	}
	return &models.AIConfigTestResult{
		Success:         true,
		Message:         "对话模型服务可用",
		ResponsePreview: safeAIConfigTestPreview(response.Content, config.ApiKey),
	}
}

func (s *AIConfigTestService) testEmbedding(ctx context.Context, config data.AIConfig) *models.AIConfigTestResult {
	payload, err := json.Marshal(map[string]any{
		"model": config.ModelName,
		"input": aiConfigTestEmbedding,
	})
	if err != nil {
		return aiConfigTestFailure("无法构造向量模型测试请求")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, aiConfigTestEmbeddingEndpoint(config.BaseUrl), bytes.NewReader(payload))
	if err != nil {
		return aiConfigTestFailure("向量模型接口地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.ApiKey)

	timeout := time.Duration(config.TimeOut) * time.Second
	factory := s.httpClientFactory
	if factory == nil {
		factory = buildChatModelHTTPClient
	}
	client := factory(timeout, config)
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	response, err := client.Do(req)
	if err != nil {
		return aiConfigTestRequestFailure(err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return aiConfigTestFailure(fmt.Sprintf("向量模型请求失败（HTTP %d），请检查鉴权和接口配置", response.StatusCode))
	}

	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, aiConfigTestMaxBodyBytes)).Decode(&result); err != nil {
		return aiConfigTestFailure("向量模型返回格式无效")
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return aiConfigTestFailure("向量模型未返回有效向量")
	}
	return &models.AIConfigTestResult{
		Success:         true,
		Message:         "向量模型服务可用",
		ResponsePreview: fmt.Sprintf("向量维度：%d", len(result.Data[0].Embedding)),
	}
}

func cloneAIConfigForTest(config *data.AIConfig) data.AIConfig {
	result := *config
	result.StopSequences = append([]string(nil), config.StopSequences...)
	return result
}

func aiConfigTestEmbeddingEndpoint(baseURL string) string {
	baseURL = normalizeBaseURL(baseURL)
	baseURL = strings.TrimSuffix(baseURL, "/chat/completions")
	baseURL = strings.TrimSuffix(baseURL, "/embeddings")
	return baseURL + "/embeddings"
}

func safeAIConfigTestPreview(content, apiKey string) string {
	if secret := strings.TrimSpace(apiKey); secret != "" {
		content = strings.ReplaceAll(content, secret, "[已隐藏]")
	}
	content = strings.Join(strings.Fields(content), " ")
	runes := []rune(content)
	if len(runes) > aiConfigTestPreviewRunes {
		return string(runes[:aiConfigTestPreviewRunes]) + "…"
	}
	return content
}

func aiConfigTestFailure(message string) *models.AIConfigTestResult {
	return &models.AIConfigTestResult{Success: false, Message: message}
}

func aiConfigTestRequestFailure(err error) *models.AIConfigTestResult {
	switch {
	case errors.Is(err, context.Canceled):
		return aiConfigTestFailure("模型服务测试已取消")
	case errors.Is(err, context.DeadlineExceeded):
		return aiConfigTestFailure("模型服务连接超时，请检查超时和网络设置")
	default:
		return aiConfigTestFailure("模型服务请求失败，请检查接口地址、API Key、模型名称和网络设置")
	}
}
