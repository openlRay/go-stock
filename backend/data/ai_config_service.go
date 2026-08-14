package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"math"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

type AIConfigReference struct {
	Type       string `json:"type"`
	SourceID   uint   `json:"sourceId"`
	SourceName string `json:"sourceName"`
	Detail     string `json:"detail"`
}

type DeleteAIConfigResult struct {
	Success    bool                `json:"success"`
	Message    string              `json:"message"`
	References []AIConfigReference `json:"references"`
}

type EffectiveAIParameters struct {
	Provider            string
	MaxTokens           int
	MaxCompletionTokens *int
	Temperature         *float64
	TopP                *float64
	TopK                *int
	PresencePenalty     *float64
	FrequencyPenalty    *float64
	Seed                *int64
	StopSequences       []string
	ResponseFormat      string
	ReasoningMode       string
	ReasoningEffort     string
	ReasoningBudget     *int
}

var aiConfigCopyMu sync.Mutex
var aiConfigDefaultMu sync.Mutex

// ResolveAIConfig applies the common model selection contract without
// mutating the supplied slice: explicit ID, persisted default, legacy first.
func ResolveAIConfig(configs []*AIConfig, explicitID int) (*AIConfig, bool) {
	if explicitID > 0 {
		for _, config := range configs {
			if config != nil && int(config.ID) == explicitID {
				return config, true
			}
		}
	}
	for _, config := range configs {
		if config != nil && config.IsDefault {
			return config, true
		}
	}
	for _, config := range configs {
		if config != nil {
			return config, true
		}
	}
	return nil, false
}

func NormalizeLegacyAIConfig(config *AIConfig) {
	if config == nil {
		return
	}
	if !config.TemperatureConfigured && config.Temperature != 0 {
		config.TemperatureConfigured = true
	}
	if strings.TrimSpace(config.ReasoningMode) == "" {
		if config.Thinking {
			capabilities := GetAIModelCapabilities(config.BaseUrl, config.ModelName)
			if capabilities.ReasoningMode.Supported {
				config.ReasoningMode = ReasoningModeOn
			} else {
				// Legacy `thinking=true` was sent universally. Unknown compatible
				// gateways must stay usable without receiving vendor-only fields.
				config.ReasoningMode = ReasoningModeOff
			}
		} else {
			config.ReasoningMode = ReasoningModeOff
		}
	}
	config.Thinking = config.ReasoningMode != ReasoningModeOff
	if config.ResponseFormat == "" {
		config.ResponseFormat = "text"
	}
	if config.StopSequences == nil {
		config.StopSequences = []string{}
	}
}

func EffectiveReasoningEnabled(config *AIConfig) bool {
	if config == nil {
		return false
	}
	mode := strings.TrimSpace(config.ReasoningMode)
	if mode == "" {
		return config.Thinking
	}
	return mode != ReasoningModeOff
}

// WithSessionThinkingOverride copies a saved configuration for one request.
// A disabled session always forces reasoning off; an enabled session preserves
// the saved mode, effort, and budget without mutating the cached configuration.
func WithSessionThinkingOverride(config AIConfig, enabled bool) AIConfig {
	NormalizeLegacyAIConfig(&config)
	if !enabled {
		config.ReasoningMode = ReasoningModeOff
		config.ReasoningEffort = ""
		config.ReasoningBudget = nil
		config.Thinking = false
	}
	return config
}

func ResolveEffectiveAIParameters(config AIConfig, sessionThinkingEnabled bool) (EffectiveAIParameters, []string, error) {
	config = WithSessionThinkingOverride(config, sessionThinkingEnabled)
	if err := ValidateAIConfig(&config); err != nil {
		return EffectiveAIParameters{}, nil, err
	}

	params := EffectiveAIParameters{
		Provider:            DetectAIModelProvider(config.BaseUrl, config.ModelName),
		MaxTokens:           config.MaxTokens,
		MaxCompletionTokens: copyIntPointer(config.MaxCompletionTokens),
		TopP:                copyFloatPointer(config.TopP),
		TopK:                copyIntPointer(config.TopK),
		PresencePenalty:     copyFloatPointer(config.PresencePenalty),
		FrequencyPenalty:    copyFloatPointer(config.FrequencyPenalty),
		Seed:                copyInt64Pointer(config.Seed),
		StopSequences:       append([]string(nil), config.StopSequences...),
		ResponseFormat:      config.ResponseFormat,
		ReasoningMode:       config.ReasoningMode,
		ReasoningEffort:     config.ReasoningEffort,
		ReasoningBudget:     copyIntPointer(config.ReasoningBudget),
	}
	if config.TemperatureConfigured || config.Temperature != 0 {
		params.Temperature = copyFloatPointer(&config.Temperature)
	}
	return params, nil, nil
}

func copyFloatPointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func copyIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func copyInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func ValidateAIConfig(config *AIConfig) error {
	if config == nil {
		return errors.New("AI 配置不能为空")
	}
	config.Name = strings.TrimSpace(config.Name)
	config.BaseUrl = normalizeAIBaseURL(config.BaseUrl)
	config.ApiKey = strings.TrimSpace(config.ApiKey)
	config.ModelName = strings.TrimSpace(config.ModelName)
	config.HttpProxy = strings.TrimSpace(config.HttpProxy)
	config.ResponseFormat = strings.TrimSpace(config.ResponseFormat)
	config.ReasoningMode = strings.TrimSpace(config.ReasoningMode)
	config.ReasoningEffort = strings.TrimSpace(config.ReasoningEffort)
	NormalizeLegacyAIConfig(config)

	if config.Name == "" || config.BaseUrl == "" || config.ApiKey == "" || config.ModelName == "" {
		return errors.New("名称、接口地址、API Key 和模型名称均不能为空")
	}
	if err := validateHTTPURL("接口地址", config.BaseUrl); err != nil {
		return err
	}
	if config.HttpProxyEnabled {
		if config.HttpProxy == "" {
			return errors.New("启用 HTTP 代理后必须填写代理地址")
		}
		if err := validateHTTPURL("HTTP 代理地址", config.HttpProxy); err != nil {
			return err
		}
	}
	if config.MaxTokens <= 0 {
		return errors.New("最大输出 Token 必须大于 0")
	}
	if config.TimeOut <= 0 {
		return errors.New("超时时间必须大于 0 秒")
	}

	capabilities := GetAIModelCapabilities(config.BaseUrl, config.ModelName)
	if err := validateOptionalNumber("最大完成 Token", config.MaxCompletionTokens, capabilities.MaxCompletionTokens); err != nil {
		return err
	}
	if config.TemperatureConfigured || config.Temperature != 0 {
		if err := validateNumber("Temperature", config.Temperature, capabilities.Temperature); err != nil {
			return err
		}
	}
	if err := validateOptionalFloat("Top P", config.TopP, capabilities.TopP); err != nil {
		return err
	}
	if err := validateOptionalNumber("Top K", config.TopK, capabilities.TopK); err != nil {
		return err
	}
	if err := validateOptionalFloat("存在惩罚", config.PresencePenalty, capabilities.PresencePenalty); err != nil {
		return err
	}
	if err := validateOptionalFloat("频率惩罚", config.FrequencyPenalty, capabilities.FrequencyPenalty); err != nil {
		return err
	}
	if config.Seed != nil && !capabilities.Seed.Supported {
		return errors.New("当前模型能力不支持随机种子，请先清除该旧参数")
	}
	if len(config.StopSequences) > 0 && !capabilities.StopSequences.Supported {
		return errors.New("当前模型能力不支持停止序列，请先清除该旧参数")
	}
	cleanStops := make([]string, 0, len(config.StopSequences))
	for _, stop := range config.StopSequences {
		stop = strings.TrimSpace(stop)
		if stop == "" {
			return errors.New("停止序列不能包含空字符串")
		}
		cleanStops = append(cleanStops, stop)
	}
	config.StopSequences = cleanStops
	if config.ResponseFormat == "" {
		config.ResponseFormat = "text"
	}
	if !capabilities.ResponseFormat.Supported && config.ResponseFormat != "text" {
		return errors.New("当前模型能力不支持所选输出格式")
	}
	if capabilities.ResponseFormat.Supported && !capabilitySupportsOption(capabilities.ResponseFormat, config.ResponseFormat) {
		return errors.New("当前模型能力不支持所选输出格式")
	}
	if !capabilities.ReasoningMode.Supported && config.ReasoningMode != ReasoningModeOff {
		return errors.New("当前模型能力不支持所选推理模式，请调整旧推理参数")
	}
	if capabilities.ReasoningMode.Supported && !capabilitySupportsOption(capabilities.ReasoningMode, config.ReasoningMode) {
		return errors.New("当前模型能力不支持所选推理模式，请调整旧推理参数")
	}
	if config.ReasoningEffort != "" && !capabilitySupportsOption(capabilities.ReasoningEffort, config.ReasoningEffort) {
		return errors.New("当前模型能力不支持所选推理强度，请清除或调整该参数")
	}
	if err := validateOptionalNumber("推理预算", config.ReasoningBudget, capabilities.ReasoningBudget); err != nil {
		return err
	}
	if config.ReasoningMode == ReasoningModeOff && (config.ReasoningEffort != "" || config.ReasoningBudget != nil) {
		return errors.New("推理模式关闭时不能设置推理强度或推理预算")
	}
	if capabilities.Profile == AIProviderGemini && config.ReasoningEffort != "" && config.ReasoningBudget != nil {
		return errors.New("Gemini 推理强度与推理预算不能同时设置")
	}
	if capabilities.Profile == AIProviderAnthropic && config.ReasoningMode == ReasoningModeOn && config.ReasoningBudget == nil {
		return errors.New("Claude 推理模式开启时必须设置推理预算，或改用自动模式")
	}
	if capabilities.Profile == AIProviderAnthropic && config.ReasoningBudget != nil && *config.ReasoningBudget >= config.MaxTokens {
		return errors.New("Claude 推理预算必须小于最大输出 Token")
	}
	config.Thinking = config.ReasoningMode != ReasoningModeOff
	return nil
}

