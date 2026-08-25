//go:build web

package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type webTestUploadFile struct {
	fieldName string
	fileName  string
	content   []byte
}

func TestEmbeddedFrontendIncludesLeadingUnderscoreAssets(t *testing.T) {
	matches, err := fs.Glob(assets, "frontend/dist/assets/_commonjsHelpers-*.js")
	if err != nil {
		t.Fatalf("fs.Glob() error = %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("embedded frontend is missing Vite's leading-underscore commonjs helper chunk")
	}
}

func TestServeSPACacheAndMissingAssetContracts(t *testing.T) {
	api := &webAPI{staticFS: fstest.MapFS{
		"index.html":        &fstest.MapFile{Data: []byte("<html>app</html>")},
		"assets/app-123.js": &fstest.MapFile{Data: []byte("export default 1")},
	}}

	t.Run("index is never stored", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		api.serveSPA(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-cache, no-store, must-revalidate" {
			t.Fatalf("Cache-Control = %q", got)
		}
	})

	t.Run("hashed asset is immutable", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		api.serveSPA(recorder, httptest.NewRequest(http.MethodGet, "/assets/app-123.js", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Fatalf("Cache-Control = %q", got)
		}
	})

	t.Run("missing asset does not fall back to index", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		api.serveSPA(recorder, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
		if strings.Contains(recorder.Body.String(), "<html>app</html>") {
			t.Fatal("missing asset returned SPA index")
		}
	})
}

