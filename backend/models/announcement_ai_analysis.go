package models

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

const (
	AnnouncementAIEventName = "announcementAIAnalysis"

	AnnouncementAIPhasePreparing = "preparing"
	AnnouncementAIPhasePreflight = "preflight"
	AnnouncementAIPhaseStreaming = "streaming"
	AnnouncementAIPhaseCompleted = "completed"
	AnnouncementAIPhaseFailed    = "failed"
	AnnouncementAIPhaseCancelled = "cancelled"
)

type AnnouncementAIAnalysisRequest struct {
	ArtCode    string `json:"artCode"`
	StockCode  string `json:"stockCode"`
	StockName  string `json:"stockName"`
	Title      string `json:"title"`
	NoticeType string `json:"noticeType"`
	NoticeDate string `json:"noticeDate"`
	AIConfigID uint   `json:"aiConfigId"`
}

type AnnouncementAIPreflight struct {
	EstimatedInputTokens int    `json:"estimatedInputTokens"`
	ReservedOutputTokens int    `json:"reservedOutputTokens"`
	EstimatedTotalTokens int    `json:"estimatedTotalTokens"`
	ContextWindow        int    `json:"contextWindow"`
	SafeBudget           int    `json:"safeBudget"`
	CapacitySource       string `json:"capacitySource"`
	UsedDefaultCapacity  bool   `json:"usedDefaultCapacity"`
	Allowed              bool   `json:"allowed"`
}

type AnnouncementAIAnalysisEvent struct {
	RequestID  string                   `json:"requestId"`
	ArtCode    string                   `json:"artCode"`
	Phase      string                   `json:"phase"`
	Message    string                   `json:"message,omitempty"`
	Delta      string                   `json:"delta,omitempty"`
	ModelName  string                   `json:"modelName,omitempty"`
	ResponseID string                   `json:"responseId,omitempty"`
	ErrorCode  string                   `json:"errorCode,omitempty"`
	Preflight  *AnnouncementAIPreflight `json:"preflight,omitempty"`
	Result     *AnnouncementAIAnalysis  `json:"result,omitempty"`
}

type AnnouncementAIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Cause   error  `json:"-"`
}

func (e *AnnouncementAIError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *AnnouncementAIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func NewAnnouncementAIError(code, message string, cause error) *AnnouncementAIError {
	return &AnnouncementAIError{Code: code, Message: message, Cause: cause}
}

func AnnouncementAIErrorf(code, format string, args ...any) *AnnouncementAIError {
	return NewAnnouncementAIError(code, fmt.Sprintf(format, args...), nil)
}

type AnnouncementAIAnalysis struct {
	gorm.Model
	ArtCode            string    `json:"artCode" gorm:"uniqueIndex;size:80;not null"`
	StockCode          string    `json:"stockCode" gorm:"size:32;not null"`
	StockName          string    `json:"stockName" gorm:"size:120;not null"`
	Title              string    `json:"title" gorm:"size:500;not null"`
	NoticeType         string    `json:"noticeType" gorm:"size:160;not null"`
	NoticeDate         string    `json:"noticeDate" gorm:"size:32;not null"`
	PDFURL             string    `json:"pdfUrl" gorm:"size:500;not null"`
	AIConfigID         uint      `json:"aiConfigId" gorm:"not null"`
	ModelName          string    `json:"modelName" gorm:"size:200;not null"`
	PromptVersion      string    `json:"promptVersion" gorm:"size:80;not null"`
	InstructionID      string    `json:"instructionId" gorm:"size:80;not null"`
	Content            string    `json:"content" gorm:"type:text;not null"`
	ProviderResponseID string    `json:"providerResponseId" gorm:"size:200"`
	GeneratedAt        time.Time `json:"generatedAt" gorm:"not null"`
}

func (AnnouncementAIAnalysis) TableName() string {
	return "announcement_ai_analysis"
}
