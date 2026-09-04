package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeStrategyConditionModel struct {
	response string
	err      error
	wait     bool
	messages []*schema.Message
}

func (f *fakeStrategyConditionModel) Generate(ctx context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	f.messages = messages
	if f.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.err != nil {
		return nil, f.err
	}
	return &schema.Message{Role: schema.Assistant, Content: f.response}, nil
}

func (f *fakeStrategyConditionModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeStrategyConditionModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

func withStrategyConditionTestDB(t *testing.T) {
	t.Helper()
	previous := db.Dao
	database, err := gorm.Open(sqlite.New(sqlite.Config{DriverName: "sqlite", DSN: "file:" + t.Name() + "?mode=memory&cache=shared"}), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&models.CustomStrategy{}))
	db.Dao = database
	t.Cleanup(func() { db.Dao = previous })
}

func strategyConditionTestConfigs() (*data.AIConfig, *data.AIConfig) {
	defaultConfig := &data.AIConfig{
		ID: 1, Name: "default", ModelName: "default-chat", ModelType: "chat", IsDefault: true,
		ReasoningMode: data.ReasoningModeOff,
	}
	selectedConfig := &data.AIConfig{
		ID: 2, Name: "selected", ModelName: "selected-chat", ModelType: "chat",
		ReasoningMode: data.ReasoningModeOn, ReasoningEffort: "high", Thinking: true,
	}
	return defaultConfig, selectedConfig
}

func TestStrategyConditionGenerateReturnsIndependentDraft(t *testing.T) {
	withStrategyConditionTestDB(t)
	defaultConfig, selectedConfig := strategyConditionTestConfigs()
	fakeModel := &fakeStrategyConditionModel{response: `{
		"query":"沪深A股；近20日涨幅大于20%；量比大于1.2；排除ST股、退市股、科创板和创业板",
		"summary":"增加量比并整理排除项",
		"warnings":["请在实际执行选股后核对上游识别条件", ""]
	}`}
	service := NewStrategyConditionAIService()
	service.settingsProvider = func() *data.SettingConfig {
		return &data.SettingConfig{AiConfigs: []*data.AIConfig{defaultConfig, selectedConfig}}
	}
	var capturedConfig data.AIConfig
	service.modelFactory = func(_ context.Context, config data.AIConfig) (model.ToolCallingChatModel, error) {
		capturedConfig = config
		return fakeModel, nil
	}

	result, err := service.Generate(context.Background(), StrategyConditionAIRequest{
		RequestID:    "strategy-condition-1",
		Instruction:  "增加量比大于1.2，并整理成清晰条件",
		CurrentQuery: "近20日涨幅大于20%，排除ST股",
		AIConfigID:   int(selectedConfig.ID),
	})
	require.NoError(t, err)
	assert.Equal(t, "沪深A股；近20日涨幅大于20%；量比大于1.2；排除ST股、退市股、科创板和创业板", result.Query)
	assert.Equal(t, "增加量比并整理排除项", result.Summary)
	assert.Equal(t, []string{"请在实际执行选股后核对上游识别条件"}, result.Warnings)
	assert.Equal(t, selectedConfig.ID, capturedConfig.ID)
	assert.False(t, capturedConfig.Thinking)
	assert.Equal(t, data.ReasoningModeOff, capturedConfig.ReasoningMode)
	assert.Empty(t, capturedConfig.ReasoningEffort)
	// 请求级覆盖不能修改设置缓存中的源配置。
	assert.True(t, selectedConfig.Thinking)
	assert.Equal(t, data.ReasoningModeOn, selectedConfig.ReasoningMode)
	assert.Equal(t, "high", selectedConfig.ReasoningEffort)

	require.Len(t, fakeModel.messages, 2)
	var payload struct {
		CurrentQuery string `json:"currentQuery"`
		Instruction  string `json:"instruction"`
	}
	require.NoError(t, json.Unmarshal([]byte(fakeModel.messages[1].Content), &payload))
	assert.Equal(t, "近20日涨幅大于20%，排除ST股", payload.CurrentQuery)
	assert.Equal(t, "增加量比大于1.2，并整理成清晰条件", payload.Instruction)

	var strategyCount int64
	require.NoError(t, db.Dao.Model(&models.CustomStrategy{}).Count(&strategyCount).Error)
	assert.Zero(t, strategyCount, "AI 建议稿不得写入策略表")
}

func TestStrategyConditionGenerateUsesDefaultChatConfig(t *testing.T) {
	defaultConfig, selectedConfig := strategyConditionTestConfigs()
	service := NewStrategyConditionAIService()
	service.settingsProvider = func() *data.SettingConfig {
		return &data.SettingConfig{AiConfigs: []*data.AIConfig{selectedConfig, defaultConfig}}
	}
	var capturedID uint
	service.modelFactory = func(_ context.Context, config data.AIConfig) (model.ToolCallingChatModel, error) {
		capturedID = config.ID
		return &fakeStrategyConditionModel{response: `{"query":"沪深A股；市盈率小于20","summary":"生成低估值条件","warnings":[]}`}, nil
	}

	_, err := service.Generate(context.Background(), StrategyConditionAIRequest{
		RequestID: "strategy-condition-default", Instruction: "生成稳健低估值条件",
	})
	require.NoError(t, err)
	assert.Equal(t, defaultConfig.ID, capturedID)
}

