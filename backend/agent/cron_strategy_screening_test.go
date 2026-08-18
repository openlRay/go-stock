package agent

import (
	"context"
	"errors"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"strings"
	"testing"
)

func TestParseStrategyScreeningTaskParams(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantLimit int
		wantErr   bool
	}{
		{name: "missing pushLimit defaults to 20", raw: `{"strategyId":1}`, wantLimit: 20},
		{name: "explicit zero means all", raw: `{"strategyId":1,"pushLimit":0}`, wantLimit: 0},
		{name: "ten", raw: `{"strategyId":1,"pushLimit":10}`, wantLimit: 10},
		{name: "fifty", raw: `{"strategyId":1,"pushLimit":50}`, wantLimit: 50},
		{name: "missing strategy", raw: `{"pushLimit":20}`, wantErr: true},
		{name: "zero strategy", raw: `{"strategyId":0,"pushLimit":20}`, wantErr: true},
		{name: "invalid pushLimit", raw: `{"strategyId":1,"pushLimit":30}`, wantErr: true},
		{name: "null pushLimit", raw: `{"strategyId":1,"pushLimit":null}`, wantErr: true},
		{name: "invalid json", raw: `{`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params, err := parseStrategyScreeningTaskParams(test.raw)
			if test.wantErr {
				if err == nil {
					t.Fatalf("parseStrategyScreeningTaskParams(%q) = %+v, want error", test.raw, params)
				}
				return
			}
			if err != nil || params.StrategyID != 1 || params.PushLimit != test.wantLimit {
				t.Fatalf("parseStrategyScreeningTaskParams(%q) = %+v, %v", test.raw, params, err)
			}
		})
	}
}

