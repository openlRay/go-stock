package data

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-resty/resty/v2"
	"github.com/ledongthuc/pdf"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	AnnouncementAIPromptVersion = "announcement-analysis-v1"
	AnnouncementAIInstructionID = "announcement-source-only-analysis"

	announcementPDFBaseURL       = "https://pdf.dfcfw.com/pdf/"
	announcementPDFMaxBytes      = 30 * 1024 * 1024
	announcementPDFTimeout       = 45 * time.Second
	announcementDefaultContext   = 200000
	announcementOutputReserve    = 8192
	announcementMessageOverhead  = 96
	announcementMinimumTextRunes = 20
)

var announcementArtCodePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,80}$`)

type AnnouncementStreamChunk struct {
	Content    string
	ModelName  string
	ResponseID string
}

type AnnouncementStreamFunc func(context.Context, AIConfig, string, string, func(AnnouncementStreamChunk) error) error

func normalizeAnnouncementRequest(req models.AnnouncementAIAnalysisRequest) models.AnnouncementAIAnalysisRequest {
	req.ArtCode = strings.TrimSpace(req.ArtCode)
	req.StockCode = strings.TrimSpace(req.StockCode)
	req.StockName = strings.TrimSpace(req.StockName)
	req.Title = strings.TrimSpace(req.Title)
	req.NoticeType = strings.TrimSpace(req.NoticeType)
	req.NoticeDate = strings.TrimSpace(req.NoticeDate)
	return req
}

func ValidateAnnouncementAIRequest(req models.AnnouncementAIAnalysisRequest) (models.AnnouncementAIAnalysisRequest, error) {
	req = normalizeAnnouncementRequest(req)
	if !announcementArtCodePattern.MatchString(req.ArtCode) {
		return req, models.NewAnnouncementAIError("invalid_metadata", "公告编号无效，请刷新公告列表后重试", nil)
	}
	if req.StockCode == "" || req.StockName == "" || req.Title == "" || req.NoticeType == "" || req.NoticeDate == "" {
		return req, models.NewAnnouncementAIError("invalid_metadata", "公告元数据不完整，请刷新公告列表后重试", nil)
	}
	if req.AIConfigID == 0 {
		return req, models.NewAnnouncementAIError("no_ai_config", "请选择可用的 AI 模型配置", nil)
	}
	if len(req.StockCode) > 32 || len(req.StockName) > 120 || len(req.Title) > 500 || len(req.NoticeType) > 160 || len(req.NoticeDate) > 32 {
		return req, models.NewAnnouncementAIError("invalid_metadata", "公告元数据长度异常，请刷新公告列表后重试", nil)
	}
	return req, nil
}

func ValidateAnnouncementArtCode(artCode string) (string, error) {
	artCode = strings.TrimSpace(artCode)
	if !announcementArtCodePattern.MatchString(artCode) {
		return "", models.NewAnnouncementAIError("invalid_metadata", "公告编号无效，请刷新公告列表后重试", nil)
	}
	return artCode, nil
}

func AnnouncementPDFURL(artCode string) (string, error) {
	artCode, err := ValidateAnnouncementArtCode(artCode)
	if err != nil {
		return "", err
	}
	return announcementPDFBaseURL + "H2_" + url.PathEscape(artCode) + "_1.pdf", nil
}

func GetAnnouncementAIAnalysis(artCode string) (*models.AnnouncementAIAnalysis, error) {
	artCode, err := ValidateAnnouncementArtCode(artCode)
	if err != nil {
		return nil, err
	}
	var result models.AnnouncementAIAnalysis
	err = db.Dao.Where("art_code = ?", artCode).Limit(1).Take(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, models.NewAnnouncementAIError("load_failed", "读取已保存的公告解读失败，请稍后重试", err)
	}
	return &result, nil
}

func UpsertAnnouncementAIAnalysis(result *models.AnnouncementAIAnalysis) error {
	if result == nil || strings.TrimSpace(result.Content) == "" {
		return models.NewAnnouncementAIError("save_failed", "公告解读内容为空，未保存本次结果", nil)
	}
	result.ArtCode = strings.TrimSpace(result.ArtCode)
	if _, err := ValidateAnnouncementArtCode(result.ArtCode); err != nil {
		return err
	}
	if result.GeneratedAt.IsZero() {
		result.GeneratedAt = time.Now()
	}
	assignments := map[string]any{
		"stock_code": result.StockCode, "stock_name": result.StockName,
		"title": result.Title, "notice_type": result.NoticeType, "notice_date": result.NoticeDate,
		"pdf_url": result.PDFURL, "ai_config_id": result.AIConfigID, "model_name": result.ModelName,
		"prompt_version": result.PromptVersion, "instruction_id": result.InstructionID,
		"content": result.Content, "provider_response_id": result.ProviderResponseID,
		"generated_at": result.GeneratedAt, "updated_at": time.Now(),
	}
	return db.Dao.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "art_code"}},
			DoUpdates: clause.Assignments(assignments),
		}).Create(result).Error; err != nil {
			return models.NewAnnouncementAIError("save_failed", "保存公告解读失败，已保留之前的结果", err)
		}
		return nil
	})
}

func FindAnnouncementAIConfig(id uint) (*AIConfig, error) {
	if id == 0 {
		return nil, models.NewAnnouncementAIError("no_ai_config", "请选择可用的 AI 模型配置", nil)
	}
	var config AIConfig
	if err := db.Dao.First(&config, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, models.NewAnnouncementAIError("no_ai_config", "所选 AI 模型配置已不存在，请重新选择", err)
		}
		return nil, models.NewAnnouncementAIError("no_ai_config", "读取 AI 模型配置失败，请稍后重试", err)
	}
	NormalizeLegacyAIConfig(&config)
	if err := ValidateAIConfig(&config); err != nil {
		return nil, models.NewAnnouncementAIError("invalid_ai_config", "所选 AI 模型配置不可用，请先在设置中修正", err)
	}
	return &config, nil
}

func DownloadAndExtractAnnouncementText(ctx context.Context, artCode string) (string, string, error) {
	pdfURL, err := AnnouncementPDFURL(artCode)
	if err != nil {
		return "", "", err
	}
	client := CreateHTTPClientWithTimeout(announcementPDFTimeout)
	disableAnnouncementRedirects(client)
	body, err := downloadAnnouncementPDF(ctx, client, pdfURL)
	if err != nil {
		return "", pdfURL, err
	}
	text, err := ExtractAnnouncementPDFText(body)
	if err != nil {
		return "", pdfURL, err
	}
	return text, pdfURL, nil
}

func downloadAnnouncementPDF(ctx context.Context, client *resty.Client, sourceURL string) ([]byte, error) {
	if client == nil {
		return nil, models.NewAnnouncementAIError("download_failed", "公告原文下载失败，请稍后重试", nil)
	}
	resp, err := client.R().
		SetContext(ctx).
		SetDoNotParseResponse(true).
		SetHeader("Accept", "application/pdf").
		SetHeader("User-Agent", "Mozilla/5.0 go-stock announcement analysis").
		Get(sourceURL)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, context.Canceled
		}
		return nil, models.NewAnnouncementAIError("download_failed", "公告原文下载失败，请检查网络后重试", err)
	}
	defer resp.RawBody().Close()
	if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices {
		return nil, models.AnnouncementAIErrorf("download_failed", "公告原文下载失败（HTTP %d），请稍后重试", resp.StatusCode())
	}
	contentLength := resp.RawResponse.ContentLength
	if contentLength > announcementPDFMaxBytes {
		return nil, models.NewAnnouncementAIError("pdf_too_large", "公告 PDF 超过 30MB，暂不支持 AI 解读", nil)
	}
	data, readErr := io.ReadAll(io.LimitReader(resp.RawBody(), announcementPDFMaxBytes+1))
	if readErr != nil {
		return nil, models.NewAnnouncementAIError("download_failed", "读取公告原文失败，请稍后重试", readErr)
	}
	if len(data) == 0 {
		return nil, models.NewAnnouncementAIError("download_failed", "公告原文为空，请稍后重试", nil)
	}
	if len(data) > announcementPDFMaxBytes {
		return nil, models.NewAnnouncementAIError("pdf_too_large", "公告 PDF 超过 30MB，暂不支持 AI 解读", nil)
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header().Get("Content-Type"))
	if mediaType != "" && mediaType != "application/pdf" && mediaType != "application/octet-stream" {
		return nil, models.NewAnnouncementAIError("not_pdf", "公告原文不是有效的 PDF 文件，请打开原文核验", nil)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, models.NewAnnouncementAIError("not_pdf", "公告原文不是有效的 PDF 文件，请打开原文核验", nil)
	}
	return data, nil
}

func ExtractAnnouncementPDFText(data []byte) (string, error) {
	if len(data) == 0 || !bytes.HasPrefix(data, []byte("%PDF-")) {
		return "", models.NewAnnouncementAIError("not_pdf", "公告原文不是有效的 PDF 文件，请打开原文核验", nil)
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", models.NewAnnouncementAIError("pdf_parse_failed", "公告 PDF 解析失败，请打开原文核验", err)
	}
	plainReader, err := reader.GetPlainText()
	if err != nil {
		return "", models.NewAnnouncementAIError("pdf_parse_failed", "公告 PDF 文本提取失败，请打开原文核验", err)
	}
	plain, err := io.ReadAll(io.LimitReader(plainReader, announcementPDFMaxBytes+1))
	if err != nil {
		return "", models.NewAnnouncementAIError("pdf_parse_failed", "公告 PDF 文本提取失败，请打开原文核验", err)
	}
	if err := validateAnnouncementExtractedTextSize(len(plain)); err != nil {
		return "", err
	}
	text := NormalizeAnnouncementText(string(plain))
	if utf8.RuneCountInString(text) < announcementMinimumTextRunes {
		return "", models.NewAnnouncementAIError("empty_pdf_text", "公告 PDF 未提取到足够文本，可能是扫描件；当前版本暂不支持 OCR", nil)
	}
	return text, nil
}

func validateAnnouncementExtractedTextSize(size int) error {
	if size > announcementPDFMaxBytes {
		return models.NewAnnouncementAIError("pdf_text_too_large", "公告提取后的正文超过 30MB，暂不支持 AI 解读；正文不会被截断", nil)
	}
	return nil
}

func NormalizeAnnouncementText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimSpace(strings.Join(strings.Fields(line), " "))
		if line == "" {
			if !blank && len(cleaned) > 0 {
				cleaned = append(cleaned, "")
			}
			blank = true
			continue
		}
		cleaned = append(cleaned, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func EstimateAnnouncementTokens(text string) int {
	tokens := 0
	asciiRun := 0
	flushASCII := func() {
		if asciiRun > 0 {
			tokens += (asciiRun + 3) / 4
			asciiRun = 0
		}
	}
	for _, r := range text {
		switch {
		case r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			asciiRun++
		case unicode.IsSpace(r):
			flushASCII()
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			flushASCII()
			tokens++
		default:
			flushASCII()
			tokens++
		}
	}
	flushASCII()
	return tokens
}

func ResolveAnnouncementContextWindow(config AIConfig) (int, string, bool) {
	model := strings.ToLower(strings.TrimSpace(config.ModelName))
	provider := DetectAIModelProvider(config.BaseUrl, config.ModelName)
	matches := func(prefixes ...string) bool {
		for _, prefix := range prefixes {
			if model == prefix || strings.HasPrefix(model, prefix+"-") || strings.HasPrefix(model, prefix+"/") || strings.HasPrefix(model, prefix+".") {
				return true
			}
		}
		return false
	}
	switch provider {
	case AIProviderOpenAI:
		if matches("gpt-4.1") {
			return 1047576, "内置模型能力数据", false
		}
		if matches("gpt-5") {
			return 400000, "内置模型能力数据", false
		}
		if matches("gpt-4o", "o1", "o3", "o4") {
			return 128000, "内置模型能力数据", false
		}
	case AIProviderAnthropic:
		return 200000, "供应商模型能力数据", false
	case AIProviderGemini:
		if matches("gemini-1.5", "gemini-2", "gemini-3") {
			return 1000000, "内置模型能力数据", false
		}
	case AIProviderDeepSeek:
		if matches("deepseek-chat", "deepseek-reasoner", "deepseek-v3", "deepseek-r1") {
			return 128000, "内置模型能力数据", false
		}
	case AIProviderDashScope:
		if matches("qwen-long") {
			return 1000000, "内置模型能力数据", false
		}
		if matches("qwen-max", "qwen-plus", "qwen-turbo") {
			return 131072, "内置模型能力数据", false
		}
	}
	return announcementDefaultContext, "未知模型默认 200,000 Token", true
}

func BuildAnnouncementPrompts(req models.AnnouncementAIAnalysisRequest, text string) (string, string) {
	systemPrompt := `你是上市公司公告解读助手。当前用户消息中提供的公告元数据和公告原文是唯一事实来源。
