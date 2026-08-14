package data

import "testing"

func pointer[T any](value T) *T { return &value }

func TestDetectAIModelProvider(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		model    string
		expected string
	}{
		{name: "openai official", baseURL: "https://api.openai.com/v1", model: "gpt-4o", expected: AIProviderOpenAI},
		{name: "localhost recognized openai reasoning model", baseURL: "http://127.0.0.1:8317/v1", model: "gpt-5.6-sol", expected: AIProviderOpenAI},
		{name: "unknown localhost stays conservative", baseURL: "http://127.0.0.1:8317/v1", model: "custom-chat", expected: AIProviderOpenAICompatible},
		{name: "third party gpt clone stays conservative", baseURL: "https://custom.example/v1", model: "gpt-5-clone", expected: AIProviderOpenAICompatible},
		{name: "deepseek endpoint", baseURL: "https://api.deepseek.com", model: "deepseek-reasoner", expected: AIProviderDeepSeek},
		{name: "gemini endpoint", baseURL: "https://generativelanguage.googleapis.com", model: "gemini-2.5-pro", expected: AIProviderGemini},
		{name: "openrouter", baseURL: "https://openrouter.ai/api/v1", model: "openai/o3", expected: AIProviderOpenRouter},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := DetectAIModelProvider(test.baseURL, test.model); actual != test.expected {
				t.Fatalf("DetectAIModelProvider() = %q, want %q", actual, test.expected)
			}
		})
	}
}

func TestValidateAIConfigRangesAndReasoning(t *testing.T) {
	base := AIConfig{
		Name: "OpenAI", BaseUrl: "http://localhost:8317/v1", ApiKey: "secret",
		ModelName: "gpt-5.6-sol", MaxTokens: 8192, TimeOut: 300,
		ReasoningMode: ReasoningModeOn, ReasoningEffort: "high",
	}
	if err := ValidateAIConfig(&base); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	invalidEffort := base
	invalidEffort.ReasoningEffort = "extreme"
	if err := ValidateAIConfig(&invalidEffort); err == nil {
		t.Fatal("unsupported reasoning effort should fail")
	}

	invalidTemperature := base
	invalidTemperature.TemperatureConfigured = true
	invalidTemperature.Temperature = 2.1
	if err := ValidateAIConfig(&invalidTemperature); err == nil {
		t.Fatal("out-of-range temperature should fail")
	}

	generic := base
	generic.ModelName = "custom-chat"
	generic.ReasoningMode = ReasoningModeOn
	generic.ReasoningEffort = "high"
	if err := ValidateAIConfig(&generic); err == nil {
		t.Fatal("generic endpoint must not accept unsupported reasoning fields")
	}
}

func TestValidateAIConfigAllowsSafeUnsupportedDefaults(t *testing.T) {
	generic := AIConfig{Name: "generic", BaseUrl: "http://localhost:8317/v1", ApiKey: "secret", ModelName: "custom-chat", MaxTokens: 4096, TimeOut: 300, ResponseFormat: "text", ReasoningMode: ReasoningModeOff}
	if err := ValidateAIConfig(&generic); err != nil {
		t.Fatalf("generic safe defaults rejected: %v", err)
	}
	claude := AIConfig{Name: "claude", BaseUrl: "https://api.anthropic.com", ApiKey: "secret", ModelName: "claude-sonnet", MaxTokens: 4096, TimeOut: 300, ResponseFormat: "text", ReasoningMode: ReasoningModeOff}
	if err := ValidateAIConfig(&claude); err != nil {
		t.Fatalf("claude text default rejected: %v", err)
	}
}

