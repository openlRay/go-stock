package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/models"
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
	Code string
	Name string
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
		stocks = append(stocks, strategyScreeningStock{Code: code, Name: name})
	}
	return stocks, nil
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
	displayCount := 0
	for displayCount < targetCount {
		candidate := renderStrategyScreeningContent(strategyName, query, stocks, pushLimit, displayCount+1)
		if runeCount(candidate.Markdown) > strategyScreeningDetailMaxRunes || runeCount(candidate.PlainText) > strategyScreeningDetailMaxRunes {
			break
		}
		displayCount++
	}
	return renderStrategyScreeningContent(strategyName, query, stocks, pushLimit, displayCount)
}

func renderStrategyScreeningContent(strategyName, query string, stocks []strategyScreeningStock, pushLimit, displayCount int) cronTaskContent {
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
	if displayCount > 0 {
		markdownLines = append(markdownLines, "| 序号 | 股票代码 | 股票名称 |", "| ---: | --- | --- |")
		plainLines = append(plainLines, "股票列表：")
		for index := 0; index < displayCount; index++ {
			code := truncateRunes(normalizeInlineText(stocks[index].Code), strategyScreeningStockCodeRunes)
			name := truncateRunes(normalizeInlineText(stocks[index].Name), strategyScreeningStockNameRunes)
			markdownLines = append(markdownLines, fmt.Sprintf("| %d | %s | %s |", index+1, escapeMarkdown(code), escapeMarkdown(name)))
			plainLines = append(plainLines, fmt.Sprintf("%d. %s %s", index+1, code, name))
		}
	}
	return cronTaskContent{
		Summary:   summary,
		Markdown:  strings.Join(markdownLines, "\n"),
		PlainText: strings.Join(plainLines, "\n"),
	}
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