禁止使用模型记忆、行情、财务数据库、新闻、网络搜索、工具调用或其他外部资料；禁止编造数字、条款或结论；禁止给出买入、卖出或收益承诺。
必须区分“公告明确事实”“基于原文的审慎推断”和“风险/不确定性”。原文没有说明的信息必须明确写“公告未说明”，不得用外部知识补充。
请用 Markdown 输出并严格包含以下章节：
## 公告核心事实
## 关键数据或条款
## 可能影响（基于原文的审慎推断）
## 风险与不确定性
## 后续需要关注
结尾必须包含：以上解读仅基于本公告原文，由 AI 生成，仅供信息参考，不构成投资建议。`
	userPrompt := fmt.Sprintf(`公告元数据：
- 股票代码：%s
- 股票名称：%s
- 公告标题：%s
- 公告类型：%s
- 公告日期：%s

以下是本次唯一允许使用的公告原文：

--- 公告原文开始 ---
%s
--- 公告原文结束 ---`, req.StockCode, req.StockName, req.Title, req.NoticeType, req.NoticeDate, text)
	return systemPrompt, userPrompt
}

func PreflightAnnouncementAI(config AIConfig, systemPrompt, userPrompt string) models.AnnouncementAIPreflight {
	contextWindow, source, usedDefault := ResolveAnnouncementContextWindow(config)
	estimatedInput := EstimateAnnouncementTokens(systemPrompt) + EstimateAnnouncementTokens(userPrompt) + announcementMessageOverhead
	safeBudget := contextWindow * 85 / 100
	total := estimatedInput + announcementOutputReserve
	return models.AnnouncementAIPreflight{
		EstimatedInputTokens: estimatedInput,
		ReservedOutputTokens: announcementOutputReserve,
		EstimatedTotalTokens: total,
		ContextWindow:        contextWindow,
		SafeBudget:           safeBudget,
		CapacitySource:       source,
		UsedDefaultCapacity:  usedDefault,
		Allowed:              total <= safeBudget,
	}
}

func StreamAnnouncementAIOnce(ctx context.Context, config AIConfig, systemPrompt, userPrompt string, onChunk func(AnnouncementStreamChunk) error) error {
	requestConfig := WithSessionThinkingOverride(config, false)
	requestConfig.ResponseFormat = "text"
	if requestConfig.MaxTokens <= 0 || requestConfig.MaxTokens > announcementOutputReserve {
		requestConfig.MaxTokens = announcementOutputReserve
	}
	if requestConfig.MaxCompletionTokens != nil && *requestConfig.MaxCompletionTokens > announcementOutputReserve {
		bounded := announcementOutputReserve
		requestConfig.MaxCompletionTokens = &bounded
	}
	o := &OpenAi{
		ctx: requestConfigContext(ctx), BaseUrl: requestConfig.BaseUrl, ApiKey: requestConfig.ApiKey,
		Model: requestConfig.ModelName, MaxTokens: requestConfig.MaxTokens, Temperature: requestConfig.Temperature,
		AIConfig: requestConfig, TimeOut: requestConfig.TimeOut,
		HttpProxy: requestConfig.HttpProxy, HttpProxyEnabled: requestConfig.HttpProxyEnabled,
	}
	parameters, err := buildAIRequestParameters(o, false)
	if err != nil {
		return models.NewAnnouncementAIError("provider_failed", "AI 模型参数无效，请检查模型配置", err)
	}
	body := map[string]any{
		"model":  config.ModelName,
		"stream": true,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
	}
	mergeAIRequestParameters(body, parameters)
	timeout := time.Duration(config.TimeOut) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	var client *resty.Client
	if config.HttpProxyEnabled && config.HttpProxy != "" {
		client = createHTTPClientWithProxy(config.HttpProxy, int(timeout/time.Second))
	} else {
		client = CreateHTTPClientWithTimeout(timeout)
	}
	disableAnnouncementRedirects(client)
	baseURL, chatPath := openAIChatEndpoint(config.BaseUrl)
	client.SetBaseURL(baseURL)
	resp, err := client.R().SetContext(ctx).SetDoNotParseResponse(true).
		SetHeader("Authorization", "Bearer "+config.ApiKey).
		SetHeader("Content-Type", "application/json").SetBody(body).Post(chatPath)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return context.Canceled
		}
		return models.NewAnnouncementAIError("provider_failed", "AI 模型请求失败，请检查网络和模型配置", err)
	}
	defer resp.RawBody().Close()
	if resp.StatusCode() != http.StatusOK {
		limited, _ := io.ReadAll(io.LimitReader(resp.RawBody(), 64*1024))
		logger.SugaredLogger.Warnf("announcement AI provider HTTP error art-independent status=%d body_bytes=%d", resp.StatusCode(), len(limited))
		return models.AnnouncementAIErrorf("provider_failed", "AI 模型请求失败（HTTP %d），请检查模型配置", resp.StatusCode())
	}
	scanner := bufio.NewScanner(resp.RawBody())
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	seenDone := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			seenDone = true
			break
		}
		var chunk struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return models.NewAnnouncementAIError("provider_failed", "AI 模型返回了无法解析的流式数据", err)
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content == "" {
				continue
			}
			if onChunk != nil {
				if err := onChunk(AnnouncementStreamChunk{Content: choice.Delta.Content, ModelName: chunk.Model, ResponseID: chunk.ID}); err != nil {
					return err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return context.Canceled
		}
		return models.NewAnnouncementAIError("provider_failed", "接收 AI 模型流式响应失败，请重试", err)
	}
	if !seenDone {
		return models.NewAnnouncementAIError("provider_failed", "AI 模型流式响应意外中断，未保存本次结果", nil)
	}
	return nil
}

func disableAnnouncementRedirects(client *resty.Client) {
	if client == nil || client.GetClient() == nil {
		return
	}
	client.GetClient().CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
}

func requestConfigContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