func TestStrategyConditionGenerateValidatesInputsBeforeModel(t *testing.T) {
	service := NewStrategyConditionAIService()
	modelCalled := false
	service.settingsProvider = func() *data.SettingConfig {
		return &data.SettingConfig{AiConfigs: []*data.AIConfig{{ID: 1, ModelType: "chat"}}}
	}
	service.modelFactory = func(context.Context, data.AIConfig) (model.ToolCallingChatModel, error) {
		modelCalled = true
		return &fakeStrategyConditionModel{}, nil
	}

	tests := []struct {
		name string
		req  StrategyConditionAIRequest
		code string
	}{
		{name: "missing request id", req: StrategyConditionAIRequest{Instruction: "生成条件"}, code: "request_invalid"},
		{name: "empty instruction", req: StrategyConditionAIRequest{RequestID: "request"}, code: "instruction_empty"},
		{name: "long instruction", req: StrategyConditionAIRequest{RequestID: "request", Instruction: strings.Repeat("选", strategyConditionMaxInstructionRunes+1)}, code: "instruction_too_long"},
		{name: "long current query", req: StrategyConditionAIRequest{RequestID: "request", Instruction: "调整", CurrentQuery: strings.Repeat("条", strategyConditionMaxCurrentQueryRunes+1)}, code: "current_query_too_long"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.Generate(context.Background(), test.req)
			var safeErr *StrategyConditionAIError
			require.ErrorAs(t, err, &safeErr)
			assert.Equal(t, test.code, safeErr.Code)
		})
	}
	assert.False(t, modelCalled)
}

func TestStrategyConditionGenerateRejectsInvalidModelOutput(t *testing.T) {
	config := &data.AIConfig{ID: 1, ModelType: "chat", IsDefault: true}
	service := NewStrategyConditionAIService()
	service.settingsProvider = func() *data.SettingConfig {
		return &data.SettingConfig{AiConfigs: []*data.AIConfig{config}}
	}

	tests := []struct {
		name     string
		response string
		code     string
	}{
		{name: "empty response", response: ``, code: "empty_result"},
		{name: "invalid json", response: `not-json`, code: "invalid_result"},
		{name: "unknown field", response: `{"query":"市盈率小于20","summary":"摘要","warnings":[],"extra":true}`, code: "invalid_result"},
		{name: "empty query", response: `{"query":"  ","summary":"摘要","warnings":[]}`, code: "empty_result"},
		{name: "markdown query", response: `{"query":"## 条件\\n- 市盈率小于20","summary":"摘要","warnings":[]}`, code: "invalid_result"},
		{name: "long query", response: `{"query":"` + strings.Repeat("条", strategyConditionMaxQueryRunes+1) + `","summary":"摘要","warnings":[]}`, code: "result_too_long"},
		{name: "too many warnings", response: `{"query":"市盈率小于20","summary":"摘要","warnings":["1","2","3","4","5","6"]}`, code: "result_too_long"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service.modelFactory = func(context.Context, data.AIConfig) (model.ToolCallingChatModel, error) {
				return &fakeStrategyConditionModel{response: test.response}, nil
			}
			_, err := service.Generate(context.Background(), StrategyConditionAIRequest{RequestID: "request", Instruction: "生成条件"})
			var safeErr *StrategyConditionAIError
			require.ErrorAs(t, err, &safeErr)
			assert.Equal(t, test.code, safeErr.Code)
		})
	}
}

func TestStrategyConditionGenerateReturnsSafeFailureAndCancellation(t *testing.T) {
	config := &data.AIConfig{ID: 1, ModelType: "chat", IsDefault: true}
	service := NewStrategyConditionAIService()
	service.settingsProvider = func() *data.SettingConfig {
		return &data.SettingConfig{AiConfigs: []*data.AIConfig{config}}
	}
	service.modelFactory = func(context.Context, data.AIConfig) (model.ToolCallingChatModel, error) {
		return &fakeStrategyConditionModel{err: errors.New("provider secret body")}, nil
	}
	_, err := service.Generate(context.Background(), StrategyConditionAIRequest{RequestID: "failed", Instruction: "生成条件"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret")
	var safeErr *StrategyConditionAIError
	require.ErrorAs(t, err, &safeErr)
	assert.Equal(t, "generation_failed", safeErr.Code)

	service.modelFactory = func(context.Context, data.AIConfig) (model.ToolCallingChatModel, error) {
		return &fakeStrategyConditionModel{wait: true}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.Generate(ctx, StrategyConditionAIRequest{RequestID: "cancelled", Instruction: "生成条件"})
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorAs(t, err, &safeErr)
	assert.Equal(t, "cancelled", safeErr.Code)
}

func TestStrategyConditionGenerateRejectsMissingExplicitConfig(t *testing.T) {
	defaultConfig, _ := strategyConditionTestConfigs()
	service := NewStrategyConditionAIService()
	service.settingsProvider = func() *data.SettingConfig {
		return &data.SettingConfig{AiConfigs: []*data.AIConfig{defaultConfig}}
	}
	_, err := service.Generate(context.Background(), StrategyConditionAIRequest{
		RequestID: "missing-config", Instruction: "生成条件", AIConfigID: 999,
	})
	var safeErr *StrategyConditionAIError
	require.ErrorAs(t, err, &safeErr)
	assert.Equal(t, "config_missing", safeErr.Code)
}
