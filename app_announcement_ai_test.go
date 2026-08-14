package main

import (
	"context"
	"errors"
	"go-stock/backend/data"
	"go-stock/backend/models"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type announcementEventRecorder struct {
	mu     sync.Mutex
	events []models.AnnouncementAIAnalysisEvent
}

func (r *announcementEventRecorder) Emit(name string, args ...any) {
	if name != models.AnnouncementAIEventName || len(args) != 1 {
		return
	}
	event, ok := args[0].(models.AnnouncementAIAnalysisEvent)
	if !ok {
		return
	}
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func (r *announcementEventRecorder) phases() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]string, 0, len(r.events))
	for _, event := range r.events {
		result = append(result, event.Phase)
	}
	return result
}

func withAnnouncementWorkerStubs(t *testing.T) {
	t.Helper()
	originalDownload := downloadAnnouncementText
	originalStream := streamAnnouncementOnce
	originalUpsert := upsertAnnouncementResult
	t.Cleanup(func() {
		downloadAnnouncementText = originalDownload
		streamAnnouncementOnce = originalStream
		upsertAnnouncementResult = originalUpsert
	})
}

func TestRunAnnouncementAIAnalysisPersistsBeforeCompleted(t *testing.T) {
	withAnnouncementWorkerStubs(t)
	recorder := &announcementEventRecorder{}
	app := &App{ctx: context.Background(), eventEmitter: recorder, announcementReqID: "req-1"}
	req := models.AnnouncementAIAnalysisRequest{
		ArtCode: "AN202608130001", StockCode: "600000", StockName: "测试公司",
		Title: "公告", NoticeType: "公司公告", NoticeDate: "2026-08-13", AIConfigID: 1,
	}
	config := data.AIConfig{
		ID: 1, Name: "test", BaseUrl: "https://custom.example/v1", ApiKey: "secret",
		ModelName: "custom-model", MaxTokens: 4096, TimeOut: 30,
		ResponseFormat: "text", ReasoningMode: data.ReasoningModeOff,
	}
	downloadAnnouncementText = func(context.Context, string) (string, string, error) {
		return "这是足够长度的公告原文，用于测试公告解读生成和保存顺序。", "https://pdf.dfcfw.com/pdf/H2_AN202608130001_1.pdf", nil
	}
	streamAnnouncementOnce = func(_ context.Context, _ data.AIConfig, _, _ string, onChunk func(data.AnnouncementStreamChunk) error) error {
		require.NoError(t, onChunk(data.AnnouncementStreamChunk{Content: "解读", ModelName: "returned-model", ResponseID: "chat-1"}))
		return nil
	}
	var saved *models.AnnouncementAIAnalysis
	upsertAnnouncementResult = func(result *models.AnnouncementAIAnalysis) error {
		copy := *result
		saved = &copy
		return nil
	}
	app.runAnnouncementAIAnalysis(context.Background(), "req-1", req, config)
	require.NotNil(t, saved)
	assert.Equal(t, "解读", saved.Content)
	assert.Equal(t, "returned-model", saved.ModelName)
	assert.Equal(t, []string{
		models.AnnouncementAIPhasePreparing,
		models.AnnouncementAIPhasePreflight,
		models.AnnouncementAIPhaseStreaming,
		models.AnnouncementAIPhaseCompleted,
	}, recorder.phases())
}

func TestRunAnnouncementAIAnalysisCancellationDoesNotPersist(t *testing.T) {
	withAnnouncementWorkerStubs(t)
	recorder := &announcementEventRecorder{}
	app := &App{ctx: context.Background(), eventEmitter: recorder, announcementReqID: "req-2"}
	req := models.AnnouncementAIAnalysisRequest{
		ArtCode: "AN202608130002", StockCode: "600000", StockName: "测试公司",
		Title: "公告", NoticeType: "公司公告", NoticeDate: "2026-08-13", AIConfigID: 1,
	}
	config := data.AIConfig{ModelName: "custom-model", BaseUrl: "https://custom.example/v1"}
	downloadAnnouncementText = func(context.Context, string) (string, string, error) {
		return "这是足够长度的公告原文，用于测试取消不会写入保存结果。", "https://pdf.dfcfw.com/pdf/H2_AN202608130002_1.pdf", nil
	}
	streamAnnouncementOnce = func(context.Context, data.AIConfig, string, string, func(data.AnnouncementStreamChunk) error) error {
		return context.Canceled
	}
	upsertAnnouncementResult = func(*models.AnnouncementAIAnalysis) error {
		return errors.New("must not persist")
	}

	app.runAnnouncementAIAnalysis(context.Background(), "req-2", req, config)
	assert.Equal(t, []string{
		models.AnnouncementAIPhasePreparing,
		models.AnnouncementAIPhasePreflight,
		models.AnnouncementAIPhaseCancelled,
	}, recorder.phases())
}

