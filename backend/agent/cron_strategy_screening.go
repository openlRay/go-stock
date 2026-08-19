package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/models"
	"math"
	"sort"
	"strconv"
	"strings"
)

// CronTaskTypeStrategyScreening 标识按“我的策略”执行并推送选股结果的任务。
const CronTaskTypeStrategyScreening = "strategy_screening"

const (
	strategyScreeningDetailMaxRunes   = cronNotificationMaxRunes - 600
	strategyScreeningDefaultPushLimit = 20
	strategyScreeningSearchPageSize   = 5000
	// Markdown 转义会把 <、> 等单个字符扩展为最多 4 个字符；这里按最坏情况
	// 预留空间，保证即使策略文本全是特殊字符，详情仍不会突破统一通知预算。
	strategyScreeningStrategyNameRunes = 60
	strategyScreeningQueryRunes        = 700
	strategyScreeningStockCodeRunes    = 16
	strategyScreeningStockNameRunes    = 24
	strategyScreeningMetricRunes       = 16
	strategyScreeningIndustryRunes     = 16
	strategyScreeningIndustryTopCount  = 3
)

var strategyScreeningPushLimits = map[int]struct{}{
	0:  {},
	10: {},
	20: {},
	50: {},
}

// StrategyScreeningTaskParams 是策略选股任务持久化在 Params 中的稳定契约。
type StrategyScreeningTaskParams struct {
	StrategyID uint `json:"strategyId"`
	PushLimit  int  `json:"pushLimit"`
}

type strategySearchFunc func(query string, pageSize int) (map[string]any, error)

func defaultStrategySearch(query string, pageSize int) (map[string]any, error) {
	return data.NewSearchStockApi(query).SearchStock(pageSize), nil
}

func parseStrategyScreeningTaskParams(raw string) (StrategyScreeningTaskParams, error) {
	params := StrategyScreeningTaskParams{PushLimit: strategyScreeningDefaultPushLimit}
	if strings.TrimSpace(raw) == "" {
		return params, fmt.Errorf("请选择我的策略")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return params, fmt.Errorf("策略选股任务参数不是有效 JSON: %w", err)
	}
	if fields == nil {
		return params, fmt.Errorf("策略选股任务参数必须是 JSON 对象")
	}
	strategyIDRaw, exists := fields["strategyId"]
	if !exists || strings.TrimSpace(string(strategyIDRaw)) == "null" {
		return params, fmt.Errorf("请选择我的策略")
	}
	if err := json.Unmarshal(strategyIDRaw, &params.StrategyID); err != nil || params.StrategyID == 0 {
		return params, fmt.Errorf("策略 ID 无效")
	}
	if pushLimitRaw, exists := fields["pushLimit"]; exists {
		if strings.TrimSpace(string(pushLimitRaw)) == "null" {
			return params, fmt.Errorf("推送数量无效")
		}
		if err := json.Unmarshal(pushLimitRaw, &params.PushLimit); err != nil {
			return params, fmt.Errorf("推送数量无效: %w", err)
		}
	}
	if _, allowed := strategyScreeningPushLimits[params.PushLimit]; !allowed {
		return params, fmt.Errorf("推送数量仅支持 10、20、50 或全部")
	}
	return params, nil
}

func (a *CronTaskApi) executeStrategyScreening(ctx context.Context, task *models.CronTask) (cronTaskContent, error) {
	select {
	case <-ctx.Done():
		return cronTaskContent{}, ctx.Err()
	default:
	}

	params, err := parseStrategyScreeningTaskParams(task.Params)
	if err != nil {
		return cronTaskContent{}, newCronTaskPublicError("策略选股任务参数无效，请重新编辑任务", err)
	}
	strategy, err := data.NewCustomStrategyApi().GetByID(params.StrategyID)
	if err != nil {
		if errors.Is(err, data.ErrCustomStrategyNotFound) {
			return cronTaskContent{}, newCronTaskPublicError("所选策略不存在或已删除，请重新编辑任务", err)
		}
		return cronTaskContent{}, newCronTaskPublicError("读取选股策略失败，请稍后重试", err)
	}
	strategyName := strings.TrimSpace(strategy.Name)
	if strategyName == "" {
		strategyName = "未命名策略"
	}
	query := strings.TrimSpace(strategy.Query)
	if query == "" {
		return cronTaskContent{}, newCronTaskPublicError(fmt.Sprintf("策略「%s」的选股条件为空，请先完善策略", strategyName), nil)
	}

	search := a.strategySearch
	if search == nil {
		search = defaultStrategySearch
	}
	response, err := search(query, strategyScreeningSearchPageSize)
	if err != nil {
		return cronTaskContent{}, newCronTaskPublicError("选股服务请求失败，请稍后重试", err)
	}
	select {
	case <-ctx.Done():
		return cronTaskContent{}, ctx.Err()
	default:
	}
	stocks, err := parseStrategyScreeningSearchResponse(response)
	if err != nil {
		return cronTaskContent{}, err
	}
	return buildStrategyScreeningContent(strategyName, query, stocks, params.PushLimit), nil
}

