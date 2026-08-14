package data

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

const (
	AIProviderOpenAICompatible = "openai_compatible"
	AIProviderOpenAI           = "openai"
	AIProviderVolcArk          = "volc_ark"
	AIProviderDashScope        = "dashscope"
	AIProviderOpenRouter       = "openrouter"
	AIProviderAnthropic        = "anthropic"
	AIProviderOllama           = "ollama"
	AIProviderGemini           = "gemini"
	AIProviderDeepSeek         = "deepseek"
)

const (
	ReasoningModeOff  = "off"
	ReasoningModeAuto = "auto"
	ReasoningModeOn   = "on"
)

type AIParameterOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type AIParameterCapability struct {
	Supported   bool                `json:"supported"`
	Label       string              `json:"label"`
	Description string              `json:"description"`
	Min         *float64            `json:"min,omitempty"`
	Max         *float64            `json:"max,omitempty"`
	Step        *float64            `json:"step,omitempty"`
	Options     []AIParameterOption `json:"options,omitempty"`
}

type AIModelCapabilities struct {
	Profile             string                `json:"profile"`
	ProviderName        string                `json:"providerName"`
	MaxTokens           AIParameterCapability `json:"maxTokens"`
	MaxCompletionTokens AIParameterCapability `json:"maxCompletionTokens"`
	Temperature         AIParameterCapability `json:"temperature"`
	TopP                AIParameterCapability `json:"topP"`
	TopK                AIParameterCapability `json:"topK"`
	PresencePenalty     AIParameterCapability `json:"presencePenalty"`
	FrequencyPenalty    AIParameterCapability `json:"frequencyPenalty"`
	Seed                AIParameterCapability `json:"seed"`
	StopSequences       AIParameterCapability `json:"stopSequences"`
	ResponseFormat      AIParameterCapability `json:"responseFormat"`
	ReasoningMode       AIParameterCapability `json:"reasoningMode"`
	ReasoningEffort     AIParameterCapability `json:"reasoningEffort"`
	ReasoningBudget     AIParameterCapability `json:"reasoningBudget"`
	Warnings            []string              `json:"warnings"`
}

func float64Pointer(v float64) *float64 { return &v }

func numericCapability(label, description string, minValue, maxValue, step float64) AIParameterCapability {
	return AIParameterCapability{
		Supported:   true,
		Label:       label,
		Description: description,
		Min:         float64Pointer(minValue),
		Max:         float64Pointer(maxValue),
		Step:        float64Pointer(step),
	}
}

func plainCapability(label, description string) AIParameterCapability {
	return AIParameterCapability{Supported: true, Label: label, Description: description}
}

func enumCapability(label, description string, values ...AIParameterOption) AIParameterCapability {
	return AIParameterCapability{Supported: true, Label: label, Description: description, Options: values}
}

func enumOptions(values ...string) []AIParameterOption {
	result := make([]AIParameterOption, 0, len(values))
	labels := map[string]string{
		ReasoningModeOff: "关闭", ReasoningModeAuto: "自动", ReasoningModeOn: "开启",
		"none": "无", "minimal": "最小", "low": "低", "medium": "中", "high": "高",
		"text": "文本", "json_object": "JSON 对象",
	}
	for _, value := range values {
		label := labels[value]
		if label == "" {
			label = value
		}
		result = append(result, AIParameterOption{Label: label, Value: value})
	}
	return result
}

func normalizeAIBaseURL(base string) string {
	return strings.TrimSuffix(strings.TrimSpace(base), "/")
}

// DetectAIModelProvider centralizes provider detection for capability discovery,
// validation, direct HTTP requests, and Eino model creation.
func DetectAIModelProvider(baseURL, modelName string) string {
	baseLower := strings.ToLower(normalizeAIBaseURL(baseURL))
	modelLower := strings.ToLower(strings.TrimSpace(modelName))

	switch {
	case strings.Contains(baseLower, "volces.com") && strings.Contains(baseLower, "ark"):
		return AIProviderVolcArk
	case strings.Contains(baseLower, "dashscope.aliyuncs.com"),
		strings.Contains(baseLower, "dashscope-intl.aliyuncs.com"),
		strings.Contains(baseLower, "maas.aliyuncs.com"):
		return AIProviderDashScope
	case strings.Contains(baseLower, "openrouter.ai"):
		return AIProviderOpenRouter
	case strings.Contains(baseLower, "anthropic.com"), strings.Contains(baseLower, "api.anthropic"):
		return AIProviderAnthropic
	case strings.Contains(baseLower, ":11434"), strings.Contains(baseLower, "ollama"):
		return AIProviderOllama
	case strings.Contains(baseLower, "generativelanguage.googleapis.com"), strings.Contains(baseLower, "ai.google.dev"):
		return AIProviderGemini
	case strings.HasPrefix(modelLower, "gemini-") || strings.HasPrefix(modelLower, "gemini/") || strings.HasPrefix(modelLower, "models/gemini"):
		if baseLower == "" || strings.Contains(baseLower, "googleapis.com") {
			return AIProviderGemini
		}
	case strings.Contains(baseLower, "api.deepseek.com"):
		return AIProviderDeepSeek
	case strings.HasPrefix(modelLower, "deepseek-") && (baseLower == "" || strings.Contains(baseLower, "deepseek.com")):
		return AIProviderDeepSeek
	case strings.Contains(baseLower, "api.openai.com"):
		return AIProviderOpenAI
	case isLocalAIEndpoint(baseLower) && isRecognizedOpenAIReasoningModel(modelLower):
		return AIProviderOpenAI
	}
	return AIProviderOpenAICompatible
}