func TestStrategyScreeningCreateUpdateNormalizesParamsAndForcesNotification(t *testing.T) {
	withCronTaskTestDB(t)
	api := NewCronTaskApi()
	task := &models.CronTask{
		Name: "策略推送", CronExpr: "0 0 9 * * *", TaskType: CronTaskTypeStrategyScreening,
		Params: `{"strategyId":7}`, Enable: true, Status: "active", NotifyOnCompletion: false,
	}
	if err := api.Create(task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !task.NotifyOnCompletion || task.Params != `{"strategyId":7,"pushLimit":20}` {
		t.Fatalf("normalized created task = %+v", task)
	}
	task.NotifyOnCompletion = false
	task.Params = `{"strategyId":7,"pushLimit":0}`
	if err := api.Update(task); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	stored, err := api.GetByID(task.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if !stored.NotifyOnCompletion || stored.Params != `{"strategyId":7,"pushLimit":0}` {
		t.Fatalf("normalized stored task = %+v", stored)
	}

	invalid := &models.CronTask{
		Name: "无效策略推送", CronExpr: "0 0 9 * * *", TaskType: CronTaskTypeStrategyScreening,
		Params: `{"strategyId":0,"pushLimit":20}`, Enable: true, Status: "active",
	}
	if err := api.Create(invalid); err == nil {
		t.Fatal("Create() should reject an invalid strategy ID")
	}

	foundType := false
	for _, taskType := range api.GetTaskTypes() {
		if taskType.A == CronTaskTypeStrategyScreening && taskType.B == "策略选股推送" {
			foundType = true
			break
		}
	}
	if !foundType {
		t.Fatal("GetTaskTypes() should include strategy screening")
	}
}

func TestCustomStrategyGetByIDNotFound(t *testing.T) {
	withCronTaskTestDB(t)
	strategy := &models.CustomStrategy{Name: "测试策略", Query: "换手率大于5%"}
	if err := db.Dao.Create(strategy).Error; err != nil {
		t.Fatalf("seed strategy: %v", err)
	}
	got, err := data.NewCustomStrategyApi().GetByID(strategy.ID)
	if err != nil || got.Query != strategy.Query {
		t.Fatalf("GetByID() = %+v, %v", got, err)
	}
	if _, err := data.NewCustomStrategyApi().GetByID(strategy.ID + 100); !errors.Is(err, data.ErrCustomStrategyNotFound) {
		t.Fatalf("GetByID(not found) error = %v", err)
	}
}

func TestParseStrategyScreeningSearchResponseFallsBackToAbbreviation(t *testing.T) {
	stocks, err := parseStrategyScreeningSearchResponse(map[string]any{
		"code": float64(100),
		"data": map[string]any{
			"result": map[string]any{
				"dataList": []any{map[string]any{
					"SECURITY_CODE":      "000001",
					"SECURITY_NAME_ABBR": "平安银行",
					"NEW_PRICE":          12.34,
					"CHANGE_RATE":        2.5,
					"TURNOVERRATE":       "3.6",
					"VOLUME_RATIO":       1.2,
					"INDUSTRY":           "银行",
				}},
			},
		},
	})
	if err != nil || len(stocks) != 1 || stocks[0].Name != "平安银行" ||
		stocks[0].LatestPrice != "12.34" || stocks[0].ChangeRate != "2.5" ||
		stocks[0].TurnoverRate != "3.6" || stocks[0].VolumeRatio != "1.2" || stocks[0].Industry != "银行" {
		t.Fatalf("parseStrategyScreeningSearchResponse() = %+v, %v", stocks, err)
	}
}

func TestExecuteStrategyScreeningUsesLatestQueryAndKeepsUpstreamOrder(t *testing.T) {
	withCronTaskTestDB(t)
	strategy := &models.CustomStrategy{Name: "短线策略", Query: "旧条件"}
	if err := db.Dao.Create(strategy).Error; err != nil {
		t.Fatalf("seed strategy: %v", err)
	}
	api := NewCronTaskApi()
	task := newStrategyScreeningTestTask(strategy.ID, 10)
	if err := api.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := db.Dao.Model(strategy).UpdateColumn("query", "最新条件").Error; err != nil {
		t.Fatalf("update strategy query: %v", err)
	}

	stocks := makeStrategyScreeningTestStocks(12)
	var gotQuery string
	var gotPageSize int
	api.strategySearch = func(query string, pageSize int) (map[string]any, error) {
		gotQuery = query
		gotPageSize = pageSize
		return strategyScreeningTestResponse(stocks), nil
	}
	result, err := api.ExecuteTask(context.Background(), task)
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}
	if gotQuery != "最新条件" || gotPageSize != strategyScreeningSearchPageSize {
		t.Fatalf("search arguments = %q, %d", gotQuery, gotPageSize)
	}
	if !result.Success || !strings.Contains(result.Summary, "命中 12 只，推送前 10 只") {
		t.Fatalf("ExecuteTask() result = %+v", result)
	}
	if !strings.Contains(result.PlainText, "01｜000001 股票1") ||
		!strings.Contains(result.PlainText, "10｜000010 股票10") ||
		strings.Contains(result.PlainText, "11｜000011 股票11") {
		t.Fatalf("PlainText did not keep the first ten upstream rows: %q", result.PlainText)
	}
	stored, err := api.GetByID(task.ID)
	if err != nil || stored.RunCount != 1 || !strings.HasPrefix(stored.LastRunResult, "成功:") {
		t.Fatalf("stored run info = %+v, %v", stored, err)
	}
}

func TestBuildStrategyScreeningContentPushLimits(t *testing.T) {
	stocks := makeStrategyScreeningTestStocks(60)
	for _, limit := range []int{10, 20, 50} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			content := buildStrategyScreeningContent("策略", "条件", stocks, limit)
			if !strings.Contains(content.Summary, fmt.Sprintf("推送前 %d 只", limit)) {
				t.Fatalf("Summary = %q", content.Summary)
			}
			lastStock := fmt.Sprintf("- **%02d｜%06d 股票%d**", limit, limit, limit)
			nextStock := fmt.Sprintf("- **%02d｜%06d 股票%d**", limit+1, limit+1, limit+1)
			if !strings.Contains(content.Markdown, "**股票明细**") ||
				!strings.Contains(content.Markdown, lastStock) || strings.Contains(content.Markdown, nextStock) ||
				strings.Contains(content.Markdown, "| 序号 |") || strings.Contains(content.Markdown, "- 1.") {
				t.Fatalf("Markdown did not contain the expected stock list: %q", content.Markdown)
			}
			if got := strategyScreeningPlainRowCount(content.PlainText); got != limit {
				t.Fatalf("displayed rows = %d, want %d\n%s", got, limit, content.PlainText)
			}
		})
	}
}