type strategyScreeningStock struct {
	Code         string
	Name         string
	LatestPrice  string
	ChangeRate   string
	TurnoverRate string
	VolumeRatio  string
	Industry     string
}

type strategyScreeningOverview struct {
	UpCount              int
	DownCount            int
	FlatCount            int
	ChangeAvailableCount int
	Industries           []strategyScreeningIndustryCount
}

type strategyScreeningIndustryCount struct {
	Name       string
	Count      int
	FirstIndex int
}

func parseStrategyScreeningSearchResponse(response map[string]any) ([]strategyScreeningStock, error) {
	if response == nil {
		return nil, newCronTaskPublicError("选股服务返回数据异常，请稍后重试", nil)
	}
	code, ok := integerValue(response["code"])
	if !ok {
		return nil, newCronTaskPublicError("选股服务返回数据异常，请稍后重试", nil)
	}
	if code != 100 {
		message := strings.TrimSpace(stringValue(response["message"]))
		if message == "" {
			message = strings.TrimSpace(stringValue(response["msg"]))
		}
		if strings.Contains(strings.ToLower(message), "qgqp_b_id") {
			return nil, newCronTaskPublicError("请先在设置中配置东财唯一标识 qgqp_b_id", nil)
		}
		return nil, newCronTaskPublicError("选股服务返回失败，请稍后重试", fmt.Errorf("upstream business code=%d", code))
	}
	responseData, ok := response["data"].(map[string]any)
	if !ok {
		return nil, newCronTaskPublicError("选股服务返回数据异常，请稍后重试", nil)
	}
	result, ok := responseData["result"].(map[string]any)
	if !ok {
		return nil, newCronTaskPublicError("选股服务返回数据异常，请稍后重试", nil)
	}
	dataList, ok := result["dataList"].([]any)
	if !ok {
		return nil, newCronTaskPublicError("选股服务返回数据异常，请稍后重试", nil)
	}

	stocks := make([]strategyScreeningStock, 0, len(dataList))
	for _, value := range dataList {
		row, ok := value.(map[string]any)
		if !ok {
			return nil, newCronTaskPublicError("选股服务返回数据异常，请稍后重试", nil)
		}
		code := normalizeInlineText(stringValue(row["SECURITY_CODE"]))
		name := normalizeInlineText(stringValue(row["SECURITY_SHORT_NAME"]))
		if name == "" {
			name = normalizeInlineText(stringValue(row["SECURITY_NAME_ABBR"]))
		}
		if code == "" || name == "" {
			return nil, newCronTaskPublicError("选股服务返回数据异常，请稍后重试", nil)
		}
		stocks = append(stocks, strategyScreeningStock{
			Code:         code,
			Name:         name,
			LatestPrice:  strategyScreeningOptionalValue(row, strategyScreeningMetricRunes, "NEW_PRICE"),
			ChangeRate:   strategyScreeningOptionalValue(row, strategyScreeningMetricRunes, "CHANGE_RATE"),
			TurnoverRate: strategyScreeningOptionalValue(row, strategyScreeningMetricRunes, "TURNOVERRATE", "TURNOVER_RATE"),
			VolumeRatio:  strategyScreeningOptionalValue(row, strategyScreeningMetricRunes, "VOLUME_RATIO"),
			Industry:     strategyScreeningOptionalValue(row, strategyScreeningIndustryRunes, "INDUSTRY"),
		})
	}
	return stocks, nil
}

func strategyScreeningOptionalValue(row map[string]any, maxRunes int, keys ...string) string {
	for _, key := range keys {
		value, exists := row[key]
		if !exists || value == nil {
			continue
		}
		switch value.(type) {
		case string, json.Number, float64, float32, int, int64, uint, uint64:
		default:
			continue
		}
		text := normalizeInlineText(stringValue(value))
		switch strings.ToLower(text) {
		case "", "-", "--", "null", "n/a", "nan":
			continue
		}
		return truncateRunes(text, maxRunes)
	}
	return ""
}