func newWebMultipartRequest(t *testing.T, path string, values map[string]string, files []webTestUploadFile) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range values {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("WriteField(%s) error = %v", name, err)
		}
	}
	for _, upload := range files {
		part, err := writer.CreateFormFile(upload.fieldName, upload.fileName)
		if err != nil {
			t.Fatalf("CreateFormFile(%s) error = %v", upload.fileName, err)
		}
		if _, err := part.Write(upload.content); err != nil {
			t.Fatalf("Write(%s) error = %v", upload.fileName, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart writer close error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func (*App) PanicForWebTest() {
	panic("test panic")
}

func TestLoadWebBindingMethods(t *testing.T) {
	methods, err := loadWebBindingMethods()
	if err != nil {
		t.Fatalf("loadWebBindingMethods() error = %v", err)
	}
	for _, name := range []string{
		"GetConfig", "GetStockList", "SummaryStockNews",
		"GetAnnouncementAIAnalysis", "StartAnnouncementAIAnalysis", "AbortAnnouncementAIAnalysis",
		"ChatWithAgentKBQA", "CreateKnowledgeBase", "GetUserProfile",
		"SubmitAgentFeedback", "RunRecommendBacktest",
		"GetMottos", "CreateMotto", "UpdateMotto", "DeleteMotto", "PolishMotto",
		"TestAIConfig",
		"PackSkillToBase64", "ImportSkillFromBase64",
	} {
		if _, ok := methods[name]; !ok {
			t.Fatalf("binding method %s not found", name)
		}
	}
	for name := range webDesktopOnlyMethods {
		if _, ok := methods[name]; ok {
			t.Fatalf("desktop-only binding method %s must be hidden from Web RPC", name)
		}
	}
	for _, name := range []string{
		"ImportTradingRecordsFromExcel", "PickKBFilePath", "PickKBFilePaths",
		"UploadKBFile", "UploadKBFiles",
	} {
		if _, ok := methods[name]; ok {
			t.Fatalf("server-file method %s must be hidden from Web RPC", name)
		}
	}
}

func TestWebCronTaskExecutedEventSerialization(t *testing.T) {
	hub := newWebEventHub()
	client := hub.subscribe()
	defer hub.unsubscribe(client)

	hub.Emit(models.CronTaskExecutedEventName, models.CronTaskExecutedEvent{
		TaskID:      42,
		Success:     true,
		CompletedAt: time.Date(2026, time.August, 16, 18, 30, 0, 0, time.FixedZone("CST", 8*60*60)),
	})

	select {
	case payload := <-client:
		var event webEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		encoded, err := json.Marshal(event.Data[0])
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		if event.Name != models.CronTaskExecutedEventName || !strings.Contains(string(encoded), `"taskId":42`) || !strings.Contains(string(encoded), `"success":true`) {
			t.Fatalf("serialized cron event = %s, name=%s", encoded, event.Name)
		}
	default:
		t.Fatal("cron task event not broadcast")
	}
}

func TestWebAnnouncementEventSerialization(t *testing.T) {
	hub := newWebEventHub()
	client := hub.subscribe()
	defer hub.unsubscribe(client)

	hub.Emit(models.AnnouncementAIEventName, models.AnnouncementAIAnalysisEvent{
		RequestID: "req-1",
		ArtCode:   "AN202608130001",
		Phase:     models.AnnouncementAIPhasePreflight,
		Preflight: &models.AnnouncementAIPreflight{
			EstimatedTotalTokens: 12000,
			ContextWindow:        200000,
			CapacitySource:       "未知模型默认 200,000 Token",
			UsedDefaultCapacity:  true,
			Allowed:              true,
		},
	})

	select {
	case payload := <-client:
		var event webEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if event.Name != models.AnnouncementAIEventName || len(event.Data) != 1 {
			t.Fatalf("event = %+v", event)
		}
		encoded, err := json.Marshal(event.Data[0])
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		if !strings.Contains(string(encoded), `"requestId":"req-1"`) || !strings.Contains(string(encoded), `"usedDefaultCapacity":true`) {
			t.Fatalf("serialized payload = %s", encoded)
		}
	default:
		t.Fatal("announcement event not broadcast")
	}
}

func TestWebBindingMethodsAreImplemented(t *testing.T) {
	methods, err := loadWebBindingMethods()
	if err != nil {
		t.Fatalf("loadWebBindingMethods() error = %v", err)
	}
	appType := reflect.TypeOf(&App{})
	for name := range methods {
		if _, ok := appType.MethodByName(name); !ok {
			t.Errorf("binding method %s is not implemented by App", name)
		}
	}
}

func TestCallWebMethod(t *testing.T) {
	app := &App{}

	result, err := callWebMethod(app, "GetTimezone", nil)
	if err != nil {
		t.Fatalf("callWebMethod() error = %v", err)
	}
	timezone, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if timezone["location"] != "Asia/Shanghai" {
		t.Fatalf("location = %v", timezone["location"])
	}
}

func TestCallWebMottoMethods(t *testing.T) {
	previous := db.Dao
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := database.AutoMigrate(&models.Motto{}); err != nil {
		t.Fatalf("migrate motto table: %v", err)
	}
	db.Dao = database
	t.Cleanup(func() { db.Dao = previous })

	app := &App{}
	createdValue, err := callWebMethod(app, "CreateMotto", []json.RawMessage{json.RawMessage(`"保持耐心"`)})
	if err != nil {
		t.Fatalf("CreateMotto reflection call error = %v", err)
	}
	created, ok := createdValue.(*models.Motto)
	if !ok || created.ID == 0 {
		t.Fatalf("CreateMotto result = %#v", createdValue)
	}

	updatedValue, err := callWebMethod(app, "UpdateMotto", []json.RawMessage{
		json.RawMessage(strconv.FormatUint(uint64(created.ID), 10)),
		json.RawMessage(`"长期主义"`),
	})
	if err != nil {
		t.Fatalf("UpdateMotto reflection call error = %v", err)
	}
	updated, ok := updatedValue.(*models.Motto)
	if !ok || updated.Content != "长期主义" {
		t.Fatalf("UpdateMotto result = %#v", updatedValue)
	}

	listValue, err := callWebMethod(app, "GetMottos", nil)
	if err != nil {
		t.Fatalf("GetMottos reflection call error = %v", err)
	}
	list, ok := listValue.([]models.Motto)
	if !ok || len(list) != 1 || list[0].Content != "长期主义" {
		t.Fatalf("GetMottos result = %#v", listValue)
	}

	if _, err := callWebMethod(app, "DeleteMotto", []json.RawMessage{json.RawMessage(strconv.FormatUint(uint64(created.ID), 10))}); err != nil {
		t.Fatalf("DeleteMotto reflection call error = %v", err)
	}
}

func TestCallWebAIConfigTestReturnsSafeValidationResult(t *testing.T) {
	configJSON, err := json.Marshal(&data.AIConfig{Name: "draft", ModelType: "chat"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	value, err := callWebMethod(&App{}, "TestAIConfig", []json.RawMessage{configJSON})
	if err != nil {
		t.Fatalf("TestAIConfig reflection call error = %v", err)
	}
	result, ok := value.(*models.AIConfigTestResult)
	if !ok || result.Success || !strings.Contains(result.Message, "配置校验失败") {
		t.Fatalf("TestAIConfig result = %#v", value)
	}
}

func TestCallWebMethodRejectsInvalidArguments(t *testing.T) {
	app := &App{}

	if _, err := callWebMethod(app, "GetTimezone", []json.RawMessage{json.RawMessage(`1`)}); err == nil {
		t.Fatal("expected argument count error")
	}
	if _, err := callWebMethod(app, "MissingMethod", nil); err == nil {
		t.Fatal("expected missing method error")
	}
}

func TestCallWebMethodRecoversPanic(t *testing.T) {
	if _, err := callWebMethod(&App{}, "PanicForWebTest", nil); err == nil || !strings.Contains(err.Error(), "调用 PanicForWebTest 失败") {
		t.Fatalf("expected recovered panic error, got %v", err)
	}
}

func TestWebRPCRejectsUnknownMethod(t *testing.T) {
	app := &App{}
	api := &webAPI{
		app:            app,
		hub:            newWebEventHub(),
		allowedMethods: map[string]struct{}{"GetTimezone": {}},
	}
	body := bytes.NewBufferString(`{"method":"DeleteEverything","args":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rpc", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	api.rpc(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestWebRPCRoutes(t *testing.T) {
	server, err := newWebHTTPServer("127.0.0.1:0", &App{}, newWebEventHub())
	if err != nil {
		t.Fatalf("newWebHTTPServer() error = %v", err)
	}

	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "method in path", path: "/api/rpc/GetTimezone", body: `{"args":[]}`},
		{name: "legacy method in body", path: "/api/rpc", body: `{"method":"GetTimezone","args":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			server.Handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestWebSkillPlazaRPCMethods(t *testing.T) {
	t.Chdir(t.TempDir())
	server, err := newWebHTTPServer("127.0.0.1:0", &App{}, newWebEventHub())
	if err != nil {
		t.Fatalf("newWebHTTPServer() error = %v", err)
	}

	tests := []struct {
		method string
		body   string
		want   string
	}{
		{method: "PackSkillToBase64", body: `{"args":["missing"]}`, want: "技能目录不存在"},
		{method: "ImportSkillFromBase64", body: `{"args":["not-base64"]}`, want: "技能包解码失败"},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/rpc/"+tt.method, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			server.Handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), tt.want) {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestWebRPCRejectsConflictingPathAndBodyMethods(t *testing.T) {
	api := &webAPI{
		app:            &App{},
		hub:            newWebEventHub(),
		allowedMethods: map[string]struct{}{"GetTimezone": {}, "GetConfig": {}},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/rpc/GetTimezone",
		strings.NewReader(`{"method":"GetConfig","args":[]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	api.rpc(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestWebRPCRejectsDesktopOnlyMethod(t *testing.T) {
	allowedMethods, err := loadWebBindingMethods()
	if err != nil {
		t.Fatalf("loadWebBindingMethods() error = %v", err)
	}
	api := &webAPI{
		app:            &App{webMode: true},
		hub:            newWebEventHub(),
		allowedMethods: allowedMethods,
	}
	body := bytes.NewBufferString(`{"method":"QuitApp","args":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rpc", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	api.rpc(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestWebRPCRejectsNonJSONBody(t *testing.T) {
	api := &webAPI{
		app:            &App{},
		hub:            newWebEventHub(),
		allowedMethods: map[string]struct{}{"GetTimezone": {}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(`{"method":"GetTimezone","args":[]}`))
	req.Header.Set("Content-Type", "text/plain")
	recorder := httptest.NewRecorder()

	api.rpc(recorder, req)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestWebEventHubBroadcast(t *testing.T) {
	hub := newWebEventHub()
	client := hub.subscribe()
	defer hub.unsubscribe(client)

	hub.Emit("stock_price", map[string]any{"code": "sh000001"})
	select {
	case payload := <-client:
		var event webEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if event.Name != "stock_price" || len(event.Data) != 1 {
			t.Fatalf("event = %+v", event)
		}
	default:
		t.Fatal("event not broadcast")
	}
}

func TestWebEventStreamStopsWhenHubCloses(t *testing.T) {
	hub := newWebEventHub()
	api := &webAPI{hub: hub}
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		api.events(recorder, req)
		close(done)
	}()

	hub.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event stream did not stop after hub close")
	}
}

func TestWebSecurityHeadersRejectCrossOriginRequest(t *testing.T) {
	handler := webSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18888/api/rpc", nil)
	req.Header.Set("Origin", "https://example.com")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", recorder.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18888/api/rpc", nil)
	req.Header.Set("Origin", "http://127.0.0.1:18888")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("same-origin status = %d", recorder.Code)
	}
}

func TestWebCheckUpdateDoesNotRunDesktopUpdater(t *testing.T) {
	hub := newWebEventHub()
	client := hub.subscribe()
	defer hub.unsubscribe(client)
	app := &App{webMode: true}
	app.setEventEmitter(hub)

	app.CheckUpdate(1)

	select {
	case payload := <-client:
		var event webEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if event.Name != "newsPush" {
			t.Fatalf("event.Name = %s", event.Name)
		}
	default:
		t.Fatal("expected Web update guidance event")
	}
}

func TestWebImportSkill(t *testing.T) {
	t.Chdir(t.TempDir())

	var packageBody bytes.Buffer
	zipWriter := zip.NewWriter(&packageBody)
	skillFile, err := zipWriter.Create("demo/SKILL.md")
	if err != nil {
		t.Fatalf("zipWriter.Create() error = %v", err)
	}
	if _, err := skillFile.Write([]byte("---\nname: demo\ndescription: demo skill\n---\n")); err != nil {
		t.Fatalf("skillFile.Write() error = %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("zipWriter.Close() error = %v", err)
	}

	var requestBody bytes.Buffer
	multipartWriter := multipart.NewWriter(&requestBody)
	formFile, err := multipartWriter.CreateFormFile("file", "demo.zip")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := formFile.Write(packageBody.Bytes()); err != nil {
		t.Fatalf("formFile.Write() error = %v", err)
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatalf("multipartWriter.Close() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/skills/import", &requestBody)
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	recorder := httptest.NewRecorder()
	api := &webAPI{app: &App{}}
	api.importSkill(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join("skills", "demo", "SKILL.md")); err != nil {
		t.Fatalf("imported SKILL.md not found: %v", err)
	}
}

func TestWebImportTradingRecordFile(t *testing.T) {
	var uploadedPath string
	api := &webAPI{
		importTradingRecords: func(filePath string) (*data.TradingRecordImportResult, error) {
			uploadedPath = filePath
			content, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("os.ReadFile() error = %v", err)
			}
			if string(content) != "成交日期\t证券代码\n" {
				t.Fatalf("uploaded content = %q", content)
			}
			return &data.TradingRecordImportResult{Total: 1, Imported: 1, Message: "导入完成"}, nil
		},
	}
	req := newWebMultipartRequest(t, "/api/trading-records/import", nil, []webTestUploadFile{{
		fieldName: "file",
		fileName:  "records.csv",
		content:   []byte("成交日期\t证券代码\n"),
	}})
	recorder := httptest.NewRecorder()

	api.importTradingRecordFile(recorder, req)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"imported":1`) {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if uploadedPath == "" {
		t.Fatal("import callback did not receive uploaded path")
	}
	if _, err := os.Stat(uploadedPath); !os.IsNotExist(err) {
		t.Fatalf("trading upload temp file still exists: %v", err)
	}
}

func TestWebImportKBFile(t *testing.T) {
	var uploadedPath string
	api := &webAPI{
		uploadKBFile: func(kbName, filePath string) ([]string, error) {
			if kbName != "研报" {
				t.Fatalf("kbName = %q", kbName)
			}
			uploadedPath = filePath
			if filepath.Base(filePath) != "report.md" {
				t.Fatalf("uploaded base name = %q", filepath.Base(filePath))
			}
			return []string{"doc-1"}, nil
		},
	}
	req := newWebMultipartRequest(t, "/api/knowledge-base/file/import", map[string]string{"kbName": "研报"}, []webTestUploadFile{{
		fieldName: "file",
		fileName:  "report.md",
		content:   []byte("# 研报"),
	}})
	recorder := httptest.NewRecorder()

	api.importKBFile(recorder, req)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"doc-1"`) {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(uploadedPath); !os.IsNotExist(err) {
		t.Fatalf("knowledge-base upload temp file still exists: %v", err)
	}
}

func TestWebImportKBFilesTransfersCleanupOwnership(t *testing.T) {
	var uploadedPaths []string
	var cleanup func()
	api := &webAPI{
		uploadKBFiles: func(kbName string, filePaths []string, cleanupFn func()) error {
			if kbName != "研报" {
				t.Fatalf("kbName = %q", kbName)
			}
			uploadedPaths = append([]string(nil), filePaths...)
			cleanup = cleanupFn
			return nil
		},
	}
	req := newWebMultipartRequest(t, "/api/knowledge-base/files/import", map[string]string{"kbName": "研报"}, []webTestUploadFile{
		{fieldName: "files", fileName: "a.md", content: []byte("A")},
		{fieldName: "files", fileName: "b.txt", content: []byte("B")},
	})
	recorder := httptest.NewRecorder()

	api.importKBFiles(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(uploadedPaths) != 2 || cleanup == nil {
		t.Fatalf("paths = %v, cleanup nil = %v", uploadedPaths, cleanup == nil)
	}
	for _, filePath := range uploadedPaths {
		if _, err := os.Stat(filePath); err != nil {
			t.Fatalf("background import path unavailable before cleanup: %v", err)
		}
	}
	tempDir := filepath.Dir(filepath.Dir(uploadedPaths[0]))
	cleanup()
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Fatalf("batch upload temp directory still exists: %v", err)
	}
}

func TestWebImportKBFilesCleansUpWhenStartFails(t *testing.T) {
	var tempDir string
	api := &webAPI{
		uploadKBFiles: func(_ string, filePaths []string, _ func()) error {
			tempDir = filepath.Dir(filepath.Dir(filePaths[0]))
			return errors.New("知识库正在向量化中")
		},
	}
	req := newWebMultipartRequest(t, "/api/knowledge-base/files/import", map[string]string{"kbName": "研报"}, []webTestUploadFile{{
		fieldName: "files",
		fileName:  "report.md",
		content:   []byte("# 研报"),
	}})
	recorder := httptest.NewRecorder()

	api.importKBFiles(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if tempDir == "" {
		t.Fatal("upload callback did not receive temporary file")
	}
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Fatalf("batch upload temp directory still exists after start failure: %v", err)
	}
}

func TestWebUploadValidation(t *testing.T) {
	t.Run("content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/trading-records/import", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		(&webAPI{}).importTradingRecordFile(recorder, req)
		if recorder.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("extension", func(t *testing.T) {
		req := newWebMultipartRequest(t, "/api/knowledge-base/file/import", map[string]string{"kbName": "研报"}, []webTestUploadFile{{
			fieldName: "file",
			fileName:  "report.pdf",
			content:   []byte("pdf"),
		}})
		recorder := httptest.NewRecorder()
		(&webAPI{}).importKBFile(recorder, req)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), ".pdf") {
			t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("file size", func(t *testing.T) {
		req := newWebMultipartRequest(t, "/upload", nil, []webTestUploadFile{{
			fieldName: "file",
			fileName:  "report.md",
			content:   []byte("1234"),
		}})
		recorder := httptest.NewRecorder()
		_, failure := receiveWebUploadedFiles(recorder, req, webUploadSpec{
			fieldName:         "file",
			description:       "测试文件",
			tempPrefix:        "go-stock-upload-test-*",
			maxFiles:          1,
			maxFileSize:       3,
			maxTotalFileSize:  4,
			allowedExtensions: webKBFileExtensions,
		})
		if failure == nil || failure.status != http.StatusRequestEntityTooLarge {
			t.Fatalf("failure = %+v", failure)
		}
	})

	t.Run("file count", func(t *testing.T) {
		req := newWebMultipartRequest(t, "/upload", nil, []webTestUploadFile{
			{fieldName: "files", fileName: "a.md", content: []byte("A")},
			{fieldName: "files", fileName: "b.md", content: []byte("B")},
		})
		recorder := httptest.NewRecorder()
		_, failure := receiveWebUploadedFiles(recorder, req, webUploadSpec{
			fieldName:         "files",
			description:       "测试文件",
			tempPrefix:        "go-stock-upload-test-*",
			maxFiles:          1,
			maxFileSize:       10,
			maxTotalFileSize:  20,
			allowedExtensions: webKBFileExtensions,
		})
		if failure == nil || failure.status != http.StatusBadRequest {
			t.Fatalf("failure = %+v", failure)
		}
	})

	t.Run("total file size", func(t *testing.T) {
		req := newWebMultipartRequest(t, "/upload", nil, []webTestUploadFile{
			{fieldName: "files", fileName: "a.md", content: []byte("123")},
			{fieldName: "files", fileName: "b.md", content: []byte("456")},
		})
		recorder := httptest.NewRecorder()
		_, failure := receiveWebUploadedFiles(recorder, req, webUploadSpec{
			fieldName:         "files",
			description:       "测试文件",
			tempPrefix:        "go-stock-upload-test-*",
			maxFiles:          2,
			maxFileSize:       3,
			maxTotalFileSize:  5,
			allowedExtensions: webKBFileExtensions,
		})
		if failure == nil || failure.status != http.StatusRequestEntityTooLarge {
			t.Fatalf("failure = %+v", failure)
		}
	})
}

func TestWebUploadRouteRejectsCrossOriginRequest(t *testing.T) {
	server, err := newWebHTTPServer("127.0.0.1:0", &App{}, newWebEventHub())
	if err != nil {
		t.Fatalf("newWebHTTPServer() error = %v", err)
	}
	req := newWebMultipartRequest(t, "http://127.0.0.1:18888/api/trading-records/import", nil, []webTestUploadFile{{
		fieldName: "file",
		fileName:  "records.csv",
		content:   []byte("成交日期\t证券代码\n"),
	}})
	req.Header.Set("Origin", "https://example.com")
	recorder := httptest.NewRecorder()

	server.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestImportSkillPackageKeepsExistingSkillOnExtractionFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	existingDir := filepath.Join("skills", "demo")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	existingPath := filepath.Join(existingDir, "existing.txt")
	if err := os.WriteFile(existingPath, []byte("keep"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	packagePath := filepath.Join(t.TempDir(), "demo.zip")
	packageFile, err := os.Create(packagePath)
	if err != nil {
		t.Fatalf("os.Create() error = %v", err)
	}
	zipWriter := zip.NewWriter(packageFile)
	for name, content := range map[string]string{
		"demo/SKILL.md":      "---\nname: demo\ndescription: demo skill\n---\n",
		"demo/conflict":      "file",
		"demo/conflict/file": "cannot create below a file",
	} {
		entry, createErr := zipWriter.Create(name)
		if createErr != nil {
			t.Fatalf("zipWriter.Create() error = %v", createErr)
		}
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			t.Fatalf("entry.Write() error = %v", writeErr)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("zipWriter.Close() error = %v", err)
	}
	if err := packageFile.Close(); err != nil {
		t.Fatalf("packageFile.Close() error = %v", err)
	}

	result := (&App{}).importSkillPackage(packagePath, "demo.zip")
	if strings.Contains(result, "导入成功") {
		t.Fatalf("expected extraction failure, result = %s", result)
	}
	content, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("existing skill was removed: %v", err)
	}
	if string(content) != "keep" {
		t.Fatalf("existing skill content = %q", content)
	}
}

func TestImportSkillFromBase64KeepsExistingSkillOnExtractionFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	existingDir := filepath.Join("skills", "demo")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	existingPath := filepath.Join(existingDir, "existing.txt")
	if err := os.WriteFile(existingPath, []byte("keep"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	var packageBody bytes.Buffer
	zipWriter := zip.NewWriter(&packageBody)
	for name, content := range map[string]string{
		"demo/SKILL.md":      "---\nname: demo\ndescription: demo skill\n---\n",
		"demo/conflict":      "file",
		"demo/conflict/file": "cannot create below a file",
	} {
		entry, err := zipWriter.Create(name)
		if err != nil {
			t.Fatalf("zipWriter.Create() error = %v", err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("entry.Write() error = %v", err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("zipWriter.Close() error = %v", err)
	}

	result := (&App{}).ImportSkillFromBase64(base64.StdEncoding.EncodeToString(packageBody.Bytes()))
	if strings.Contains(result, "导入成功") {
		t.Fatalf("expected extraction failure, result = %s", result)
	}
	content, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("existing skill was removed: %v", err)
	}
	if string(content) != "keep" {
		t.Fatalf("existing skill content = %q", content)
	}
}