func TestBuildStrategyScreeningContentIncludesAvailableMarketDetails(t *testing.T) {
	content := buildStrategyScreeningContent("策略", "条件", []strategyScreeningStock{{
		Code: "688502", Name: "茂莱光学", LatestPrice: "265.36", ChangeRate: "2.01",
		TurnoverRate: "5.68%", VolumeRatio: "1.32", Industry: "光学光电子",
	}}, 20)
	wantMarkdown := "- **01｜688502 茂莱光学**｜现价 265.36｜涨跌 2.01%｜换手 5.68%｜量比 1.32｜行业 光学光电子"
	wantPlainText := "01｜688502 茂莱光学｜现价 265.36｜涨跌 2.01%｜换手 5.68%｜量比 1.32｜行业 光学光电子"
	if !strings.Contains(content.Markdown, wantMarkdown) || !strings.Contains(content.PlainText, wantPlainText) {
		t.Fatalf("rich stock details were not rendered: %+v", content)
	}
}

func TestBuildStrategyScreeningContentAllAndLengthFallback(t *testing.T) {
	complete := buildStrategyScreeningContent("策略", "条件", makeStrategyScreeningTestStocks(3), 0)
	if !strings.Contains(complete.Summary, "命中并推送 3 只") || strategyScreeningPlainRowCount(complete.PlainText) != 3 {
		t.Fatalf("complete all content = %+v", complete)
	}

	limited := buildStrategyScreeningContent("策略", strings.Repeat("很长的选股条件", 200), makeStrategyScreeningTestStocks(5000), 0)
	displayed := strategyScreeningPlainRowCount(limited.PlainText)
	if displayed <= 0 || displayed >= 5000 {
		t.Fatalf("fallback displayed rows = %d", displayed)
	}
	if !strings.Contains(limited.Summary, fmt.Sprintf("实际展示 %d 只", displayed)) {
		t.Fatalf("fallback summary = %q", limited.Summary)
	}
	if runeCount(limited.Markdown) > strategyScreeningDetailMaxRunes || runeCount(limited.PlainText) > strategyScreeningDetailMaxRunes {
		t.Fatalf("fallback lengths = markdown:%d plain:%d", runeCount(limited.Markdown), runeCount(limited.PlainText))
	}

	escaped := buildStrategyScreeningContent(
		strings.Repeat("<", strategyScreeningStrategyNameRunes),
		strings.Repeat(">", strategyScreeningQueryRunes),
		makeStrategyScreeningTestStocks(1),
		0,
	)
	if runeCount(escaped.Markdown) > strategyScreeningDetailMaxRunes || runeCount(escaped.PlainText) > strategyScreeningDetailMaxRunes {
		t.Fatalf("escaped lengths = markdown:%d plain:%d", runeCount(escaped.Markdown), runeCount(escaped.PlainText))
	}
}

func TestExecuteStrategyScreeningEmptyResult(t *testing.T) {
	withCronTaskTestDB(t)
	strategy := &models.CustomStrategy{Name: "空结果策略", Query: "不可能命中的条件"}
	if err := db.Dao.Create(strategy).Error; err != nil {
		t.Fatalf("seed strategy: %v", err)
	}
	api := NewCronTaskApi()
	api.strategySearch = func(string, int) (map[string]any, error) {
		return strategyScreeningTestResponse(nil), nil
	}
	task := newStrategyScreeningTestTask(strategy.ID, 20)
	if err := api.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	result, err := api.ExecuteTask(context.Background(), task)
	if err != nil || !result.Success || !strings.Contains(result.Summary, "本次没有符合条件的股票") {
		t.Fatalf("ExecuteTask(empty) = %+v, %v", result, err)
	}
}