func buildStrategyScreeningContent(strategyName, query string, stocks []strategyScreeningStock, pushLimit int) cronTaskContent {
	strategyName = truncateRunes(normalizeInlineText(strategyName), strategyScreeningStrategyNameRunes)
	query = truncateRunes(normalizeInlineText(query), strategyScreeningQueryRunes)
	total := len(stocks)
	if total == 0 {
		summary := fmt.Sprintf("策略「%s」筛选完成，本次没有符合条件的股票", strategyName)
		return cronTaskContent{
			Summary: summary,
			Markdown: strings.Join([]string{
				"## 策略选股结果",
				fmt.Sprintf("- **策略名称**：%s", escapeMarkdown(strategyName)),
				fmt.Sprintf("- **选股条件**：%s", escapeMarkdown(query)),
				"- **命中总数**：0",
				"- **结果**：本次没有符合条件的股票",
			}, "\n\n"),
			PlainText: strings.Join([]string{
				"策略选股结果",
				"策略名称：" + strategyName,
				"选股条件：" + query,
				"命中总数：0",
				"结果：本次没有符合条件的股票",
			}, "\n"),
		}
	}

	targetCount := total
	if pushLimit > 0 && targetCount > pushLimit {
		targetCount = pushLimit
	}
	overview := summarizeStrategyScreeningStocks(stocks)
	displayCount := 0
	for displayCount < targetCount {
		candidate := renderStrategyScreeningContent(strategyName, query, stocks, overview, pushLimit, displayCount+1)
		if runeCount(candidate.Markdown) > strategyScreeningDetailMaxRunes || runeCount(candidate.PlainText) > strategyScreeningDetailMaxRunes {
			break
		}
		displayCount++
	}
	return renderStrategyScreeningContent(strategyName, query, stocks, overview, pushLimit, displayCount)
}

func renderStrategyScreeningContent(strategyName, query string, stocks []strategyScreeningStock, overview strategyScreeningOverview, pushLimit, displayCount int) cronTaskContent {
	total := len(stocks)
	summary := ""
	switch {
	case pushLimit == 0 && displayCount == total:
		summary = fmt.Sprintf("策略「%s」筛选完成，命中并推送 %d 只", strategyName, total)
	case pushLimit > 0 && displayCount == min(total, pushLimit):
		summary = fmt.Sprintf("策略「%s」筛选完成，命中 %d 只，推送前 %d 只", strategyName, total, displayCount)
	default:
		summary = fmt.Sprintf("策略「%s」筛选完成，命中 %d 只，实际展示 %d 只", strategyName, total, displayCount)
	}

	markdownLines := []string{
		"## 策略选股结果",
		fmt.Sprintf("- **策略名称**：%s", escapeMarkdown(strategyName)),
		fmt.Sprintf("- **选股条件**：%s", escapeMarkdown(query)),
		fmt.Sprintf("- **命中总数**：%d", total),
		fmt.Sprintf("- **实际展示**：%d", displayCount),
	}
	plainLines := []string{
		"策略选股结果",
		"策略名称：" + strategyName,
		"选股条件：" + query,
		fmt.Sprintf("命中总数：%d", total),
		fmt.Sprintf("实际展示：%d", displayCount),
	}
	if overview.ChangeAvailableCount > 0 {
		coverage := ""
		if overview.ChangeAvailableCount < total {
			coverage = fmt.Sprintf("（覆盖 %d/%d 只）", overview.ChangeAvailableCount, total)
		}
		markdownLines = append(markdownLines, fmt.Sprintf("- **涨跌分布**：涨 %d｜跌 %d｜平 %d%s",
			overview.UpCount, overview.DownCount, overview.FlatCount, coverage))
		plainLines = append(plainLines, fmt.Sprintf("涨跌分布：涨 %d｜跌 %d｜平 %d%s",
			overview.UpCount, overview.DownCount, overview.FlatCount, coverage))
	}
	if len(overview.Industries) > 0 {
		markdownIndustries := make([]string, 0, len(overview.Industries))
		plainIndustries := make([]string, 0, len(overview.Industries))
		for _, industry := range overview.Industries {
			markdownIndustries = append(markdownIndustries, fmt.Sprintf("%s %d", escapeMarkdown(industry.Name), industry.Count))
			plainIndustries = append(plainIndustries, fmt.Sprintf("%s %d", industry.Name, industry.Count))
		}
		markdownLines = append(markdownLines, "- **主要行业**："+strings.Join(markdownIndustries, "｜"))
		plainLines = append(plainLines, "主要行业："+strings.Join(plainIndustries, "｜"))
	}
	if displayCount > 0 {
		// 飞书和钉钉的 Markdown 子集都能稳定渲染列表；序号后不使用点号，避免飞书解析成嵌套列表。
		markdownLines = append(markdownLines, "", "**股票明细**")
		plainLines = append(plainLines, "股票列表：")
		for index := 0; index < displayCount; index++ {
			markdownLine, plainLine := renderStrategyScreeningStockLine(index+1, stocks[index])
			markdownLines = append(markdownLines, markdownLine)
			plainLines = append(plainLines, plainLine)
		}
	}
	return cronTaskContent{
		Summary:   summary,
		Markdown:  strings.Join(markdownLines, "\n"),
		PlainText: strings.Join(plainLines, "\n"),
	}
}

