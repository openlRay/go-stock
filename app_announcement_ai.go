package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"go-stock/backend/data"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"strings"
	"time"
)

var (
	downloadAnnouncementText = data.DownloadAndExtractAnnouncementText
	streamAnnouncementOnce   = data.StreamAnnouncementAIOnce
	upsertAnnouncementResult = data.UpsertAnnouncementAIAnalysis
)

func (a *App) GetAnnouncementAIAnalysis(artCode string) (*models.AnnouncementAIAnalysis, error) {
	return data.GetAnnouncementAIAnalysis(artCode)
}

func (a *App) StartAnnouncementAIAnalysis(req models.AnnouncementAIAnalysisRequest) (string, error) {
	normalized, err := data.ValidateAnnouncementAIRequest(req)
	if err != nil {
		return "", err
	}
	config, err := data.FindAnnouncementAIConfig(normalized.AIConfigID)
	if err != nil {
		return "", err
	}
	requestID, err := newAnnouncementRequestID()
	if err != nil {
		return "", models.NewAnnouncementAIError("start_failed", "无法启动公告解读，请稍后重试", err)
	}
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)

	a.announcementMu.Lock()
	previousCancel := a.announcementCancel
	a.announcementCancel = cancel
	a.announcementReqID = requestID
	a.announcementMu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}

	go a.runAnnouncementAIAnalysis(ctx, requestID, normalized, *config)
	return requestID, nil
}

func (a *App) AbortAnnouncementAIAnalysis(requestID string) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	a.announcementMu.Lock()
	if a.announcementReqID != requestID {
		a.announcementMu.Unlock()
		return
	}
	cancel := a.announcementCancel
	a.announcementReqID = ""
	a.announcementCancel = nil
	a.announcementMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func newAnnouncementRequestID() (string, error) {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "announcement-" + hex.EncodeToString(buf[:]), nil
}

func (a *App) runAnnouncementAIAnalysis(ctx context.Context, requestID string, req models.AnnouncementAIAnalysisRequest, config data.AIConfig) {
	defer a.clearAnnouncementRequest(requestID)
	a.emitAnnouncementEvent(models.AnnouncementAIAnalysisEvent{
		RequestID: requestID, ArtCode: req.ArtCode, Phase: models.AnnouncementAIPhasePreparing,
		Message: "正在下载并提取公告原文…",
	})

	text, pdfURL, err := downloadAnnouncementText(ctx, req.ArtCode)
	if err != nil {
		a.finishAnnouncementWithError(ctx, requestID, req.ArtCode, err)
		return
	}
	if err := ctx.Err(); err != nil {
		a.finishAnnouncementWithError(ctx, requestID, req.ArtCode, err)
		return
	}

	systemPrompt, userPrompt := data.BuildAnnouncementPrompts(req, text)
	preflight := data.PreflightAnnouncementAI(config, systemPrompt, userPrompt)
	a.emitAnnouncementEvent(models.AnnouncementAIAnalysisEvent{
		RequestID: requestID, ArtCode: req.ArtCode, Phase: models.AnnouncementAIPhasePreflight,
		Message: "已完成上下文容量预检", Preflight: &preflight,
	})
	if !preflight.Allowed {
		err := models.AnnouncementAIErrorf(
			"context_too_large",
			"公告预计需要约 %d Token，所选模型上下文容量按 %d Token（%s）计算，超过安全预算 %d Token；请更换长上下文模型",
			preflight.EstimatedTotalTokens, preflight.ContextWindow, preflight.CapacitySource, preflight.SafeBudget,
		)
		a.finishAnnouncementWithError(ctx, requestID, req.ArtCode, err)
		return
	}

	var content strings.Builder
	modelName := strings.TrimSpace(config.ModelName)
	responseID := ""
	err = streamAnnouncementOnce(ctx, config, systemPrompt, userPrompt, func(chunk data.AnnouncementStreamChunk) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		content.WriteString(chunk.Content)
		if chunk.ModelName != "" {
			modelName = chunk.ModelName
		}
		if chunk.ResponseID != "" {
			responseID = chunk.ResponseID
		}
		a.emitAnnouncementEvent(models.AnnouncementAIAnalysisEvent{
			RequestID: requestID, ArtCode: req.ArtCode, Phase: models.AnnouncementAIPhaseStreaming,
			Message: "AI 正在解读公告…", Delta: chunk.Content, ModelName: modelName, ResponseID: responseID,
		})
		return nil
	})
	if err != nil {
		a.finishAnnouncementWithError(ctx, requestID, req.ArtCode, err)
		return
	}
	if err := ctx.Err(); err != nil {
		a.finishAnnouncementWithError(ctx, requestID, req.ArtCode, err)
		return
	}
	finalContent := strings.TrimSpace(content.String())
	if finalContent == "" {
		a.finishAnnouncementWithError(ctx, requestID, req.ArtCode,
			models.NewAnnouncementAIError("empty_result", "AI 模型未返回可用的公告解读，未保存本次结果", nil))
		return
	}
	result := &models.AnnouncementAIAnalysis{
		ArtCode: req.ArtCode, StockCode: req.StockCode, StockName: req.StockName,
		Title: req.Title, NoticeType: req.NoticeType, NoticeDate: req.NoticeDate, PDFURL: pdfURL,
		AIConfigID: config.ID, ModelName: modelName, PromptVersion: data.AnnouncementAIPromptVersion,
		InstructionID: data.AnnouncementAIInstructionID, Content: finalContent,
		ProviderResponseID: responseID, GeneratedAt: time.Now(),
	}
	if err := ctx.Err(); err != nil {
		a.finishAnnouncementWithError(ctx, requestID, req.ArtCode, err)
		return
	}
	stored, err := a.persistAnnouncementResultIfActive(ctx, requestID, result)
	if err != nil {
		a.finishAnnouncementWithError(ctx, requestID, req.ArtCode, err)
		return
	}
	a.emitAnnouncementEvent(models.AnnouncementAIAnalysisEvent{
		RequestID: requestID, ArtCode: req.ArtCode, Phase: models.AnnouncementAIPhaseCompleted,
		Message: "公告 AI 解读已完成并保存", ModelName: stored.ModelName,
		ResponseID: stored.ProviderResponseID, Preflight: &preflight, Result: stored,
	})
}

