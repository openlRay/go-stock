package data

import (
	"context"
	"encoding/json"
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func useAnnouncementTestDB(t *testing.T) {
	t.Helper()
	previous := db.Dao
	testDB, err := gorm.Open(sqlite.New(sqlite.Config{DriverName: "sqlite", DSN: "file:" + t.Name() + "?mode=memory&cache=shared"}), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&models.AnnouncementAIAnalysis{}, &AIConfig{}))
	db.Dao = testDB
	t.Cleanup(func() { db.Dao = previous })
}

func validAnnouncementRequest() models.AnnouncementAIAnalysisRequest {
	return models.AnnouncementAIAnalysisRequest{
		ArtCode: "AN202608130001", StockCode: "600000", StockName: "测试公司",
		Title: "关于测试事项的公告", NoticeType: "公司公告", NoticeDate: "2026-08-13", AIConfigID: 1,
	}
}

func validAnnouncementConfig(baseURL string) AIConfig {
	return AIConfig{
		Name: "test", BaseUrl: baseURL, ApiKey: "secret", ModelName: "custom-model",
		MaxTokens: 4096, TimeOut: 5, ResponseFormat: "text", ReasoningMode: ReasoningModeOff,
	}
}

func TestAnnouncementAnalysisPersistenceReplacesAtomically(t *testing.T) {
	useAnnouncementTestDB(t)
	first := &models.AnnouncementAIAnalysis{
		ArtCode: "AN202608130001", StockCode: "600000", StockName: "测试公司", Title: "公告",
		NoticeType: "公司公告", NoticeDate: "2026-08-13", PDFURL: "https://pdf.dfcfw.com/pdf/H2_AN202608130001_1.pdf",
		AIConfigID: 1, ModelName: "model-a", PromptVersion: AnnouncementAIPromptVersion,
		InstructionID: AnnouncementAIInstructionID, Content: "旧结果", GeneratedAt: time.Now().Add(-time.Hour),
	}
	require.NoError(t, UpsertAnnouncementAIAnalysis(first))
	second := *first
	second.ID = 0
	second.ModelName = "model-b"
	second.Content = "新结果"
	second.GeneratedAt = time.Now()
	require.NoError(t, UpsertAnnouncementAIAnalysis(&second))

	var count int64
	require.NoError(t, db.Dao.Model(&models.AnnouncementAIAnalysis{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	stored, err := GetAnnouncementAIAnalysis(first.ArtCode)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "新结果", stored.Content)
	assert.Equal(t, "model-b", stored.ModelName)

	invalid := second
	invalid.Content = ""
	require.Error(t, UpsertAnnouncementAIAnalysis(&invalid))
	stored, err = GetAnnouncementAIAnalysis(first.ArtCode)
	require.NoError(t, err)
	assert.Equal(t, "新结果", stored.Content)
}

func TestAnnouncementValidationAndCanonicalURL(t *testing.T) {
	request, err := ValidateAnnouncementAIRequest(validAnnouncementRequest())
	require.NoError(t, err)
	assert.Equal(t, "AN202608130001", request.ArtCode)

	url, err := AnnouncementPDFURL(request.ArtCode)
	require.NoError(t, err)
	assert.Equal(t, "https://pdf.dfcfw.com/pdf/H2_AN202608130001_1.pdf", url)

	bad := validAnnouncementRequest()
	bad.ArtCode = "../../etc/passwd"
	_, err = ValidateAnnouncementAIRequest(bad)
	require.Error(t, err)
	var safeErr *models.AnnouncementAIError
	require.ErrorAs(t, err, &safeErr)
	assert.Equal(t, "invalid_metadata", safeErr.Code)
}

func TestDownloadAnnouncementPDFValidation(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		status      int
		wantCode    string
	}{
		{name: "pdf", contentType: "application/pdf", body: "%PDF-1.4\nbody", status: http.StatusOK},
		{name: "signature wins for octet stream", contentType: "application/octet-stream", body: "%PDF-1.4\nbody", status: http.StatusOK},
		{name: "html", contentType: "text/html", body: "<html>no</html>", status: http.StatusOK, wantCode: "not_pdf"},
		{name: "unrecognized script", contentType: "application/pdf", body: `<script>document["cookie"]="other=1";</script>`, status: http.StatusOK, wantCode: "not_pdf"},
		{name: "bad signature", contentType: "application/pdf", body: "not pdf", status: http.StatusOK, wantCode: "not_pdf"},
		{name: "upstream error", contentType: "text/plain", body: "error", status: http.StatusBadGateway, wantCode: "download_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			body, err := downloadAnnouncementPDF(context.Background(), CreateHTTPClientWithTimeout(time.Second), server.URL)
			if tt.wantCode == "" {
				require.NoError(t, err)
				assert.Equal(t, tt.body, string(body))
				return
			}
			require.Error(t, err)
			var safeErr *models.AnnouncementAIError
			require.ErrorAs(t, err, &safeErr)
			assert.Equal(t, tt.wantCode, safeErr.Code)
		})
	}
}