func TestBuildAIRequestParametersUsesCompletionLimitExclusively(t *testing.T) {
	completionLimit := 8192
	o := &OpenAi{AIConfig: AIConfig{Name: "openai", BaseUrl: "http://localhost:8317/v1", ApiKey: "secret", ModelName: "gpt-5.6-sol", MaxTokens: 4096, MaxCompletionTokens: &completionLimit, TimeOut: 300, ReasoningMode: ReasoningModeOn, ReasoningEffort: "high"}}
	body, err := buildAIRequestParameters(o, true)
	if err != nil {
		t.Fatalf("buildAIRequestParameters() error = %v", err)
	}
	if _, exists := body["max_tokens"]; exists {
		t.Fatalf("body includes both token limits: %+v", body)
	}
	if body["max_completion_tokens"] != completionLimit || body["reasoning_effort"] != "high" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestBuildAIRequestParametersSessionOnPreservesSavedReasoningOff(t *testing.T) {
	o := &OpenAi{AIConfig: AIConfig{
		Name: "openrouter", BaseUrl: "https://openrouter.ai/api/v1", ApiKey: "secret",
		ModelName: "openai/o3", MaxTokens: 4096, TimeOut: 300,
		ReasoningMode: ReasoningModeOff,
	}}
	body, err := buildAIRequestParameters(o, true)
	if err != nil {
		t.Fatalf("buildAIRequestParameters() error = %v", err)
	}
	if _, exists := body["reasoning"]; exists {
		t.Fatalf("session on added reasoning to a saved-off config: %+v", body)
	}
}

func TestBuildAIRequestParametersProviderReasoningMappings(t *testing.T) {
	tests := []struct {
		name   string
		config AIConfig
		assert func(*testing.T, map[string]any)
	}{
		{
			name: "openai effort",
			config: AIConfig{BaseUrl: "http://localhost:8317/v1", ModelName: "gpt-5.6-sol",
				ReasoningMode: ReasoningModeOn, ReasoningEffort: "high"},
			assert: func(t *testing.T, body map[string]any) {
				if body["reasoning_effort"] != "high" {
					t.Fatalf("unexpected OpenAI reasoning body: %+v", body)
				}
			},
		},
		{
			name: "openrouter effort and budget",
			config: AIConfig{BaseUrl: "https://openrouter.ai/api/v1", ModelName: "openai/o3",
				ReasoningMode: ReasoningModeOn, ReasoningEffort: "high", ReasoningBudget: pointer(2048)},
			assert: func(t *testing.T, body map[string]any) {
				reasoning, ok := body["reasoning"].(map[string]any)
				if !ok || reasoning["enabled"] != true || reasoning["effort"] != "high" || reasoning["max_tokens"] != 2048 {
					t.Fatalf("unexpected OpenRouter reasoning body: %+v", body)
				}
			},
		},
		{
			name: "ark automatic reasoning",
			config: AIConfig{BaseUrl: "https://ark.cn-beijing.volces.com/api/v3", ModelName: "doubao-seed",
				ReasoningMode: ReasoningModeAuto, ReasoningEffort: "low"},
			assert: func(t *testing.T, body map[string]any) {
				thinking, ok := body["thinking"].(map[string]any)
				if !ok || thinking["type"] != "auto" || body["reasoning_effort"] != "low" {
					t.Fatalf("unexpected Ark reasoning body: %+v", body)
				}
			},
		},
		{
			name: "gemini effort",
			config: AIConfig{BaseUrl: "https://generativelanguage.googleapis.com", ModelName: "gemini-2.5-pro",
				ReasoningMode: ReasoningModeOn, ReasoningEffort: "high"},
			assert: func(t *testing.T, body map[string]any) {
				thinking, ok := body["thinkingConfig"].(map[string]any)
				if !ok || thinking["includeThoughts"] != true || thinking["thinkingLevel"] != "HIGH" {
					t.Fatalf("unexpected Gemini reasoning body: %+v", body)
				}
			},
		},
		{
			name: "claude adaptive reasoning",
			config: AIConfig{BaseUrl: "https://api.anthropic.com", ModelName: "claude-sonnet",
				ReasoningMode: ReasoningModeAuto},
			assert: func(t *testing.T, body map[string]any) {
				thinking, ok := body["thinking"].(map[string]any)
				if !ok || thinking["type"] != "adaptive" {
					t.Fatalf("unexpected Claude reasoning body: %+v", body)
				}
			},
		},
		{
			name: "deepseek switch",
			config: AIConfig{BaseUrl: "https://api.deepseek.com", ModelName: "deepseek-reasoner",
				ReasoningMode: ReasoningModeOn},
			assert: func(t *testing.T, body map[string]any) {
				thinking, ok := body["thinking"].(map[string]any)
				if !ok || thinking["type"] != "enabled" {
					t.Fatalf("unexpected DeepSeek reasoning body: %+v", body)
				}
			},
		},
		{
			name: "qwen switch",
			config: AIConfig{BaseUrl: "https://dashscope.aliyuncs.com/compatible-mode/v1", ModelName: "qwen-plus",
				ReasoningMode: ReasoningModeOn},
			assert: func(t *testing.T, body map[string]any) {
				if body["enable_thinking"] != true {
					t.Fatalf("unexpected Qwen reasoning body: %+v", body)
				}
			},
		},
		{
			name: "ollama switch",
			config: AIConfig{BaseUrl: "http://localhost:11434/v1", ModelName: "qwen3",
				ReasoningMode: ReasoningModeOn},
			assert: func(t *testing.T, body map[string]any) {
				if body["think"] != true {
					t.Fatalf("unexpected Ollama reasoning body: %+v", body)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.config.Name = test.name
			test.config.ApiKey = "secret"
			test.config.MaxTokens = 8192
			test.config.TimeOut = 300
			o := &OpenAi{AIConfig: test.config}
			body, err := buildAIRequestParameters(o, true)
			if err != nil {
				t.Fatalf("buildAIRequestParameters() error = %v", err)
			}
			test.assert(t, body)
		})
	}
}

func TestResolveEffectiveAIParametersDoesNotMutateConfig(t *testing.T) {
	config := AIConfig{
		Name: "OpenRouter", BaseUrl: "https://openrouter.ai/api/v1", ApiKey: "secret",
		ModelName: "openai/o3", MaxTokens: 8192, TimeOut: 300,
		ReasoningMode: ReasoningModeOn, ReasoningEffort: "high", ReasoningBudget: pointer(2048),
	}

	params, _, err := ResolveEffectiveAIParameters(config, false)
	if err != nil {
		t.Fatalf("ResolveEffectiveAIParameters() error = %v", err)
	}
	if params.ReasoningMode != ReasoningModeOff || params.ReasoningEffort != "" || params.ReasoningBudget != nil {
		t.Fatalf("session override did not disable reasoning: %+v", params)
	}
	if config.ReasoningMode != ReasoningModeOn || config.ReasoningEffort != "high" || config.ReasoningBudget == nil {
		t.Fatalf("source config was mutated: %+v", config)
	}
}

func TestResolveLegacyGenericThinkingFallsBackSafely(t *testing.T) {
	legacy := AIConfig{Name: "legacy", BaseUrl: "https://custom.example/v1", ApiKey: "secret", ModelName: "custom-chat", MaxTokens: 4096, TimeOut: 300, Thinking: true}
	params, _, err := ResolveEffectiveAIParameters(legacy, true)
	if err != nil {
		t.Fatalf("legacy generic config should remain usable: %v", err)
	}
	if params.ReasoningMode != ReasoningModeOff || params.ReasoningEffort != "" || params.ReasoningBudget != nil {
		t.Fatalf("legacy generic reasoning was not disabled safely: %+v", params)
	}
}

func TestWithSessionThinkingOverride(t *testing.T) {
	budget := 2048
	saved := AIConfig{ReasoningMode: ReasoningModeOn, ReasoningEffort: "high", ReasoningBudget: &budget, Thinking: true}
	disabled := WithSessionThinkingOverride(saved, false)
	if disabled.ReasoningMode != ReasoningModeOff || disabled.ReasoningEffort != "" || disabled.ReasoningBudget != nil || disabled.Thinking {
		t.Fatalf("session off was not applied: %+v", disabled)
	}
	if saved.ReasoningMode != ReasoningModeOn || saved.ReasoningEffort != "high" || saved.ReasoningBudget == nil || !saved.Thinking {
		t.Fatalf("saved config was mutated: %+v", saved)
	}
	enabled := WithSessionThinkingOverride(saved, true)
	if enabled.ReasoningMode != ReasoningModeOn || enabled.ReasoningEffort != "high" || enabled.ReasoningBudget == nil {
		t.Fatalf("session on did not preserve saved reasoning: %+v", enabled)
	}
	offSaved := AIConfig{BaseUrl: "http://localhost:8317/v1", ModelName: "gpt-5.6-sol", ReasoningMode: ReasoningModeOff}
	enabledFromOff := WithSessionThinkingOverride(offSaved, true)
	if enabledFromOff.ReasoningMode != ReasoningModeOff || enabledFromOff.Thinking {
		t.Fatalf("session on did not preserve saved reasoning-off mode: %+v", enabledFromOff)
	}
}
