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

// maxOllamaNumCtx Ollama num_ctx 解析值（非用户显式配置）的默认上限。
// KV cache 显存占用随 num_ctx 线性增长，resolveContextWindow 对未知模型兜底
// 64000，直接透传可能撑爆本地显存/内存导致加载失败或推理骤降。
const maxOllamaNumCtx = 32768

// minOllamaNumCtx Ollama num_ctx 解析值的下限。旧配置兜底路径可能把小
// MaxTokens（如 100）误判为上下文窗口，透传会造成灾难性头部截断；Agent 的
// 工具 schema 本身就需要约 8k token，低于此值 Agent 无法正常工作。
const minOllamaNumCtx = 8192

func normalizeBaseURL(base string) string {
	return strings.TrimSuffix(strings.TrimSpace(base), "/")
}

func normalizeChatModelBaseURL(base string) string {
	base = normalizeBaseURL(base)
	return strings.TrimSuffix(base, "/chat/completions")
}

// normalizeOllamaBaseURL 归一化 Ollama 原生 API 的 BaseURL。
//
// Ollama 原生 API 端点固定在根路径（/api/chat、/api/generate），用户可能误填
// OpenAI 风格路径（如 /v1 或 /v1/chat/completions），导致 url.JoinPath 拼出
// /v1/api/chat 等不存在的端点，返回 Gin 默认 404 "404 page not found"。
// eino-contrib/ollama 的 stream 函数逐行 JSON 反序列化时，"404" 被解析为
// 有效 JSON 数字，随后的 'p'（page）触发 "invalid character 'p' after top-level value"。
func normalizeOllamaBaseURL(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return "http://127.0.0.1:11434"
	}
	u, err := url.Parse(base)
	if err != nil {
		return strings.TrimSuffix(base, "/")
	}
	path := u.Path
	// 迭代剥离已知的 OpenAI 风格路径后缀（先去尾斜杠再匹配，支持多层组合）
	for {
		path = strings.TrimSuffix(path, "/")
		switch {
		case strings.HasSuffix(path, "/chat/completions"):
			path = strings.TrimSuffix(path, "/chat/completions")
		case path == "/v1" || strings.HasSuffix(path, "/v1"):
			path = strings.TrimSuffix(path, "/v1")
		default:
			goto done
		}
	}
done:
	u.Path = path
	u.RawPath = ""
	result := u.String()
	if result != base {
		logger.SugaredLogger.Infof("normalizeOllamaBaseURL: %q -> %q (已剥离 OpenAI 风格路径后缀)", base, result)
	}
	return result
}

func safeAIEndpointHost(base string) string {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return ""
	}
	return parsed.Hostname()
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
	// MaxTokens 是输出上限；ContextWindow 单独描述输入与输出的总容量。
	// 对旧配置使用内置模型表和安全默认值兜底，避免把整个上下文窗口作为输出预算发送。
	contextWindow := resolveContextWindow(aiConfig)
	maxTok := resolveOutputMaxTokens(aiConfig, contextWindow)
	if maxTok <= 0 {
		maxTok = 4096
	}
	outputMaxTokens := &maxTok

	p := effective.Provider
	logger.SugaredLogger.Infof("createChatModel provider=%s host=%q model=%q", p, safeAIEndpointHost(aiConfig.BaseUrl), aiConfig.ModelName)

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
			MaxTokens: outputMaxTokens,
			Timeout:   timeout,
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
		maxOut := maxTok
		if maxOut <= 0 {
			maxOut = 8192
		}
		httpClient := buildChatModelHTTPClient(timeout, aiConfig)
		if httpClient == nil {
			httpClient = &http.Client{Timeout: timeout}
		}
		cfg := &claude.Config{
			APIKey:     aiConfig.ApiKey,
			Model:      aiConfig.ModelName,
			MaxTokens:  maxOut,
			HTTPClient: httpClient,
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
		base := normalizeOllamaBaseURL(aiConfig.BaseUrl)
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
		// num_ctx：Ollama 默认上下文仅 4096，而 Agent 注入的工具 schema + 系统提示
		// 轻松超过 8000 token，超出部分被 Ollama 静默截断（丢弃最头部内容，即系统
		// 提示与工具定义），表现为模型无视指令、工具调用格式错乱等诡异行为。
		// 显式透传上下文窗口，与 React/PlanExecute/DeepAgents 的 token 预算计算对齐：
		//   - 用户显式配置 ContextWindow > 0：直接使用（用户了解自己硬件，不设上限）
		//   - 否则用解析值（内置表/旧 MaxTokens/默认 64000），但封顶 maxOllamaNumCtx
		opt.NumCtx = aiConfig.ContextWindow
		if opt.NumCtx <= 0 {
			opt.NumCtx = contextWindow
			if opt.NumCtx > maxOllamaNumCtx {
				opt.NumCtx = maxOllamaNumCtx
			}
			if opt.NumCtx < minOllamaNumCtx {
				opt.NumCtx = minOllamaNumCtx
			}
		}
		// num_predict：输出上限与云端 max_tokens 语义对齐，避免单轮输出挤占上下文
		opt.NumPredict = maxTok
		logger.SugaredLogger.Infof("createChatModel ollama: num_ctx=%d num_predict=%d (config_ctx=%d resolved_ctx=%d)",
			opt.NumCtx, opt.NumPredict, aiConfig.ContextWindow, contextWindow)
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
		if httpClient := buildChatModelHTTPClient(timeout, aiConfig); httpClient != nil {
			deepseekCfg.HTTPClient = httpClient
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
		if httpClient := buildChatModelHTTPClient(timeout, aiConfig); httpClient != nil {
			cfg.HTTPClient = httpClient
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

// headerInjectTransport 包装 http.RoundTripper，在每次请求时注入自定义 Header
// （支持模板变量展开，如 {{sessionId}}、{{uuid}}）。
type headerInjectTransport struct {
	base      http.RoundTripper
	headers   map[string]string // 含模板变量的原始 header 值
	sessionId string
}

func (t *headerInjectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	for k, v := range t.headers {
		cloned.Header.Set(k, data.ExpandHeaderVars(v, t.sessionId))
	}
	return t.base.RoundTrip(cloned)
}

// buildChatModelHTTPClient 构建当前 AI 配置专属的代理和 Header transport。
// 代理必须使用 request-local 配置，不能回退到全局 Settings 以免不同模型配置互相污染。
func buildChatModelHTTPClient(timeout time.Duration, config data.AIConfig) *http.Client {
	headers := data.ParseHeaders(config.ExtraHeaders)
	hasHeaders := len(headers) > 0
	hasProxy := config.HttpProxyEnabled && config.HttpProxy != ""

	if !hasHeaders && !hasProxy {
		return nil
	}

	var transport http.RoundTripper = http.DefaultTransport
	if hasProxy {
		proxyURL, err := url.Parse(config.HttpProxy)
		if err != nil {
			logger.SugaredLogger.Warnf("解析 HTTP 代理失败，model=%q", config.ModelName)
		} else {
			if base, ok := http.DefaultTransport.(*http.Transport); ok {
				proxyTransport := base.Clone()
				proxyTransport.Proxy = http.ProxyURL(proxyURL)
				transport = proxyTransport
			} else {
				transport = &http.Transport{Proxy: http.ProxyURL(proxyURL)}
			}
		}
	}

	if hasHeaders {
		transport = &headerInjectTransport{
			base:      transport,
			headers:   headers,
			sessionId: config.SessionId,
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}