func TestDownloadAnnouncementPDFCompletesBotChallenge(t *testing.T) {
	const challenge = `<script>
var statusName = "__tst_status";
var sessionName = "EO_Bot_Ssid";
document["cookie"] = statusName + "=2465593332#;";
document["cookie"] = sessionName + "=8454144;";
</script>`
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		statusCookie, statusErr := r.Cookie("__tst_status")
		sessionCookie, sessionErr := r.Cookie("EO_Bot_Ssid")
		w.Header().Set("Content-Type", "application/pdf")
		if statusErr != nil || sessionErr != nil {
			_, _ = w.Write([]byte(challenge))
			return
		}
		assert.Equal(t, "2465593332#", statusCookie.Value)
		assert.Equal(t, "8454144", sessionCookie.Value)
		_, _ = w.Write([]byte("%PDF-1.7\nbody"))
	}))
	defer server.Close()

	body, err := downloadAnnouncementPDF(context.Background(), CreateHTTPClientWithTimeout(time.Second), server.URL)
	require.NoError(t, err)
	assert.Equal(t, "%PDF-1.7\nbody", string(body))
	assert.Equal(t, int32(2), requests.Load())
}

func TestAnnouncementBotChallengeExecutionIsBounded(t *testing.T) {
	challenge := []byte(`<script>
var statusName = "__tst_status";
var sessionName = "EO_Bot_Ssid";
var cookie = document["cookie"];
while (true) {}
</script>`)
	startedAt := time.Now()

	_, err := solveAnnouncementBotChallenge(context.Background(), challenge)

	require.Error(t, err)
	assert.Less(t, time.Since(startedAt), 2*time.Second)
}

func TestDownloadAnnouncementPDFDoesNotFollowRedirect(t *testing.T) {
	var redirectedRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectedRequests.Add(1)
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4\nbody"))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	client := CreateHTTPClientWithTimeout(time.Second)
	disableAnnouncementRedirects(client)

	_, err := downloadAnnouncementPDF(context.Background(), client, redirect.URL)
	require.Error(t, err)
	assert.Equal(t, int32(0), redirectedRequests.Load())
}

func TestNormalizeAnnouncementTextAndInvalidExtraction(t *testing.T) {
	input := " 第一行\r\n\r\n  第二行\t 数据  \r\n\r\n\r\n第三行\x00"
	assert.Equal(t, "第一行\n\n第二行 数据\n\n第三行", NormalizeAnnouncementText(input))

	_, err := ExtractAnnouncementPDFText([]byte("not a pdf"))
	require.Error(t, err)
	var safeErr *models.AnnouncementAIError
	require.ErrorAs(t, err, &safeErr)
	assert.Equal(t, "not_pdf", safeErr.Code)
}

func TestAnnouncementExtractedTextIsNeverTruncated(t *testing.T) {
	require.NoError(t, validateAnnouncementExtractedTextSize(announcementPDFMaxBytes))
	err := validateAnnouncementExtractedTextSize(announcementPDFMaxBytes + 1)
	require.Error(t, err)
	var safeErr *models.AnnouncementAIError
	require.ErrorAs(t, err, &safeErr)
	assert.Equal(t, "pdf_text_too_large", safeErr.Code)
}

func TestEstimateAnnouncementTokens(t *testing.T) {
	tests := []struct {
		text string
		min  int
		max  int
	}{
		{"", 0, 0},
		{"abcd", 1, 1},
		{"abcdefgh", 2, 2},
		{"公告核心事实", 6, 6},
		{"公司 revenue 12345 增长 10%", 9, 14},
	}
	for _, tt := range tests {
		got := EstimateAnnouncementTokens(tt.text)
		assert.GreaterOrEqual(t, got, tt.min, tt.text)
		assert.LessOrEqual(t, got, tt.max, tt.text)
	}
}