func validateOptionalFloat(name string, value *float64, capability AIParameterCapability) error {
	if value == nil {
		return nil
	}
	return validateNumber(name, *value, capability)
}

func validateOptionalNumber(name string, value *int, capability AIParameterCapability) error {
	if value == nil {
		return nil
	}
	return validateNumber(name, float64(*value), capability)
}

func validateNumber(name string, value float64, capability AIParameterCapability) error {
	if !capability.Supported {
		return fmt.Errorf("当前模型能力不支持%s，请先清除该旧参数", name)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%s不是有效数值", name)
	}
	if capability.Min != nil && value < *capability.Min {
		return fmt.Errorf("%s不能小于 %v", name, *capability.Min)
	}
	if capability.Max != nil && value > *capability.Max {
		return fmt.Errorf("%s不能大于 %v", name, *capability.Max)
	}
	return nil
}

func createAIConfig(config *AIConfig) (*AIConfig, error) {
	if config == nil {
		return nil, errors.New("AI 配置不能为空")
	}
	if config.ID != 0 {
		return nil, errors.New("新增配置不能包含已有 ID")
	}
	if err := ValidateAIConfig(config); err != nil {
		return nil, err
	}
	aiConfigDefaultMu.Lock()
	defer aiConfigDefaultMu.Unlock()
	if err := db.Dao.Transaction(func(tx *gorm.DB) error {
		if err := ensureUniqueAIConfigName(tx, config.Name, 0); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&AIConfig{}).Count(&count).Error; err != nil {
			return errors.New("检查默认 AI 配置失败")
		}
		config.IsDefault = count == 0
		if err := tx.Create(config).Error; err != nil {
			logger.SugaredLogger.Errorf("创建 AI 配置失败: %v", err)
			return errors.New("创建 AI 配置失败")
		}
		return ensureSingleDefaultAIConfig(tx)
	}); err != nil {
		return nil, err
	}
	return config, nil
}

func updateAIConfig(config *AIConfig) (*AIConfig, error) {
	if config == nil || config.ID == 0 {
		return nil, errors.New("更新配置需要有效 ID")
	}
	if err := ValidateAIConfig(config); err != nil {
		return nil, err
	}
	if err := ensureUniqueAIConfigName(db.Dao, config.Name, config.ID); err != nil {
		return nil, err
	}
	var existing AIConfig
	if err := db.Dao.First(&existing, config.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("要更新的 AI 配置不存在")
		}
		return nil, errors.New("查询 AI 配置失败")
	}
	if err := db.Dao.Model(&AIConfig{}).Where("id = ?", config.ID).Updates(aiConfigUpdateMap(config)).Error; err != nil {
		logger.SugaredLogger.Errorf("更新 AI 配置失败(id=%d): %v", config.ID, err)
		return nil, errors.New("更新 AI 配置失败")
	}
	var result AIConfig
	if err := db.Dao.First(&result, config.ID).Error; err != nil {
		return nil, errors.New("重新读取 AI 配置失败")
	}
	NormalizeLegacyAIConfig(&result)
	return &result, nil
}

