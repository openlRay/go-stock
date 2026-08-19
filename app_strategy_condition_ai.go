package main

import (
	"context"
	"errors"
	"fmt"
	"go-stock/backend/agent"
	"go-stock/backend/logger"
	"strings"
)

// GenerateStrategyCondition 生成独立建议稿；它不会保存策略或修改现有选股条件。
func (a *App) GenerateStrategyCondition(req agent.StrategyConditionAIRequest) (*agent.StrategyConditionAIResult, error) {
	req.RequestID = strings.TrimSpace(req.RequestID)
	if req.RequestID == "" {
		return nil, fmt.Errorf("缺少本次生成请求标识")
	}

	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	// generation 区分复用同一 request ID 的连续请求，避免旧请求的 defer 清理新请求状态。
	a.strategyAIMu.Lock()
	previousCancel := a.strategyAICancel
	a.strategyAIGeneration++
	generation := a.strategyAIGeneration
	a.strategyAICancel = cancel
	a.strategyAIReqID = req.RequestID
	a.strategyAIMu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	defer a.clearStrategyConditionRequest(req.RequestID, generation)

	generator := a.strategyAIGenerate
	if generator == nil {
		service := agent.NewStrategyConditionAIService()
		generator = service.Generate
	}
	result, err := generator(ctx, req)
	if err == nil {
		return result, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return nil, fmt.Errorf("已取消本次选股条件生成")
	}
	var safeErr *agent.StrategyConditionAIError
	if errors.As(err, &safeErr) {
		return nil, safeErr
	}
	// 未分类错误不记录 prompt、模型响应或底层错误正文，避免意外泄露敏感信息。
	logger.SugaredLogger.Warnf("strategy condition generation failed request_id=%s", req.RequestID)
	return nil, fmt.Errorf("AI 生成选股条件失败，请稍后重试")
}

// AbortStrategyConditionGeneration 只取消仍与 requestID 匹配的活动请求。
func (a *App) AbortStrategyConditionGeneration(requestID string) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	a.strategyAIMu.Lock()
	if a.strategyAIReqID != requestID {
		a.strategyAIMu.Unlock()
		return
	}
	cancel := a.strategyAICancel
	a.strategyAIReqID = ""
	a.strategyAICancel = nil
	a.strategyAIMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) clearStrategyConditionRequest(requestID string, generation uint64) {
	a.strategyAIMu.Lock()
	defer a.strategyAIMu.Unlock()
	if a.strategyAIReqID != requestID || a.strategyAIGeneration != generation {
		return
	}
	a.strategyAIReqID = ""
	a.strategyAICancel = nil
}
