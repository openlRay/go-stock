package main

import (
	"context"
	"errors"
	"go-stock/backend/agent"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAbortStrategyConditionGenerationOnlyCancelsMatchingRequest(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan error, 1)
	app := &App{}
	app.strategyAIGenerate = func(ctx context.Context, _ agent.StrategyConditionAIRequest) (*agent.StrategyConditionAIResult, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	go func() {
		_, err := app.GenerateStrategyCondition(agent.StrategyConditionAIRequest{RequestID: "active", Instruction: "生成条件"})
		finished <- err
	}()
	<-started

	app.AbortStrategyConditionGeneration("stale")
	select {
	case err := <-finished:
		t.Fatalf("stale request cancelled active generation: %v", err)
	default:
	}
	app.AbortStrategyConditionGeneration("active")
	err := <-finished
	assert.Contains(t, err.Error(), "已取消")
	assert.Empty(t, app.strategyAIReqID)
	assert.Nil(t, app.strategyAICancel)
}

func TestGenerateStrategyConditionLatestRequestWins(t *testing.T) {
	firstStarted := make(chan struct{})
	firstFinished := make(chan error, 1)
	app := &App{}
	app.strategyAIGenerate = func(ctx context.Context, req agent.StrategyConditionAIRequest) (*agent.StrategyConditionAIResult, error) {
		if req.RequestID == "first" {
			close(firstStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return &agent.StrategyConditionAIResult{Query: "市盈率小于20"}, nil
	}
	go func() {
		_, err := app.GenerateStrategyCondition(agent.StrategyConditionAIRequest{RequestID: "first", Instruction: "首次生成"})
		firstFinished <- err
	}()
	<-firstStarted

	result, err := app.GenerateStrategyCondition(agent.StrategyConditionAIRequest{RequestID: "second", Instruction: "新的生成"})
	require.NoError(t, err)
	assert.Equal(t, "市盈率小于20", result.Query)
	assert.Contains(t, (<-firstFinished).Error(), "已取消")
	assert.Empty(t, app.strategyAIReqID)
	assert.Nil(t, app.strategyAICancel)
}

func TestClearStrategyConditionRequestDoesNotClearNewRequest(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := &App{strategyAIReqID: "new", strategyAICancel: cancel, strategyAIGeneration: 2}
	app.clearStrategyConditionRequest("old", 1)
	assert.Equal(t, "new", app.strategyAIReqID)
	assert.NotNil(t, app.strategyAICancel)
}

func TestGenerateStrategyConditionSameRequestIDKeepsLatestCancelState(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	firstFinished := make(chan error, 1)
	secondFinished := make(chan error, 1)
	var calls atomic.Int32
	app := &App{}
	app.strategyAIGenerate = func(ctx context.Context, _ agent.StrategyConditionAIRequest) (*agent.StrategyConditionAIResult, error) {
		if calls.Add(1) == 1 {
			close(firstStarted)
		} else {
			close(secondStarted)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}

	request := agent.StrategyConditionAIRequest{RequestID: "reused", Instruction: "生成条件"}
	go func() {
		_, err := app.GenerateStrategyCondition(request)
		firstFinished <- err
	}()
	<-firstStarted
	go func() {
		_, err := app.GenerateStrategyCondition(request)
		secondFinished <- err
	}()
	<-secondStarted

	assert.Contains(t, (<-firstFinished).Error(), "已取消")
	app.strategyAIMu.Lock()
	assert.Equal(t, "reused", app.strategyAIReqID)
	assert.NotNil(t, app.strategyAICancel)
	app.strategyAIMu.Unlock()

	app.AbortStrategyConditionGeneration("reused")
	assert.Contains(t, (<-secondFinished).Error(), "已取消")
	assert.Empty(t, app.strategyAIReqID)
	assert.Nil(t, app.strategyAICancel)
}

func TestGenerateStrategyConditionReturnsSafeErrors(t *testing.T) {
	app := &App{}
	if _, err := app.GenerateStrategyCondition(agent.StrategyConditionAIRequest{Instruction: "生成条件"}); err == nil {
		t.Fatal("missing request ID should fail")
	}
	app.strategyAIGenerate = func(context.Context, agent.StrategyConditionAIRequest) (*agent.StrategyConditionAIResult, error) {
		return nil, errors.New("provider secret body")
	}
	_, err := app.GenerateStrategyCondition(agent.StrategyConditionAIRequest{RequestID: "safe-error", Instruction: "生成条件"})
	require.Error(t, err)
	assert.NotContains(t, strings.ToLower(err.Error()), "secret")
	assert.Contains(t, err.Error(), "稍后重试")
}
