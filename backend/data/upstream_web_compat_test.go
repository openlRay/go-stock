package data

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBKConstituentsRequestValidationAndQuery(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/qt/clist/get" || r.URL.Query().Get("fs") != "b:BK0475" || r.URL.Query().Get("pz") != "100" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"total":1,"diff":[{"f12":"000001","f14":"平安银行","f2":12.5,"f62":5000}]}}`)
	}))
	defer server.Close()
	previous := bkConstituentsHosts
	bkConstituentsHosts = []string{server.URL}
	t.Cleanup(func() { bkConstituentsHosts = previous })
	timeout := SharedHTTPClient.GetClient().Timeout
	for _, code := range []string{"", "BK0475&fs=m:1", "../BK0475"} {
		if got := NewBKConstituentsApi().GetBKConstituentStocks(code); len(got) != 0 {
			t.Errorf("invalid code returned data: %q", code)
		}
	}
	if requests != 0 {
		t.Fatal("invalid input triggered an HTTP request")
	}
	stocks := NewBKConstituentsApi().GetBKConstituentStocks(" bk0475 ")
	if requests != 1 || len(stocks) != 1 || stocks[0].Code != "000001" || stocks[0].Price != 12.5 {
		t.Fatalf("request/result mismatch: requests=%d stocks=%+v", requests, stocks)
	}
	if SharedHTTPClient.GetClient().Timeout != timeout {
		t.Fatal("板块请求修改了共享 HTTP 超时")
	}
}

func TestPolicyRequestsRejectInvalidInputBeforeNetwork(t *testing.T) {
	requests := 0
	previous := sharedTransport
	sharedTransport = &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		requests++
		return nil, fmt.Errorf("unexpected network request")
	}}
	t.Cleanup(func() { sharedTransport = previous })
	for _, address := range []string{
		"https://www.gov.cn.evil.example/policy", "https://www.gov.cn@evil.example/policy",
		"file://www.gov.cn/policy", "http://127.0.0.1/policy", "https://www.gov.cn:18888/policy",
	} {
		result := NewPolicyNewsApi().GetPolicyNewsDetail(address)
		if !strings.Contains(result, "仅支持政府部门") {
			t.Errorf("invalid URL not rejected: %q => %s", address, result)
		}
	}
	api := NewGovPolicyLibApi()
	for _, args := range [][3]string{{"bad-field", "", "score"}, {"title", "unknown-category", "score"}, {"title", "", "bad-sort"}} {
		if result := api.SearchGovPolicyLibrary("", args[0], "", args[1], args[2], 1, 10); len(*result) != 0 {
			t.Error("invalid enum returned data")
		}
	}
	if result := api.SearchGovPolicyLibrary("", "title", "", "", "score", 10001, 10); len(*result) != 0 {
		t.Error("invalid page returned data")
	}
	oldNames, oldAt := govPolicyDeptNames, govPolicyDeptNamesAt
	govPolicyDeptNames, govPolicyDeptNamesAt = []string{"商务部"}, time.Now()
	t.Cleanup(func() { govPolicyDeptNames, govPolicyDeptNamesAt = oldNames, oldAt })
	if result := api.SearchGovPolicyLibrary("", "title", "不存在的部门", "", "score", 1, 10); len(*result) != 0 {
		t.Error("未知部门不应退回全部门结果")
	}
	if requests != 0 {
		t.Fatalf("invalid input triggered %d network requests", requests)
	}
}

func TestPolicyDetailRejectsRedirectOutsideGovernment(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Redirect(w, r, "http://private.example/policy", http.StatusFound)
	}))
	defer server.Close()
	previous := sharedTransport
	dialer := &net.Dialer{}
	sharedTransport = &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, server.Listener.Addr().String())
	}}
	t.Cleanup(func() { sharedTransport.CloseIdleConnections(); sharedTransport = previous })
	result := NewPolicyNewsApi().GetPolicyNewsDetail("http://www.gov.cn/policy")
	if requests != 1 || !strings.Contains(result, "跳转超出政府网站范围") {
		t.Fatalf("redirect escaped the boundary: requests=%d result=%s", requests, result)
	}
}

func TestBuildTableXLSXRejectsUnboundedInput(t *testing.T) {
	for name, table := range map[string]ExportTableData{
		"deep headers":     {Columns: []ExportTableColumn{{Children: []ExportTableColumn{{Children: []ExportTableColumn{{Key: "code"}}}}}}, Rows: []map[string]interface{}{{"code": "000001"}}},
		"too many rows":    {Columns: []ExportTableColumn{{Key: "code"}}, Rows: make([]map[string]interface{}, 10001)},
		"too many columns": {Columns: make([]ExportTableColumn, 257), Rows: []map[string]interface{}{{}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := (StockDataApi{}).BuildTableXLSX(table); err == nil {
				t.Fatal("unbounded export accepted")
			}
		})
	}
}
