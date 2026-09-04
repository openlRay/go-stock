package data

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"go-stock/backend/db"
)

func TestLhbSeatDetail(t *testing.T) {
	if os.Getenv("GO_STOCK_RUN_LIVE_TESTS") != "1" {
		t.Skip("设置 GO_STOCK_RUN_LIVE_TESTS=1 后运行真实龙虎榜接口测试")
	}
	db.Init("../../data/stock.db")
	api := NewLhbSeatApi()
	detail := api.GetLhbSeatDetail("600077", "2022-03-10")
	if detail == nil {
		t.Fatal("detail is nil")
	}
	if len(detail.BuySeats) == 0 {
		t.Fatal("买入席位为空")
	}
	if len(detail.SellSeats) == 0 {
		t.Fatal("卖出席位为空")
	}
	if detail.Explanation == "" {
		t.Fatal("上榜原因为空")
	}
	t.Logf("上榜原因: %s", detail.Explanation)
	t.Logf("买入席位数: %d, 卖出席位数: %d", len(detail.BuySeats), len(detail.SellSeats))
	for i, seat := range detail.BuySeats {
		t.Logf("买%d %s [%s] %s(%s) 买入%.2f万 占比%.2f%%", i+1, seat.OperateDeptName, seat.SeatType, seat.HotMoneyName, seat.Tier, seat.Buy/1e4, seat.BuyRatio*100)
	}
	for i, seat := range detail.SellSeats {
		t.Logf("卖%d %s [%s] %s(%s) 卖出%.2f万 占比%.2f%%", i+1, seat.OperateDeptName, seat.SeatType, seat.HotMoneyName, seat.Tier, seat.Sell/1e4, seat.SellRatio*100)
	}

	// 席位类型识别单测（龙虎榜全称带"股份有限公司"，须归一化后匹配标准名录的简写席位）
	cases := []struct {
		dept string
		typ  string
		hm   string
	}{
		{"机构专用", "机构", ""},
		{"沪股通专用", "北向资金", ""},
		{"华鑫证券有限责任公司上海分公司", "游资", "炒股养家"}, // 名录中同时属于养家席位与量化席位，游资优先
		{"中国银河证券股份有限公司绍兴证券营业部", "游资", "赵老哥"},
		{"国泰君安证券股份有限公司上海江苏路证券营业部", "游资", "章盟主"},
		{"东方财富证券股份有限公司拉萨团结路第二证券营业部", "散户", "拉萨天团(散户集中营)"},
		{"华泰证券股份有限公司总部", "量化", "量化"},
		{"某不知名证券营业部", "营业部", ""},
	}
	for _, c := range cases {
		typ, hm := classifyLhbSeat(c.dept)
		if typ != c.typ || hm != c.hm {
			t.Errorf("classifyLhbSeat(%s) = (%s,%s), want (%s,%s)", c.dept, typ, hm, c.typ, c.hm)
		}
	}

	// 游资画像字段（tier/style/riskLevel）
	if entry := lookupHotMoneySeat("国泰君安证券股份有限公司上海江苏路证券营业部"); entry == nil || entry.tier == "" || entry.style == "" {
		t.Errorf("lookupHotMoneySeat 画像字段缺失: %+v", entry)
	}

	md := api.GetLhbSeatDetailToMarkdown("600077", "2022-03-10")
	if md == "" || len(md) < 100 {
		t.Fatalf("markdown 输出异常: %s", md)
	}
	t.Logf("markdown 预览:\n%s", md[:300])
}

// TestLhbDailySummary 验证当日游资/机构动向汇总（真实数据，2022-03-10 上榜约 40+ 只）
func TestLhbDailySummary(t *testing.T) {
	if os.Getenv("GO_STOCK_RUN_LIVE_TESTS") != "1" {
		t.Skip("设置 GO_STOCK_RUN_LIVE_TESTS=1 后运行真实龙虎榜接口测试")
	}
	db.Init("../../data/stock.db")
	api := NewLhbSeatApi()
	summary := api.GetLhbDailySummary("2022-03-10")
	if summary == nil {
		t.Fatal("summary is nil")
	}
	if summary.StockCount == 0 {
		t.Fatal("当日上榜个股为空")
	}
	t.Logf("日期=%s 上榜个股数=%d 游资活跃数=%d 机构个股数=%d",
		summary.Date, summary.StockCount, len(summary.HotMoneyActivities), len(summary.InstitutionActions))
	if len(summary.HotMoneyActivities) == 0 {
		t.Fatal("游资动向为空（2022-03-10 应有知名游资上榜）")
	}
	if len(summary.InstitutionActions) == 0 {
		t.Fatal("机构动向为空")
	}
	for i, act := range summary.HotMoneyActivities {
		if i >= 8 {
			break
		}
		stocks := make([]string, 0, len(act.Stocks))
		for _, s := range act.Stocks {
			stocks = append(stocks, fmt.Sprintf("%s(买%.0f万/卖%.0f万)", s.StockName, s.Buy/1e4, s.Sell/1e4))
		}
		t.Logf("游资#%d %s[%s] 合计买%.0f万/卖%.0f万: %s",
			i+1, act.HotMoneyName, act.Tier, act.TotalBuy/1e4, act.TotalSell/1e4, strings.Join(stocks, ", "))
	}
	for i, ia := range summary.InstitutionActions {
		if i >= 5 {
			break
		}
		t.Logf("机构#%d %s(%s) 买方%d席/卖方%d席 净额%.0f万",
			i+1, ia.StockName, ia.StockCode, ia.BuyCount, ia.SellCount, ia.Net/1e4)
	}
}