func isLocalAIEndpoint(baseLower string) bool {
	if baseLower == "" {
		return true
	}
	parsed, err := url.Parse(baseLower)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func isRecognizedOpenAIReasoningModel(modelLower string) bool {
	modelLower = strings.TrimSpace(modelLower)
	for _, prefix := range []string{"o1", "o3", "o4", "gpt-5"} {
		if modelLower == prefix || strings.HasPrefix(modelLower, prefix+"-") || strings.HasPrefix(modelLower, prefix+".") {
			return true
		}
	}
	return false
}

func GetAIModelCapabilities(baseURL, modelName string) AIModelCapabilities {
	provider := DetectAIModelProvider(baseURL, modelName)
	capabilities := AIModelCapabilities{
		Profile:          provider,
		ProviderName:     providerDisplayName(provider),
		MaxTokens:        numericCapability("最大输出 Token", "限制最终回答可使用的 Token 数。数值越大，回答可能越完整，但成本和耗时也会增加。", 1, 1048576, 1),
		Temperature:      numericCapability("Temperature", "控制输出随机性。较低更稳定，较高更多样；通常只调整 Temperature 与 Top P 中的一项。", 0, 2, 0.1),
		TopP:             numericCapability("Top P", "核采样阈值。较低更聚焦；通常不要与 Temperature 同时调整。", 0, 1, 0.05),
		PresencePenalty:  numericCapability("存在惩罚", "鼓励模型引入新主题，范围 -2 到 2。", -2, 2, 0.1),
		FrequencyPenalty: numericCapability("频率惩罚", "降低重复用词的概率，范围 -2 到 2。", -2, 2, 0.1),
		Seed:             plainCapability("随机种子", "尽力提高相同输入的可复现性；供应商不保证完全确定。"),
		StopSequences:    plainCapability("停止序列", "模型遇到任一序列时停止生成，每行填写一个。"),
		ResponseFormat:   enumCapability("输出格式", "选择普通文本或要求模型输出 JSON 对象。", enumOptions("text", "json_object")...),
		Warnings:         []string{"Temperature 与 Top P 通常只调整其中一项。"},
	}

	switch provider {
	case AIProviderOpenAI:
		capabilities.MaxCompletionTokens = numericCapability("最大完成 Token", "包含可见回答和推理 Token 的完成上限，适用于 OpenAI 推理模型。", 1, 1048576, 1)
		capabilities.ReasoningMode = enumCapability("推理模式", "关闭时本配置不请求推理；自动或开启时使用所选推理强度。", enumOptions(ReasoningModeOff, ReasoningModeAuto, ReasoningModeOn)...)
		capabilities.ReasoningEffort = enumCapability("推理强度", "更高强度通常提升复杂任务质量，也会增加延迟、Token 消耗和成本。", enumOptions("low", "medium", "high")...)
	case AIProviderOpenRouter:
		capabilities.MaxCompletionTokens = numericCapability("最大完成 Token", "包含最终回答与推理 Token 的完成上限。", 1, 1048576, 1)
		capabilities.ReasoningMode = enumCapability("推理模式", "控制 OpenRouter 是否启用模型推理。", enumOptions(ReasoningModeOff, ReasoningModeAuto, ReasoningModeOn)...)
		capabilities.ReasoningEffort = enumCapability("推理强度", "更高强度通常提升复杂任务质量，也会增加延迟、Token 消耗和成本。", enumOptions("none", "minimal", "low", "medium", "high")...)
		capabilities.ReasoningBudget = numericCapability("推理预算", "为推理过程分配的最大 Token 数。", 1, 1048576, 1)
	case AIProviderVolcArk:
		capabilities.Temperature.Max = float64Pointer(1)
		capabilities.MaxCompletionTokens = numericCapability("最大完成 Token", "包含最终回答与推理 Token 的完成上限；设置后不再发送最大输出 Token。", 1, 65536, 1)
		capabilities.ReasoningMode = enumCapability("推理模式", "控制火山引擎模型的思考模式。", enumOptions(ReasoningModeOff, ReasoningModeAuto, ReasoningModeOn)...)
		capabilities.ReasoningEffort = enumCapability("推理强度", "更高强度通常提升复杂任务质量，也会增加延迟、Token 消耗和成本。", enumOptions("minimal", "low", "medium", "high")...)
	case AIProviderAnthropic:
		capabilities.Temperature.Max = float64Pointer(1)
		capabilities.TopK = numericCapability("Top K", "限制参与采样的候选 Token 数。", 1, 1000, 1)
		capabilities.PresencePenalty = AIParameterCapability{}
		capabilities.FrequencyPenalty = AIParameterCapability{}
		capabilities.Seed = AIParameterCapability{}
		capabilities.ResponseFormat = AIParameterCapability{}
		capabilities.ReasoningMode = enumCapability("推理模式", "自动模式使用 Claude 自适应推理；开启模式使用显式 Token 预算。", enumOptions(ReasoningModeOff, ReasoningModeAuto, ReasoningModeOn)...)
		capabilities.ReasoningBudget = numericCapability("推理预算", "Claude 用于扩展思考的 Token 预算；需小于输出 Token 上限。", 1024, 1048576, 1)
	case AIProviderGemini:
		capabilities.Temperature.Max = float64Pointer(1)
		capabilities.TopK = numericCapability("Top K", "限制参与采样的候选 Token 数。", 1, 1000, 1)
		capabilities.PresencePenalty = AIParameterCapability{}
		capabilities.FrequencyPenalty = AIParameterCapability{}
		capabilities.Seed = AIParameterCapability{}
		capabilities.StopSequences = AIParameterCapability{}
		capabilities.ReasoningMode = enumCapability("推理模式", "控制 Gemini 的思考配置。", enumOptions(ReasoningModeOff, ReasoningModeAuto, ReasoningModeOn)...)
		capabilities.ReasoningEffort = enumCapability("推理强度", "更高强度通常提升复杂任务质量，也会增加延迟、Token 消耗和成本。", enumOptions("minimal", "low", "medium", "high")...)
		capabilities.ReasoningBudget = numericCapability("推理预算", "Gemini 思考过程可使用的 Token 数。部分模型不允许与推理强度同时设置。", 1, 1048576, 1)
	case AIProviderDeepSeek:
		capabilities.ReasoningMode = enumCapability("推理模式", "当前适配器仅稳定支持开启或关闭，不提供强度与预算。", enumOptions(ReasoningModeOff, ReasoningModeOn)...)
	case AIProviderDashScope:
		capabilities.ReasoningMode = enumCapability("推理模式", "当前 Qwen 适配器仅稳定支持开启或关闭，不提供强度与预算。", enumOptions(ReasoningModeOff, ReasoningModeOn)...)
	case AIProviderOllama:
		capabilities.TopK = numericCapability("Top K", "限制参与采样的候选 Token 数。", 1, 1000, 1)
		capabilities.PresencePenalty = AIParameterCapability{}
		capabilities.FrequencyPenalty = AIParameterCapability{}
		capabilities.ResponseFormat = AIParameterCapability{}
		capabilities.ReasoningMode = enumCapability("推理模式", "控制本地 Ollama 模型是否启用 thinking；具体模型需支持该能力。", enumOptions(ReasoningModeOff, ReasoningModeOn)...)
	default:
		capabilities.Warnings = append(capabilities.Warnings, "未识别的 OpenAI 兼容接口仅发送通用参数，不会发送供应商专属推理字段。")
	}

	return capabilities
}

func providerDisplayName(provider string) string {
	return map[string]string{
		AIProviderOpenAICompatible: "OpenAI 兼容接口",
		AIProviderOpenAI:           "OpenAI",
		AIProviderVolcArk:          "火山引擎 Ark",
		AIProviderDashScope:        "阿里云百炼 Qwen",
		AIProviderOpenRouter:       "OpenRouter",
		AIProviderAnthropic:        "Anthropic Claude",
		AIProviderOllama:           "Ollama",
		AIProviderGemini:           "Google Gemini",
		AIProviderDeepSeek:         "DeepSeek",
	}[provider]
}

func capabilitySupportsOption(capability AIParameterCapability, value string) bool {
	if value == "" {
		return true
	}
	return slices.ContainsFunc(capability.Options, func(option AIParameterOption) bool {
		return option.Value == value
	})
}

func validateHTTPURL(fieldName, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s必须是有效的 HTTP(S) 地址", fieldName)
	}
	return nil
}