func summarizeStrategyScreeningStocks(stocks []strategyScreeningStock) strategyScreeningOverview {
	overview := strategyScreeningOverview{}
	industries := make(map[string]strategyScreeningIndustryCount)
	for index, stock := range stocks {
		if direction, ok := strategyScreeningChangeDirection(stock.ChangeRate); ok {
			overview.ChangeAvailableCount++
			switch {
			case direction > 0:
				overview.UpCount++
			case direction < 0:
				overview.DownCount++
			default:
				overview.FlatCount++
			}
		}

		industry := truncateRunes(normalizeInlineText(stock.Industry), strategyScreeningIndustryRunes)
		if industry == "" {
			continue
		}
		stat, exists := industries[industry]
		if !exists {
			stat = strategyScreeningIndustryCount{Name: industry, FirstIndex: index}
		}
		stat.Count++
		industries[industry] = stat
	}

	overview.Industries = make([]strategyScreeningIndustryCount, 0, len(industries))
	for _, stat := range industries {
		overview.Industries = append(overview.Industries, stat)
	}
	sort.SliceStable(overview.Industries, func(left, right int) bool {
		if overview.Industries[left].Count != overview.Industries[right].Count {
			return overview.Industries[left].Count > overview.Industries[right].Count
		}
		return overview.Industries[left].FirstIndex < overview.Industries[right].FirstIndex
	})
	if len(overview.Industries) > strategyScreeningIndustryTopCount {
		overview.Industries = overview.Industries[:strategyScreeningIndustryTopCount]
	}
	return overview
}

func strategyScreeningChangeDirection(value string) (int, bool) {
	value = strings.TrimSpace(strings.TrimSuffix(normalizeInlineText(value), "%"))
	value = strings.ReplaceAll(value, ",", "")
	if value == "" {
		return 0, false
	}
	change, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(change) || math.IsInf(change, 0) {
		return 0, false
	}
	switch {
	case change > 0:
		return 1, true
	case change < 0:
		return -1, true
	default:
		return 0, true
	}
}

func renderStrategyScreeningStockLine(index int, stock strategyScreeningStock) (string, string) {
	code := truncateRunes(normalizeInlineText(stock.Code), strategyScreeningStockCodeRunes)
	name := truncateRunes(normalizeInlineText(stock.Name), strategyScreeningStockNameRunes)
	markdownDetails := make([]string, 0, 5)
	plainDetails := make([]string, 0, 5)
	appendDetail := func(label, value string) {
		value = normalizeInlineText(value)
		if value == "" {
			return
		}
		markdownDetails = append(markdownDetails, label+" "+escapeMarkdown(value))
		plainDetails = append(plainDetails, label+" "+value)
	}
	appendDetail("现价", truncateRunes(stock.LatestPrice, strategyScreeningMetricRunes))
	appendDetail("涨跌", strategyScreeningPercent(stock.ChangeRate))
	appendDetail("换手", strategyScreeningPercent(stock.TurnoverRate))
	appendDetail("量比", truncateRunes(stock.VolumeRatio, strategyScreeningMetricRunes))
	appendDetail("行业", truncateRunes(stock.Industry, strategyScreeningIndustryRunes))

	markdownLine := fmt.Sprintf("- **%02d｜%s %s**", index, escapeMarkdown(code), escapeMarkdown(name))
	plainLine := fmt.Sprintf("%02d｜%s %s", index, code, name)
	if len(markdownDetails) > 0 {
		markdownLine += "｜" + strings.Join(markdownDetails, "｜")
		plainLine += "｜" + strings.Join(plainDetails, "｜")
	}
	return markdownLine, plainLine
}

func strategyScreeningPercent(value string) string {
	value = truncateRunes(normalizeInlineText(value), strategyScreeningMetricRunes)
	if value == "" || strings.HasSuffix(value, "%") {
		return value
	}
	return value + "%"
}

func normalizeInlineText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func escapeMarkdown(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"`", "\\`",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"#", "\\#",
		"|", "\\|",
		"<", "&lt;",
		">", "&gt;",
	)
	return replacer.Replace(value)
}

func integerValue(value any) (int, bool) {
	text := strings.TrimSpace(stringValue(value))
	if text == "" {
		return 0, false
	}
	number, err := strconv.Atoi(text)
	return number, err == nil
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func runeCount(value string) int {
	return len([]rune(value))
}
