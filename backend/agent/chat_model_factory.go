package agent

import (
	"context"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/logger"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/deepseek"
	"github.com/cloudwego/eino-ext/components/model/gemini"
	"github.com/cloudwego/eino-ext/components/model/ollama"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino-ext/components/model/openrouter"
	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
	ollamaapi "github.com/eino-contrib/ollama/api"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"google.golang.org/genai"
)

func normalizeBaseURL(base string) string {
	return strings.TrimSuffix(strings.TrimSpace(base), "/")
}

func normalizeChatModelBaseURL(base string) string {
	base = normalizeBaseURL(base)
	return strings.TrimSuffix(base, "/chat/completions")
}

func parseAccessSecret(apiKey string) (ak, sk string) {
	s := strings.TrimSpace(apiKey)
	if s == "" {
		return "", ""
	}
	for _, sep := range []string{"|", ";"} {
		if idx := strings.Index(s, sep); idx > 0 && idx < len(s)-len(sep) {
			return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+len(sep):])
		}
	}
	if idx := strings.Index(s, ":"); idx > 0 && idx < len(s)-1 {
		// 避免误拆 http(s)://
		prefix := strings.ToLower(s[:idx])
		if strings.HasPrefix(prefix, "http") {
			return s, ""
		}
		return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:])
	}
	return s, ""
}

func ptrBool(v bool) *bool { return &v }