func copyAIConfig(id uint) (*AIConfig, error) {
	if id == 0 {
		return nil, errors.New("复制配置需要有效 ID")
	}
	aiConfigCopyMu.Lock()
	defer aiConfigCopyMu.Unlock()
	var result AIConfig
	if err := db.Dao.Transaction(func(tx *gorm.DB) error {
		var source AIConfig
		if err := tx.First(&source, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("要复制的 AI 配置不存在")
			}
			return errors.New("查询 AI 配置失败")
		}
		source.ID = 0
		source.CreatedAt = time.Time{}
		source.UpdatedAt = time.Time{}
		source.SessionId = ""
		source.IsDefault = false
		source.Name = nextAIConfigCopyName(tx, source.Name)
		if err := tx.Create(&source).Error; err != nil {
			logger.SugaredLogger.Errorf("复制 AI 配置失败(id=%d): %v", id, err)
			return errors.New("复制 AI 配置失败")
		}
		result = source
		return nil
	}); err != nil {
		return nil, err
	}
	NormalizeLegacyAIConfig(&result)
	return &result, nil
}

func deleteAIConfig(id uint) (*DeleteAIConfigResult, error) {
	if id == 0 {
		return nil, errors.New("删除配置需要有效 ID")
	}
	aiConfigDefaultMu.Lock()
	defer aiConfigDefaultMu.Unlock()
	result := &DeleteAIConfigResult{References: []AIConfigReference{}}
	err := db.Dao.Transaction(func(tx *gorm.DB) error {
		var config AIConfig
		if err := tx.First(&config, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("要删除的 AI 配置不存在")
			}
			return errors.New("查询 AI 配置失败")
		}
		references, err := findAIConfigReferences(tx, id)
		if err != nil {
			return err
		}
		if len(references) > 0 {
			result.Message = "该配置仍被使用，无法删除"
			result.References = references
			return nil
		}
		if err := tx.Delete(&AIConfig{}, id).Error; err != nil {
			return errors.New("删除 AI 配置失败")
		}
		if err := ensureSingleDefaultAIConfig(tx); err != nil {
			return err
		}
		result.Success = true
		result.Message = "删除成功"
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func CreateAIConfig(config *AIConfig) (*AIConfig, error) {
	result, err := createAIConfig(config)
	if err == nil {
		refreshAIConfigRuntime()
	}
	return result, err
}

func UpdateAIConfig(config *AIConfig) (*AIConfig, error) {
	result, err := updateAIConfig(config)
	if err == nil {
		refreshAIConfigRuntime()
	}
	return result, err
}

func CopyAIConfig(id uint) (*AIConfig, error) {
	result, err := copyAIConfig(id)
	if err == nil {
		refreshAIConfigRuntime()
	}
	return result, err
}

func DeleteAIConfig(id uint) (*DeleteAIConfigResult, error) {
	result, err := deleteAIConfig(id)
	if err == nil && result != nil && result.Success {
		refreshAIConfigRuntime()
	}
	return result, err
}

func setDefaultAIConfig(id uint) (*AIConfig, error) {
	if id == 0 {
		return nil, errors.New("设置默认配置需要有效 ID")
	}
	aiConfigDefaultMu.Lock()
	defer aiConfigDefaultMu.Unlock()

	var result AIConfig
	if err := db.Dao.Transaction(func(tx *gorm.DB) error {
		var target AIConfig
		if err := tx.First(&target, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("要设为默认的 AI 配置不存在")
			}
			return errors.New("查询 AI 配置失败")
		}
		if err := tx.Model(&AIConfig{}).Where("is_default = ?", true).Update("is_default", false).Error; err != nil {
			return errors.New("清除原默认 AI 配置失败")
		}
		if err := tx.Model(&AIConfig{}).Where("id = ?", id).Update("is_default", true).Error; err != nil {
			return errors.New("设置默认 AI 配置失败")
		}
		if err := tx.First(&result, id).Error; err != nil {
			return errors.New("重新读取默认 AI 配置失败")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	NormalizeLegacyAIConfig(&result)
	return &result, nil
}

func SetDefaultAIConfig(id uint) (*AIConfig, error) {
	result, err := setDefaultAIConfig(id)
	if err == nil {
		refreshAIConfigRuntime()
	}
	return result, err
}

func ensureSingleDefaultAIConfig(database *gorm.DB) error {
	var configs []AIConfig
	if err := database.Select("id", "is_default").Order("id ASC").Find(&configs).Error; err != nil {
		return err
	}
	if len(configs) == 0 {
		return nil
	}
	keeperID := configs[0].ID
	for _, config := range configs {
		if config.IsDefault {
			keeperID = config.ID
			break
		}
	}
	if err := database.Model(&AIConfig{}).Where("is_default = ? AND id <> ?", true, keeperID).Update("is_default", false).Error; err != nil {
		return errors.New("清理重复默认 AI 配置失败")
	}
	if err := database.Model(&AIConfig{}).Where("id = ?", keeperID).Update("is_default", true).Error; err != nil {
		return errors.New("设置默认 AI 配置失败")
	}
	return nil
}

func aiConfigUpdateMap(config *AIConfig) map[string]any {
	stopSequences, _ := json.Marshal(config.StopSequences)
	return map[string]any{
		"name": config.Name, "base_url": config.BaseUrl, "api_key": config.ApiKey,
		"model_name": config.ModelName, "max_tokens": config.MaxTokens,
		"max_completion_tokens": config.MaxCompletionTokens, "temperature": config.Temperature,
		"temperature_configured": config.TemperatureConfigured, "top_p": config.TopP,
		"top_k": config.TopK, "presence_penalty": config.PresencePenalty,
		"frequency_penalty": config.FrequencyPenalty, "seed": config.Seed,
		"stop_sequences": string(stopSequences), "response_format": config.ResponseFormat,
		"reasoning_mode": config.ReasoningMode, "reasoning_effort": config.ReasoningEffort,
		"reasoning_budget": config.ReasoningBudget, "time_out": config.TimeOut,
		"http_proxy": config.HttpProxy, "http_proxy_enabled": config.HttpProxyEnabled,
		"session_id": config.SessionId, "thinking": config.Thinking,
	}
}

func ensureUniqueAIConfigName(database *gorm.DB, name string, excludeID uint) error {
	var count int64
	query := database.Model(&AIConfig{}).Where("LOWER(name) = LOWER(?)", name)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	if err := query.Count(&count).Error; err != nil {
		return errors.New("检查配置名称失败")
	}
	if count > 0 {
		return fmt.Errorf("配置名称“%s”已存在", name)
	}
	return nil
}

func nextAIConfigCopyName(database *gorm.DB, name string) string {
	base := strings.TrimSpace(name) + "-副本"
	candidate := base
	for index := 2; ; index++ {
		var count int64
		if err := database.Model(&AIConfig{}).Where("LOWER(name) = LOWER(?)", candidate).Count(&count).Error; err != nil || count == 0 {
			return candidate
		}
		candidate = fmt.Sprintf("%s%d", base, index)
	}
}

func findAIConfigReferences(tx *gorm.DB, id uint) ([]AIConfigReference, error) {
	references := make([]AIConfigReference, 0)
	var settings []Settings
	if err := tx.Find(&settings).Error; err != nil {
		return nil, errors.New("检查飞书机器人引用失败")
	}
	for _, setting := range settings {
		if setting.FeishuBotAiConfigId == int(id) {
			references = append(references, AIConfigReference{Type: "feishu_bot", SourceID: setting.ID, SourceName: "飞书机器人", Detail: "飞书应用机器人正在使用此配置"})
		}
	}

	var tasks []models.CronTask
	if err := tx.Find(&tasks).Error; err != nil {
		return nil, errors.New("检查定时任务引用失败")
	}
	for _, task := range tasks {
		params := strings.TrimSpace(task.Params)
		if params == "" {
			continue
		}
		var object map[string]any
		if err := json.Unmarshal([]byte(params), &object); err != nil {
			if strings.Contains(params, "aiConfigId") || strings.Contains(params, "ai_config_id") {
				logger.SugaredLogger.Warnf("定时任务 AI 配置引用 JSON 无法解析(task_id=%d)", task.ID)
				return nil, fmt.Errorf("定时任务“%s”的参数格式异常，无法安全确认 AI 配置引用", task.Name)
			}
			continue
		}
		if jsonIDMatches(object["aiConfigId"], id) || jsonIDMatches(object["ai_config_id"], id) {
			references = append(references, AIConfigReference{Type: "cron_task", SourceID: task.ID, SourceName: task.Name, Detail: fmt.Sprintf("定时任务类型：%s", task.TaskType)})
		}
	}
	return references, nil
}

func jsonIDMatches(value any, id uint) bool {
	switch typed := value.(type) {
	case float64:
		return typed == float64(id)
	case string:
		return strings.TrimSpace(typed) == fmt.Sprint(id)
	case json.Number:
		return typed.String() == fmt.Sprint(id)
	default:
		return false
	}
}

func refreshAIConfigRuntime() {
	ConfigureFromSettings(GetSettingConfig())
	go EmitAppEvent("aiConfigsChanged")
}
