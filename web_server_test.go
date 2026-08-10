//go:build web

package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func (*App) PanicForWebTest() {
	panic("test panic")
}

func TestLoadWebBindingMethods(t *testing.T) {
	methods, err := loadWebBindingMethods()
	if err != nil {
		t.Fatalf("loadWebBindingMethods() error = %v", err)
	}
	for _, name := range []string{"GetConfig", "GetStockList", "SummaryStockNews"} {
		if _, ok := methods[name]; !ok {
			t.Fatalf("binding method %s not found", name)
		}
	}
	for name := range webDesktopOnlyMethods {
		if _, ok := methods[name]; ok {
			t.Fatalf("desktop-only binding method %s must be hidden from Web RPC", name)
		}
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