func (a *App) persistAnnouncementResultIfActive(ctx context.Context, requestID string, result *models.AnnouncementAIAnalysis) (*models.AnnouncementAIAnalysis, error) {
	a.announcementMu.Lock()
	defer a.announcementMu.Unlock()
	if err := ctx.Err(); err != nil || a.announcementReqID != requestID {
		return nil, context.Canceled
	}
	if err := upsertAnnouncementResult(result); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *App) emitAnnouncementEvent(event models.AnnouncementAIAnalysisEvent) {
	a.emit(models.AnnouncementAIEventName, event)
}

func (a *App) finishAnnouncementWithError(ctx context.Context, requestID, artCode string, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		a.emitAnnouncementEvent(models.AnnouncementAIAnalysisEvent{
			RequestID: requestID, ArtCode: artCode, Phase: models.AnnouncementAIPhaseCancelled,
			Message: "已取消本次公告解读，已保存的旧结果不受影响",
		})
		return
	}
	code := "analysis_failed"
	message := "公告 AI 解读失败，请稍后重试"
	var safeErr *models.AnnouncementAIError
	if errors.As(err, &safeErr) {
		code = safeErr.Code
		message = safeErr.Message
	}
	logger.SugaredLogger.Warnf("announcement AI analysis failed art_code=%s request_id=%s code=%s error=%v", artCode, requestID, code, err)
	a.emitAnnouncementEvent(models.AnnouncementAIAnalysisEvent{
		RequestID: requestID, ArtCode: artCode, Phase: models.AnnouncementAIPhaseFailed,
		Message: message, ErrorCode: code,
	})
}

func (a *App) clearAnnouncementRequest(requestID string) {
	a.announcementMu.Lock()
	defer a.announcementMu.Unlock()
	if a.announcementReqID != requestID {
		return
	}
	a.announcementReqID = ""
	a.announcementCancel = nil
}