func TestAnnouncementContextWindowAndPreflight(t *testing.T) {
	official := validAnnouncementConfig("https://api.openai.com/v1")
	official.ModelName = "gpt-5.2"
	window, _, fallback := ResolveAnnouncementContextWindow(official)
	assert.Equal(t, 400000, window)
	assert.False(t, fallback)

	spoofed := validAnnouncementConfig("https://custom.example/v1")
	spoofed.ModelName = "gpt-5-clone"
	window, source, fallback := ResolveAnnouncementContextWindow(spoofed)
	assert.Equal(t, announcementDefaultContext, window)
	assert.Contains(t, source, "默认")
	assert.True(t, fallback)

	preflight := PreflightAnnouncementAI(spoofed, "system", "short")
	assert.True(t, preflight.Allowed)
	assert.Equal(t, announcementDefaultContext*85/100, preflight.SafeBudget)

	tooLarge := strings.Repeat("公告", announcementDefaultContext)
	preflight = PreflightAnnouncementAI(spoofed, "system", tooLarge)
	assert.False(t, preflight.Allowed)
	assert.Greater(t, preflight.EstimatedTotalTokens, preflight.SafeBudget)

	exactUserTokens := preflight.SafeBudget - announcementOutputReserve - announcementMessageOverhead - EstimateAnnouncementTokens("system")
	exact := strings.Repeat("公", exactUserTokens)
	boundary := PreflightAnnouncementAI(spoofed, "system", exact)
	assert.True(t, boundary.Allowed)
	assert.Equal(t, boundary.SafeBudget, boundary.EstimatedTotalTokens)
}

func TestBuildAnnouncementPromptsAreSourceOnly(t *testing.T) {
	systemPrompt, userPrompt := BuildAnnouncementPrompts(validAnnouncementRequest(), "公告正文唯一内容")
	assert.Contains(t, systemPrompt, "唯一")
	assert.Contains(t, systemPrompt, "禁止使用模型记忆")
	assert.Contains(t, systemPrompt, "## 公告核心事实")
	assert.Contains(t, userPrompt, "公告正文唯一内容")
	assert.Contains(t, userPrompt, "600000")
}

func TestStreamAnnouncementAIOnceUsesOneToolFreeRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Nil(t, body["tools"])
		assert.Nil(t, body["response_format"])
		encoded, _ := json.Marshal(body["messages"])
		assert.Contains(t, string(encoded), "公告正文唯一内容")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chat-1\",\"model\":\"custom-model\",\"choices\":[{\"delta\":{\"reasoning_content\":\"hidden\",\"content\":\"结果\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	config := validAnnouncementConfig(server.URL + "/v1")
	var content strings.Builder
	err := StreamAnnouncementAIOnce(context.Background(), config, "system", "公告正文唯一内容", func(chunk AnnouncementStreamChunk) error {
		content.WriteString(chunk.Content)
		assert.Equal(t, "chat-1", chunk.ResponseID)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), requests.Load())
	assert.Equal(t, "结果", content.String())
}

func TestStreamAnnouncementAIOnceDoesNotRetryFailures(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{name: "provider 400", handler: func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unsupported", http.StatusBadRequest) }},
		{name: "malformed stream", handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, "data: {bad}\n\n") }},
		{name: "unexpected end", handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, "data: {\"choices\":[]}\n\n") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				tt.handler(w, r)
			}))
			defer server.Close()
			err := StreamAnnouncementAIOnce(context.Background(), validAnnouncementConfig(server.URL+"/v1"), "system", "user", nil)
			require.Error(t, err)
			assert.Equal(t, int32(1), requests.Load())
		})
	}
}

func TestStreamAnnouncementAIOnceDoesNotFollowRedirect(t *testing.T) {
	var redirectedRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectedRequests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	err := StreamAnnouncementAIOnce(context.Background(), validAnnouncementConfig(redirect.URL+"/v1"), "system", "user", nil)
	require.Error(t, err)
	assert.Equal(t, int32(0), redirectedRequests.Load())
}
