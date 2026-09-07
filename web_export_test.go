//go:build web

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go-stock/backend/data"

	"github.com/xuri/excelize/v2"
)

func TestWebTableExport(t *testing.T) {
	server, err := newWebHTTPServer("127.0.0.1:0", &App{}, newWebEventHub())
	if err != nil {
		t.Fatal(err)
	}
	table := data.ExportTableData{
		SheetName: "选股",
		Columns: []data.ExportTableColumn{
			{Title: "股票代码", Key: "code"},
			{Title: "行情", Children: []data.ExportTableColumn{{Title: "价格", Key: "price"}}},
		},
		Rows: []map[string]interface{}{{"code": "000001", "price": 12.5}},
	}
	body, err := json.Marshal(map[string]any{"filename": `C:\private\选股.xlsx`, "table": table})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/tables/export", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://localhost")
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != webXLSXMediaType {
		t.Fatalf("export status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Download-Filename"); got != url.PathEscape("选股.xlsx") {
		t.Fatalf("download filename leaked a server path: %q", got)
	}
	workbook, err := excelize.OpenReader(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer workbook.Close()
	for cell, want := range map[string]string{"A1": "股票代码", "B1": "行情", "B2": "价格", "A3": "000001", "B3": "12.5"} {
		if got, err := workbook.GetCellValue("选股", cell); err != nil || got != want {
			t.Fatalf("cell %s=%q, want %q, err=%v", cell, got, want, err)
		}
	}
}

func TestWebTableExportRejectsInvalidRequests(t *testing.T) {
	api := &webAPI{}
	handler := webSecurityHeaders(http.HandlerFunc(api.exportTable))
	for _, tt := range []struct {
		name, method, mediaType, origin, body string
		status                                int
	}{
		{"method", "GET", "", "", "", http.StatusMethodNotAllowed},
		{"media type", "POST", "text/plain", "", "{}", http.StatusUnsupportedMediaType},
		{"cross origin", "POST", "application/json", "https://other.example", "{}", http.StatusForbidden},
		{"empty table", "POST", "application/json", "", "{}", http.StatusBadRequest},
		{"invalid json", "POST", "application/json", "", "{", http.StatusBadRequest},
		{"trailing json", "POST", "application/json", "", "{} {}", http.StatusBadRequest},
		{"server path", "POST", "application/json", "", `{"path":"/private/test.xlsx"}`, http.StatusBadRequest},
		{"oversized", "POST", "application/json", "", `{"filename":"` + strings.Repeat("x", int(webTableExportMaxSize)) + `"}`, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, "http://localhost/api/tables/export", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", tt.mediaType)
			request.Header.Set("Origin", tt.origin)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.status {
				t.Fatalf("status=%d, want %d", recorder.Code, tt.status)
			}
		})
	}
}

func TestWebUpstreamMethodsAvailable(t *testing.T) {
	methods, err := loadWebBindingMethods()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"GetGovDepartments", "GetPolicyNews", "GetKeyDeptPolicyNews", "GetAllDeptPolicyNews",
		"GetStoredPolicyNews", "GetKeyDepartments", "SaveKeyDepartments", "GetBKConstituentStocks",
		"GenerateDailyReviewNow", "GenerateMorningStrategyNow", "GetDailyReviewByDate", "GetDailyReviewList",
		"GetLatestDailyReview", "DeleteDailyReview", "GetMorningStrategyByDate", "GetMorningStrategyList",
		"GetLatestMorningStrategy", "DeleteMorningStrategy", "EnableFilesystemSkill", "DisableFilesystemSkill",
		"UpdateFilesystemSkillDescription",
	} {
		if _, ok := methods[name]; !ok {
			t.Errorf("upstream method missing from Web RPC: %s", name)
		}
	}
	server, err := newWebHTTPServer("127.0.0.1:0", &App{}, newWebEventHub())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"GenerateDailyReviewNow", "GenerateMorningStrategyNow"} {
		request := httptest.NewRequest(http.MethodPost, "/api/rpc/"+name, strings.NewReader(`{"args":["2026-02-30",0,0,""]}`))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		server.Handler.ServeHTTP(recorder, request)
		if !strings.Contains(recorder.Body.String(), "日期必须") || !strings.Contains(recorder.Body.String(), `"error"`) {
			t.Fatalf("invalid report date did not reach the error boundary: %s", recorder.Body.String())
		}
	}
}