// TestNormalizeRemoteSeatURL 验证 GitHub blob 链接自动转 raw
func TestNormalizeRemoteSeatURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://gh-proxy.com/https://github.com/ArvinLovegood/go-stock/blob/dev/data/hot_money_seats.json",
			"https://gh-proxy.com/https://raw.githubusercontent.com/ArvinLovegood/go-stock/dev/data/hot_money_seats.json"},
		{"https://github.com/a/b/blob/main/c.json",
			"https://raw.githubusercontent.com/a/b/main/c.json"},
		{"https://gh-proxy.com/https://raw.githubusercontent.com/a/b/main/c.json",
			"https://gh-proxy.com/https://raw.githubusercontent.com/a/b/main/c.json"}, // 已是 raw 不变
		{"https://example.com/seats.json", "https://example.com/seats.json"}, // 非 GitHub 不变
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeRemoteSeatURL(c.in); got != c.want {
			t.Errorf("normalizeRemoteSeatURL(%s) = %s, want %s", c.in, got, c.want)
		}
	}
	for _, rawURL := range []string{
		"http://example.com/seats.json",
		"file:///etc/passwd",
		"https://localhost/seats.json",
		"https://127.0.0.1/seats.json",
		"https://192.168.1.10/seats.json",
		"https://user:password@example.com/seats.json",
	} {
		if err := validateRemoteSeatURL(rawURL); err == nil {
			t.Errorf("validateRemoteSeatURL(%q) 应拒绝危险地址", rawURL)
		}
	}
	if err := validateRemoteSeatURL("https://example.com/seats.json"); err != nil {
		t.Fatalf("合法 HTTPS 地址被拒绝: %v", err)
	}
}

func TestNormalizeLhbInputs(t *testing.T) {
	for _, stockCode := range []string{"600519", "000001"} {
		if !isValidLhbCode(stockCode) {
			t.Errorf("isValidLhbCode(%q) = false", stockCode)
		}
	}
	for _, stockCode := range []string{"", "60051", "600519x", `600519\")`} {
		if isValidLhbCode(stockCode) {
			t.Errorf("isValidLhbCode(%q) = true", stockCode)
		}
	}
	if date, ok := normalizeLhbDate("2026-09-04"); !ok || date != "2026-09-04" {
		t.Fatalf("normalizeLhbDate() = %q, %v", date, ok)
	}
	for _, date := range []string{"2026-02-30", "2026/09/04", `2026-09-04')`} {
		if _, ok := normalizeLhbDate(date); ok {
			t.Errorf("normalizeLhbDate(%q) 应拒绝非法日期", date)
		}
	}
}

func useHotMoneySeatTestFile(t *testing.T) {
	t.Helper()
	previousFile := hotMoneySeatsFile
	hotMoneySeatsFile = filepath.Join(t.TempDir(), "hot_money_seats.json")
	hotMoneySeatsOnce = sync.Once{}
	hotMoneySeatIndex = nil
	t.Cleanup(func() {
		hotMoneySeatsFile = previousFile
		hotMoneySeatsOnce = sync.Once{}
		hotMoneySeatIndex = nil
	})
}

// TestHotMoneySeatsExternalFile 验证外置 JSON 名录（新标准格式）加载与匹配
func TestHotMoneySeatsExternalFile(t *testing.T) {
	useHotMoneySeatTestFile(t)
	seed := builtinHotMoneySeatsSeed()
	seed.RemoteURL = ""
	raw, err := json.Marshal(seed)
	if err != nil {
		t.Fatalf("序列化测试名录失败: %v", err)
	}
	if err := os.WriteFile(hotMoneySeatsFile, raw, 0o644); err != nil {
		t.Fatalf("写入测试名录失败: %v", err)
	}
	idx := loadHotMoneySeatIndex()
	if len(idx) == 0 {
		t.Fatal("游资名录索引为空")
	}
	// 测试使用独立临时文件，不能读写真实 runtime root 下的名录。
	if hm := matchHotMoneySeat("中国银河证券股份有限公司绍兴证券营业部"); hm != "赵老哥" {
		t.Errorf("matchHotMoneySeat(银河绍兴) = %s, want 赵老哥", hm)
	}
	t.Logf("当前生效游资名录索引条目数: %d", len(idx))
}

// TestHotMoneySeatsSaveReset 验证名录保存（校验+落盘+索引热更新）与重置回环
func TestHotMoneySeatsSaveReset(t *testing.T) {
	useHotMoneySeatTestFile(t)

	// 保存自定义名录：含新增游资，应即时生效
	custom := builtinHotMoneySeatsSeed()
	custom.RemoteURL = ""
	custom.HotMoneyList = append(custom.HotMoneyList, HotMoneySeat{
		Name: "测试游资", Tier: "测试梯队", Style: "测试风格", Risk: "低",
		Seats: []HotMoneySeatBranch{{Branch: "测试证券测试路证券营业部", Primary: true}},
	})
	if err := SaveHotMoneySeats(&custom); err != nil {
		t.Fatalf("保存名录失败: %v", err)
	}
	if hm := matchHotMoneySeat("测试证券股份有限公司测试路证券营业部"); hm != "测试游资" {
		t.Errorf("保存后热更新未生效: %s", hm)
	}

	// 校验失败场景：花名为空
	bad := builtinHotMoneySeatsSeed()
	bad.HotMoneyList[0].Name = ""
	if err := SaveHotMoneySeats(&bad); err == nil {
		t.Error("花名为空应返回错误")
	}

	// 重置回内置数据
	if err := ResetHotMoneySeats(); err != nil {
		t.Fatalf("重置名录失败: %v", err)
	}
	if hm := matchHotMoneySeat("测试证券股份有限公司测试路证券营业部"); hm != "" {
		t.Errorf("重置后测试游资应被清除: %s", hm)
	}

}