// createChatModel 按 Eino 生态组件路由（参见 https://www.cloudwego.io/zh/docs/eino/ecosystem_integration/chat_model/ ）
// 未命中专用实现时回退到 OpenAI 兼容 ChatModel（硅基流动、LM Studio、Azure OpenAI 等）。
func createChatModel(ctx context.Context, aiConfig data.AIConfig) (model.ToolCallingChatModel, error) {
	baseURL := normalizeChatModelBaseURL(aiConfig.BaseUrl)
	effective, _, err := data.ResolveEffectiveAIParameters(aiConfig, data.EffectiveReasoningEnabled(&aiConfig))
	if err != nil {
		return nil, err
	}
	var temperature *float32
	if effective.Temperature != nil {
		value := float32(*effective.Temperature)
		temperature = &value
	}
	var topP *float32
	if effective.TopP != nil {
		value := float32(*effective.TopP)
		topP = &value
	}
	var presencePenalty *float32
	if effective.PresencePenalty != nil {
		value := float32(*effective.PresencePenalty)
		presencePenalty = &value
	}
	var frequencyPenalty *float32
	if effective.FrequencyPenalty != nil {
		value := float32(*effective.FrequencyPenalty)
		frequencyPenalty = &value
	}
	timeout := time.Duration(aiConfig.TimeOut) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	maxTok := effective.MaxTokens
	if maxTok <= 0 {
		maxTok = 4096
	}

	p := effective.Provider
	logger.SugaredLogger.Infof("createChatModel provider=%s base=%q model=%q", p, aiConfig.BaseUrl, aiConfig.ModelName)

	switch p {
	case data.AIProviderVolcArk:
		var thinking *ark.Thinking
		if effective.ReasoningMode != data.ReasoningModeOff {
			thinkingType := arkmodel.ThinkingTypeAuto
			if effective.ReasoningMode == data.ReasoningModeOn {
				thinkingType = arkmodel.ThinkingTypeEnabled
			}
			thinking = &ark.Thinking{Type: thinkingType}
		}
		cfg := &ark.ChatModelConfig{
			BaseURL:          baseURL,
			Model:            aiConfig.ModelName,
			APIKey:           aiConfig.ApiKey,
			MaxTokens:        &maxTok,
			Temperature:      temperature,
			TopP:             topP,
			Stop:             effective.StopSequences,
			PresencePenalty:  presencePenalty,
			FrequencyPenalty: frequencyPenalty,
			Thinking:         thinking,
			Timeout:          &timeout,
		}
		if effective.MaxCompletionTokens != nil {
			cfg.MaxTokens = nil
			cfg.MaxCompletionTokens = effective.MaxCompletionTokens
		}
		if effective.ReasoningEffort != "" {
			effort := arkmodel.ReasoningEffort(effective.ReasoningEffort)
			cfg.ReasoningEffort = &effort
		}
		return ark.NewChatModel(ctx, cfg)

	case data.AIProviderDashScope:
		cfg := &qwen.ChatModelConfig{
			APIKey:    aiConfig.ApiKey,
			BaseURL:   baseURL,
			Model:     aiConfig.ModelName,
			Timeout:   timeout,
			MaxTokens: &maxTok,
		}
		cfg.Temperature = temperature
		cfg.TopP = topP
		cfg.Stop = effective.StopSequences
		cfg.PresencePenalty = presencePenalty
		cfg.FrequencyPenalty = frequencyPenalty
		if effective.Seed != nil {
			seed := int(*effective.Seed)
			cfg.Seed = &seed
		}
		if effective.ResponseFormat == "json_object" {
			cfg.ResponseFormat = &einoopenai.ChatCompletionResponseFormat{Type: einoopenai.ChatCompletionResponseFormatTypeJSONObject}
		}
		if effective.ReasoningMode != data.ReasoningModeOff {
			cfg.EnableThinking = ptrBool(true)
		}
		return qwen.NewChatModel(ctx, cfg)

	case data.AIProviderOpenRouter:
		cfg := &openrouter.Config{
			APIKey:              aiConfig.ApiKey,
			BaseURL:             baseURL,
			Model:               aiConfig.ModelName,
			Timeout:             timeout,
			MaxTokens:           &maxTok,
			MaxCompletionTokens: effective.MaxCompletionTokens,
			Temperature:         temperature,
			TopP:                topP,
			Stop:                effective.StopSequences,
			PresencePenalty:     presencePenalty,
			FrequencyPenalty:    frequencyPenalty,
		}
		if effective.MaxCompletionTokens != nil {
			cfg.MaxTokens = nil
		}
		if effective.Seed != nil {
			seed := int(*effective.Seed)
			cfg.Seed = &seed
		}
		if effective.ResponseFormat == "json_object" {
			cfg.ResponseFormat = &openrouter.ChatCompletionResponseFormat{Type: openrouter.ChatCompletionResponseFormatTypeJSONObject}
		}
		if effective.ReasoningMode != data.ReasoningModeOff {
			enabled := effective.ReasoningMode == data.ReasoningModeOn
			reasoning := &openrouter.Reasoning{Enabled: &enabled, Effort: openrouter.Effort(effective.ReasoningEffort)}
			if effective.ReasoningBudget != nil {
				reasoning.MaxTokens = *effective.ReasoningBudget
			}
			cfg.Reasoning = reasoning
		}
		return openrouter.NewChatModel(ctx, cfg)

	case data.AIProviderAnthropic:
		maxOut := aiConfig.MaxTokens
		if maxOut <= 0 {
			maxOut = 8192
		}
		cfg := &claude.Config{
			APIKey:    aiConfig.ApiKey,
			Model:     aiConfig.ModelName,
			MaxTokens: maxOut,
			HTTPClient: &http.Client{
				Timeout: timeout,
			},
		}
		cfg.Temperature = temperature
		cfg.TopP = topP
		if effective.TopK != nil {
			value := int32(*effective.TopK)
			cfg.TopK = &value
		}
		cfg.StopSequences = effective.StopSequences
		if b := baseURL; b != "" {
			cfg.BaseURL = &b
		}
		if effective.ReasoningMode == data.ReasoningModeOn && effective.ReasoningBudget != nil {
			cfg.Thinking = &claude.Thinking{Enable: true, BudgetTokens: *effective.ReasoningBudget}
		} else if effective.ReasoningMode == data.ReasoningModeAuto {
			cfg.ThinkingConfig = &anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}
		}
		return claude.NewChatModel(ctx, cfg)

	case data.AIProviderOllama:
		base := strings.TrimSpace(aiConfig.BaseUrl)
		if base == "" {
			base = "http://127.0.0.1:11434"
		}
		opt := &ollamaapi.Options{}
		if temperature != nil {
			opt.Temperature = *temperature
		}
		if topP != nil {
			opt.TopP = *topP
		}
		if effective.TopK != nil {
			opt.TopK = *effective.TopK
		}
		opt.Stop = effective.StopSequences
		if effective.Seed != nil {
			opt.Seed = int(*effective.Seed)
		}
		cfg := &ollama.ChatModelConfig{
			BaseURL: base,
			Model:   aiConfig.ModelName,
			Timeout: timeout,
			Options: opt,
		}
		if effective.ReasoningMode != data.ReasoningModeOff {
			tv := ollamaapi.ThinkValue{Value: true}
			cfg.Thinking = &tv
		}
		return ollama.NewChatModel(ctx, cfg)

	case data.AIProviderGemini:
		cc := &genai.ClientConfig{APIKey: aiConfig.ApiKey}
		if b := baseURL; b != "" {
			cc.HTTPOptions = genai.HTTPOptions{BaseURL: b}
		}
		client, err := genai.NewClient(ctx, cc)
		if err != nil {
			return nil, fmt.Errorf("gemini genai client: %w", err)
		}
		gcfg := &gemini.Config{
			Client: client,
			Model:  aiConfig.ModelName,
		}
		if maxTok > 0 {
			gcfg.MaxTokens = &maxTok
		}
		gcfg.Temperature = temperature
		gcfg.TopP = topP
		if effective.TopK != nil {
			value := int32(*effective.TopK)
			gcfg.TopK = &value
		}
		if effective.ReasoningMode != data.ReasoningModeOff {
			thinking := &genai.ThinkingConfig{IncludeThoughts: true}
			if effective.ReasoningBudget != nil {
				value := int32(*effective.ReasoningBudget)
				thinking.ThinkingBudget = &value
			}
			switch effective.ReasoningEffort {
			case "minimal":
				thinking.ThinkingLevel = genai.ThinkingLevelMinimal
			case "low":
				thinking.ThinkingLevel = genai.ThinkingLevelLow
			case "medium":
				thinking.ThinkingLevel = genai.ThinkingLevelMedium
			case "high":
				thinking.ThinkingLevel = genai.ThinkingLevelHigh
			}
			gcfg.ThinkingConfig = thinking
		}
		return gemini.NewChatModel(ctx, gcfg)

	case data.AIProviderDeepSeek:
		deepseekCfg := &deepseek.ChatModelConfig{
			BaseURL:          baseURL,
			Model:            aiConfig.ModelName,
			APIKey:           aiConfig.ApiKey,
			MaxTokens:        maxTok,
			Temperature:      derefFloat32(temperature),
			TopP:             derefFloat32(topP),
			Stop:             effective.StopSequences,
			PresencePenalty:  derefFloat32(presencePenalty),
			FrequencyPenalty: derefFloat32(frequencyPenalty),
			Timeout:          timeout,
		}
		if effective.ResponseFormat == "json_object" {
			deepseekCfg.ResponseFormatType = deepseek.ResponseFormatTypeJSONObject
		}
		if effective.ReasoningMode != data.ReasoningModeOff {
			deepseekCfg.ThinkingConfig = &deepseek.ThinkingConfig{Type: "enabled"}
		}
		if proxyClient := buildProxyHTTPClient(timeout, aiConfig); proxyClient != nil {
			deepseekCfg.HTTPClient = proxyClient
		}
		return deepseek.NewChatModel(ctx, deepseekCfg)

	default:
		extraFields := map[string]any{}
		cfg := &einoopenai.ChatModelConfig{
			BaseURL:             baseURL,
			Model:               aiConfig.ModelName,
			APIKey:              aiConfig.ApiKey,
			Timeout:             timeout,
			MaxTokens:           &maxTok,
			MaxCompletionTokens: effective.MaxCompletionTokens,
			Temperature:         temperature,
			TopP:                topP,
			Stop:                effective.StopSequences,
			PresencePenalty:     presencePenalty,
			FrequencyPenalty:    frequencyPenalty,
			ExtraFields:         extraFields,
		}
		if effective.MaxCompletionTokens != nil {
			cfg.MaxTokens = nil
		}
		if effective.Seed != nil {
			seed := int(*effective.Seed)
			cfg.Seed = &seed
		}
		if effective.ResponseFormat == "json_object" {
			cfg.ResponseFormat = &einoopenai.ChatCompletionResponseFormat{Type: einoopenai.ChatCompletionResponseFormatTypeJSONObject}
		}
		if p == data.AIProviderOpenAI && effective.ReasoningEffort != "" {
			cfg.ReasoningEffort = einoopenai.ReasoningEffortLevel(effective.ReasoningEffort)
		}
		if proxyClient := buildProxyHTTPClient(timeout, aiConfig); proxyClient != nil {
			cfg.HTTPClient = proxyClient
		}
		return einoopenai.NewChatModel(ctx, cfg)
	}
}

func derefFloat32(value *float32) float32 {
	if value == nil {
		return 0
	}
	return *value
}

// buildProxyHTTPClient 构建带代理的HTTP客户端，若未配置代理则返回nil
func buildProxyHTTPClient(timeout time.Duration, config data.AIConfig) *http.Client {
	if !config.HttpProxyEnabled || config.HttpProxy == "" {
		return nil
	}
	proxyURL, err := url.Parse(config.HttpProxy)
	if err != nil {
		logger.SugaredLogger.Warnf("解析HTTP代理失败: %v", err)
		return nil
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: timeout,
	}
}