func TestExecuteStrategyScreeningFailuresAreSafe(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		deleteFirst bool
		search      strategySearchFunc
		wantSummary string
	}{
		{name: "deleted strategy", query: "条件", deleteFirst: true, wantSummary: "所选策略不存在或已删除"},
		{name: "empty query", query: "   ", wantSummary: "选股条件为空"},
		{
			name: "missing qgqp config", query: "条件",
			search: func(string, int) (map[string]any, error) {
				return map[string]any{"code": -1, "message": "please configure qgqp_b_id with secret details"}, nil
			},
			wantSummary: "请先在设置中配置东财唯一标识 qgqp_b_id",
		},
		{
			name: "external failure", query: "条件",
			search: func(string, int) (map[string]any, error) {
				return nil, errors.New("provider response includes private-token")
			},
			wantSummary: "选股服务请求失败，请稍后重试",
		},
		{
			name: "malformed response", query: "条件",
			search: func(string, int) (map[string]any, error) {
				return map[string]any{"code": 100, "data": map[string]any{}}, nil
			},
			wantSummary: "选股服务返回数据异常，请稍后重试",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withCronTaskTestDB(t)
			strategy := &models.CustomStrategy{Name: "失败策略", Query: test.query}
			if err := db.Dao.Create(strategy).Error; err != nil {
				t.Fatalf("seed strategy: %v", err)
			}
			api := NewCronTaskApi()
			if test.search != nil {
				api.strategySearch = test.search
			}
			task := newStrategyScreeningTestTask(strategy.ID, 20)
			if err := api.Create(task); err != nil {
				t.Fatalf("create task: %v", err)
			}
			if test.deleteFirst {
				if err := db.Dao.Delete(strategy).Error; err != nil {
					t.Fatalf("delete strategy: %v", err)
				}
			}
			result, err := api.ExecuteTask(context.Background(), task)
			if err == nil || result == nil || result.Success || !strings.Contains(result.Summary, test.wantSummary) {
				t.Fatalf("ExecuteTask() = %+v, %v", result, err)
			}
			if strings.Contains(result.Summary, "private-token") || strings.Contains(result.Summary, "secret details") {
				t.Fatalf("unsafe failure summary = %q", result.Summary)
			}
		})
	}
}

func newStrategyScreeningTestTask(strategyID uint, pushLimit int) *models.CronTask {
	return &models.CronTask{
		Name: "策略选股推送", CronExpr: "0 0 9 * * *", TaskType: CronTaskTypeStrategyScreening,
		Params: fmt.Sprintf(`{"strategyId":%d,"pushLimit":%d}`, strategyID, pushLimit),
		Enable: true, Status: "active",
	}
}

func makeStrategyScreeningTestStocks(count int) []strategyScreeningStock {
	stocks := make([]strategyScreeningStock, 0, count)
	for index := 1; index <= count; index++ {
		stocks = append(stocks, strategyScreeningStock{
			Code: fmt.Sprintf("%06d", index),
			Name: fmt.Sprintf("股票%d", index),
		})
	}
	return stocks
}

func strategyScreeningTestResponse(stocks []strategyScreeningStock) map[string]any {
	dataList := make([]any, 0, len(stocks))
	for _, stock := range stocks {
		dataList = append(dataList, map[string]any{
			"SECURITY_CODE":       stock.Code,
			"SECURITY_SHORT_NAME": stock.Name,
		})
	}
	return map[string]any{
		"code": 100,
		"data": map[string]any{
			"result": map[string]any{"dataList": dataList},
		},
	}
}

func strategyScreeningPlainRowCount(plainText string) int {
	lines := strings.Split(plainText, "\n")
	count := 0
	for _, line := range lines {
		if strings.Contains(line, "｜") {
			count++
		}
	}
	return count
}