func TestAbortAnnouncementAIAnalysisIgnoresStaleRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := &App{announcementReqID: "active", announcementCancel: cancel}
	app.AbortAnnouncementAIAnalysis("stale")
	assert.NoError(t, ctx.Err())
	app.AbortAnnouncementAIAnalysis("active")
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestRunAnnouncementAIAnalysisCancellationAfterStreamDoesNotPersist(t *testing.T) {
	withAnnouncementWorkerStubs(t)
	ctx, cancel := context.WithCancel(context.Background())
	recorder := &announcementEventRecorder{}
	app := &App{ctx: context.Background(), eventEmitter: recorder, announcementReqID: "req-3"}
	req := models.AnnouncementAIAnalysisRequest{
		ArtCode: "AN202608130003", StockCode: "600000", StockName: "测试公司",
		Title: "公告", NoticeType: "公司公告", NoticeDate: "2026-08-13", AIConfigID: 1,
	}
	config := data.AIConfig{ModelName: "custom-model", BaseUrl: "https://custom.example/v1"}
	downloadAnnouncementText = func(context.Context, string) (string, string, error) {
		return "这是足够长度的公告原文，用于测试流结束后的取消不会写入保存结果。", "https://pdf.dfcfw.com/pdf/H2_AN202608130003_1.pdf", nil
	}
	streamAnnouncementOnce = func(_ context.Context, _ data.AIConfig, _, _ string, onChunk func(data.AnnouncementStreamChunk) error) error {
		require.NoError(t, onChunk(data.AnnouncementStreamChunk{Content: "解读"}))
		cancel()
		return nil
	}
	var persisted atomic.Bool
	upsertAnnouncementResult = func(*models.AnnouncementAIAnalysis) error {
		persisted.Store(true)
		return nil
	}

	app.runAnnouncementAIAnalysis(ctx, "req-3", req, config)
	assert.False(t, persisted.Load())
	assert.Equal(t, models.AnnouncementAIPhaseCancelled, recorder.phases()[len(recorder.phases())-1])
}

func TestRunAnnouncementAIAnalysisStaleRequestDoesNotPersist(t *testing.T) {
	withAnnouncementWorkerStubs(t)
	recorder := &announcementEventRecorder{}
	app := &App{ctx: context.Background(), eventEmitter: recorder, announcementReqID: "new-request"}
	req := models.AnnouncementAIAnalysisRequest{
		ArtCode: "AN202608130004", StockCode: "600000", StockName: "测试公司",
		Title: "公告", NoticeType: "公司公告", NoticeDate: "2026-08-13", AIConfigID: 1,
	}
	config := data.AIConfig{ModelName: "custom-model", BaseUrl: "https://custom.example/v1"}
	downloadAnnouncementText = func(context.Context, string) (string, string, error) {
		return "这是足够长度的公告原文，用于测试过期请求不会覆盖已经启动的新请求。", "https://pdf.dfcfw.com/pdf/H2_AN202608130004_1.pdf", nil
	}
	streamAnnouncementOnce = func(_ context.Context, _ data.AIConfig, _, _ string, onChunk func(data.AnnouncementStreamChunk) error) error {
		return onChunk(data.AnnouncementStreamChunk{Content: "旧请求解读"})
	}
	var persisted atomic.Bool
	upsertAnnouncementResult = func(*models.AnnouncementAIAnalysis) error {
		persisted.Store(true)
		return nil
	}

	app.runAnnouncementAIAnalysis(context.Background(), "old-request", req, config)
	assert.False(t, persisted.Load())
	assert.Equal(t, models.AnnouncementAIPhaseCancelled, recorder.phases()[len(recorder.phases())-1])
}

func TestRunAnnouncementAIAnalysisPreflightRejectsBeforeModelRequest(t *testing.T) {
	withAnnouncementWorkerStubs(t)
	recorder := &announcementEventRecorder{}
	app := &App{ctx: context.Background(), eventEmitter: recorder, announcementReqID: "req-too-large"}
	req := models.AnnouncementAIAnalysisRequest{
		ArtCode: "AN202608130005", StockCode: "600000", StockName: "测试公司",
		Title: "超长公告", NoticeType: "定期报告", NoticeDate: "2026-08-13", AIConfigID: 1,
	}
	config := data.AIConfig{ModelName: "unknown-custom-model", BaseUrl: "https://custom.example/v1"}
	downloadAnnouncementText = func(context.Context, string) (string, string, error) {
		return strings.Repeat("公", 200000), "https://pdf.dfcfw.com/pdf/H2_AN202608130005_1.pdf", nil
	}
	var modelCalled atomic.Bool
	streamAnnouncementOnce = func(context.Context, data.AIConfig, string, string, func(data.AnnouncementStreamChunk) error) error {
		modelCalled.Store(true)
		return nil
	}
	var persisted atomic.Bool
	upsertAnnouncementResult = func(*models.AnnouncementAIAnalysis) error {
		persisted.Store(true)
		return nil
	}

	app.runAnnouncementAIAnalysis(context.Background(), "req-too-large", req, config)
	assert.False(t, modelCalled.Load())
	assert.False(t, persisted.Load())
	assert.Equal(t, []string{
		models.AnnouncementAIPhasePreparing,
		models.AnnouncementAIPhasePreflight,
		models.AnnouncementAIPhaseFailed,
	}, recorder.phases())
}
