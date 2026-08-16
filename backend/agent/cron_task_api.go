package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

const cronTaskTimezone = "Asia/Shanghai"

const CronTaskTypeMottoPush = "motto_push"

const (
	cronLastRunResultMaxRunes = 500
	cronNotificationMaxRunes  = 4000
)

// CronTaskExecutionResult 是任务持久化完成后交给通知层的统一结果。
type CronTaskExecutionResult struct {
	Success       bool      `json:"success"`
	Summary       string    `json:"summary"`
	Markdown      string    `json:"markdown"`
	PlainText     string    `json:"plainText"`
	CompletedAt   time.Time `json:"completedAt"`
	LastRunResult string    `json:"lastRunResult"`
}

type cronTaskContent struct {
	Summary   string
	Markdown  string
	PlainText string
}

type cronTaskPublicError struct {
	message string
	cause   error
}

func (e *cronTaskPublicError) Error() string {
	return e.message
}

func (e *cronTaskPublicError) Unwrap() error {
	return e.cause
}

func newCronTaskPublicError(message string, cause error) error {
	return &cronTaskPublicError{message: message, cause: cause}
}

func cronTaskFailureSummary(err error) string {
	var publicErr *cronTaskPublicError
	switch {
	case errors.As(err, &publicErr):
		return publicErr.message
	case errors.Is(err, context.Canceled):
		return "任务执行已取消"
	case errors.Is(err, context.DeadlineExceeded):
		return "任务执行超时"
	default:
		// 外部 HTTP、数据库或模型错误可能携带响应正文、URL 凭据等敏感信息。
		return "任务执行失败，请稍后重试"
	}
}

func cronTaskLocation() *time.Location {
	location, err := time.LoadLocation(cronTaskTimezone)
	if err != nil {
		logger.SugaredLogger.Warnf("加载定时任务时区失败，回退到本地时区：%v", err)
		return time.Local
	}
	return location
}

// CronTaskLocation 返回定时调度和结果展示共享的业务时区。
func CronTaskLocation() *time.Location {
	return cronTaskLocation()
}

type CronTaskApi struct{}

func NewCronTaskApi() *CronTaskApi {
	return &CronTaskApi{}
}

func (a *CronTaskApi) Create(task *models.CronTask) error {
	if task == nil {
		return fmt.Errorf("任务信息不能为空")
	}
	if task.TaskType == CronTaskTypeMottoPush {
		task.NotifyOnCompletion = true
	}
	requestedEnable := task.Enable
	return db.Dao.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		if requestedEnable {
			return nil
		}
		// CronTask.Enable 的 legacy default:true 会让 GORM 把 Go 零值改写为 true；
		// 显式回写 false，保证创建停用任务时数据库和调度状态一致。
		if err := tx.Model(&models.CronTask{}).Where("id = ?", task.ID).UpdateColumn("enable", false).Error; err != nil {
			return err
		}
		task.Enable = false
		return nil
	})
}

func (a *CronTaskApi) Update(task *models.CronTask) error {
	if task == nil || task.ID == 0 {
		return fmt.Errorf("无效的任务ID")
	}
	if task.TaskType == CronTaskTypeMottoPush {
		task.NotifyOnCompletion = true
	}

	updates := map[string]any{
		"name":                 task.Name,
		"cron_expr":            task.CronExpr,
		"task_type":            task.TaskType,
		"target":               task.Target,
		"params":               task.Params,
		"enable":               task.Enable,
		"notify_on_completion": task.NotifyOnCompletion,
		"status":               task.Status,
		"description":          task.Description,
	}

	return db.Dao.Model(&models.CronTask{}).
		Where("id = ?", task.ID).
		Updates(updates).Error
}

func (a *CronTaskApi) Delete(id uint) error {
	return db.Dao.Delete(&models.CronTask{}, id).Error
}

func (a *CronTaskApi) GetByID(id uint) (*models.CronTask, error) {
	var task models.CronTask
	err := db.Dao.First(&task, id).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (a *CronTaskApi) List(query *models.CronTaskQuery) *models.CronTaskPageResp {
	var tasks []models.CronTask
	var total int64

	dbQuery := db.Dao.Model(&models.CronTask{})

	if query.Name != "" {
		dbQuery = dbQuery.Where("name LIKE ?", "%"+query.Name+"%")
	}
	if query.TaskType != "" {
		dbQuery = dbQuery.Where("task_type = ?", query.TaskType)
	}
	if query.Status != "" {
		dbQuery = dbQuery.Where("status = ?", query.Status)
	}
	if query.Enable != nil {
		dbQuery = dbQuery.Where("enable = ?", *query.Enable)
	}

	dbQuery.Count(&total)

	page := query.Page
	pageSize := query.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}

	err := dbQuery.Offset((page - 1) * pageSize).Limit(pageSize).Order("created_at DESC").Find(&tasks).Error
	if err != nil {
		logger.SugaredLogger.Errorf("查询定时任务列表失败:%s", err.Error())
		return nil
	}

	return &models.CronTaskPageResp{
		Total: int(total),
		Data:  tasks,
	}
}

func (a *CronTaskApi) GetAll() []models.CronTask {
	var tasks []models.CronTask
	db.Dao.Where("enable = ?", true).Order("created_at DESC").Find(&tasks)
	return tasks
}

func (a *CronTaskApi) ExistsByTaskType(taskType string) bool {
	var count int64
	db.Dao.Model(&models.CronTask{}).Where("task_type = ?", taskType).Count(&count)
	return count > 0
}

func (a *CronTaskApi) EnableTask(id uint, enable bool) error {
	return db.Dao.Model(&models.CronTask{}).Where("id = ?", id).Updates(map[string]any{
		"enable": enable,
	}).Error
}

func (a *CronTaskApi) UpdateRunInfo(id uint, lastRunAt time.Time, nextRunAt *time.Time, lastRunResult string) error {
	return db.Dao.Model(&models.CronTask{}).Where("id = ?", id).Updates(map[string]any{
		"last_run_at":     lastRunAt,
		"next_run_at":     nextRunAt,
		"run_count":       gorm.Expr("run_count + 1"),
		"last_run_result": lastRunResult,
	}).Error
}

func (a *CronTaskApi) GetTaskTypes() []lo.Tuple2[string, string] {
	return []lo.Tuple2[string, string]{
		{A: "stock_analysis", B: "股票分析"},
		{A: "market_analysis", B: "市场分析"},
		{A: "global_stock_index_cache", B: "全球指数缓存"},
		{A: "stock_change_save", B: "异动数据保存"},
		{A: CronTaskTypeMottoPush, B: "推送格言"},
	}
}

func (a *CronTaskApi) ValidateCronExpr(expr string) error {
	_, err := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(expr)
	return err
}

func (a *CronTaskApi) CalculateNextRunTimes(cronExpr string, count int) []time.Time {
	if count <= 0 {
		return []time.Time{}
	}

	schedule, err := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(cronExpr)
	if err != nil {
		logger.SugaredLogger.Errorf("解析 Cron 表达式失败：%v", err)
		return []time.Time{}
	}

	times := make([]time.Time, 0, count)
	next := time.Now().In(cronTaskLocation())
	for i := 0; i < count; i++ {
		next = schedule.Next(next)
		times = append(times, next)
	}
	return times
}

func (a *CronTaskApi) SearchTasks(keyword string) []models.CronTask {
	var tasks []models.CronTask
	query := db.Dao.Model(&models.CronTask{})
	if keyword != "" {
		keyword = strings.TrimSpace(keyword)
		query = query.Where("name LIKE ? OR target LIKE ? OR description LIKE ?",
			"%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
	}
	query.Order("created_at DESC").Limit(20).Find(&tasks)
	return tasks
}

func (a *CronTaskApi) ExecuteTask(ctx context.Context, task *models.CronTask) (*CronTaskExecutionResult, error) {
	if task == nil {
		return nil, fmt.Errorf("任务信息不能为空")
	}
	logger.SugaredLogger.Infof("开始执行定时任务：%s (ID: %d)", task.Name, task.ID)

	content, err := a.executeTaskByType(ctx, task)
	completedAt := time.Now()
	result := &CronTaskExecutionResult{
		Success:     err == nil,
		CompletedAt: completedAt,
	}
	if err != nil {
		result.Summary = truncateRunes(cronTaskFailureSummary(err), cronLastRunResultMaxRunes-4)
		result.Markdown = result.Summary
		result.PlainText = result.Summary
		logger.SugaredLogger.Errorf("执行定时任务失败，task_id=%d task_type=%s error_type=%T", task.ID, task.TaskType, err)
	} else {
		result.Summary = strings.TrimSpace(content.Summary)
		if result.Summary == "" {
			result.Summary = "任务执行完成"
		}
		result.Markdown = truncateRunes(strings.TrimSpace(content.Markdown), cronNotificationMaxRunes)
		result.PlainText = truncateRunes(strings.TrimSpace(content.PlainText), cronNotificationMaxRunes)
		if result.Markdown == "" {
			result.Markdown = result.Summary
		}
		if result.PlainText == "" {
			result.PlainText = result.Summary
		}
	}
	statusPrefix := "成功: "
	if !result.Success {
		statusPrefix = "失败: "
	}
	result.LastRunResult = truncateRunes(statusPrefix+result.Summary, cronLastRunResultMaxRunes)

	nextRunAt := a.CalculateNextRunTime(task.CronExpr)
	if updateErr := a.UpdateRunInfo(task.ID, completedAt, &nextRunAt, result.LastRunResult); updateErr != nil {
		logger.SugaredLogger.Errorf("更新定时任务运行信息失败，task_id=%d error_type=%T", task.ID, updateErr)
		// nil result 表示完成状态尚未持久化，App 层不得发送完成事件或结果通知。
		return nil, fmt.Errorf("更新任务运行信息失败: %w", updateErr)
	}

	return result, err
}

func (a *CronTaskApi) executeTaskByType(ctx context.Context, task *models.CronTask) (cronTaskContent, error) {
	switch task.TaskType {
	case "stock_analysis":
		return a.executeStockAnalysis(ctx, task)
	case "market_analysis":
		return a.executeMarketAnalysis(ctx, task)
	case "global_stock_index_cache":
		return a.executeGlobalStockIndexCache(ctx, task)
	case "fund_analysis":
		return a.executeFundAnalysis(ctx, task)
	case "news_fetch":
		return a.executeNewsFetch(ctx, task)
	case "stock_monitor":
		return a.executeStockMonitor(ctx, task)
	case "stock_change_save":
		return a.executeStockChangeSave(ctx, task)
	case "custom":
		return a.executeCustomTask(ctx, task)
	case CronTaskTypeMottoPush:
		return a.executeMottoPush(ctx, task)
	default:
		logger.SugaredLogger.Warnf("未知任务类型：%s", task.TaskType)
		return cronTaskContent{}, newCronTaskPublicError(fmt.Sprintf("未知任务类型：%s", task.TaskType), nil)
	}
}

func (a *CronTaskApi) CalculateNextRunTime(cronExpr string) time.Time {
	schedule, err := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(cronExpr)
	if err != nil {
		return time.Now().In(cronTaskLocation()).Add(time.Hour)
	}
	return schedule.Next(time.Now().In(cronTaskLocation()))
}

func (a *CronTaskApi) executeStockAnalysis(ctx context.Context, task *models.CronTask) (cronTaskContent, error) {
	logger.SugaredLogger.Infof("执行股票分析任务：%s", task.Name)
	var params struct {
		PromptId    int    `json:"promptId"`
		AiConfigId  int    `json:"aiConfigId"`
		SysPromptId int    `json:"sysPromptId"`
		Thinking    bool   `json:"thinking"`
		StockCode   string `json:"stockCode"`
		StockName   string `json:"stockName"`
		AgentMode   string `json:"agentMode"`
	}
	if task.Params != "" {
		err := json.Unmarshal([]byte(task.Params), &params)
		if err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return cronTaskContent{}, err
		}
	}

	prompt := fmt.Sprintf("分析总结市场资讯，针对%s[%s]，找出潜在投资机会", params.StockName, params.StockCode)
	prompt = data.NewPromptTemplateApi().GetPromptTemplateByID(params.PromptId)
	var tools []data.Tool
	tools = data.Tools(tools)
	msgs := data.NewDeepSeekOpenAi(ctx, params.AiConfigId).NewChatStream(params.StockName, data.ConvertTushareCodeToStockCode(params.StockCode), prompt, &params.SysPromptId, tools, params.Thinking)
	content := &strings.Builder{}
	for msg := range msgs {
		if v, ok := msg["content"].(string); ok {
			content.WriteString(v)
		}
	}
	analysis := strings.TrimSpace(content.String())
	data.NewDeepSeekOpenAi(ctx, params.AiConfigId).SaveAIResponseResult(params.StockCode, params.StockName, analysis, "", prompt)
	return cronTaskContent{
		Summary:   fmt.Sprintf("股票 %s[%s] 分析完成", params.StockName, params.StockCode),
		Markdown:  analysis,
		PlainText: analysis,
	}, nil
}

func (a *CronTaskApi) executeFundAnalysis(ctx context.Context, task *models.CronTask) (cronTaskContent, error) {
	var params struct {
		FundCodes  []string `json:"fund_codes"`
		AiConfigId int      `json:"ai_config_id"`
	}

	if task.Params != "" {
		err := json.Unmarshal([]byte(task.Params), &params)
		if err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return cronTaskContent{}, err
		}
	}

	for _, fundCode := range params.FundCodes {
		select {
		case <-ctx.Done():
			return cronTaskContent{}, ctx.Err()
		default:
			logger.SugaredLogger.Infof("分析基金：%s", fundCode)
		}
	}

	return cronTaskContent{Summary: fmt.Sprintf("已完成 %d 只基金的分析检查", len(params.FundCodes))}, nil
}

func (a *CronTaskApi) executeNewsFetch(ctx context.Context, task *models.CronTask) (cronTaskContent, error) {
	select {
	case <-ctx.Done():
		return cronTaskContent{}, ctx.Err()
	default:
		news := data.NewMarketNewsApi().TelegraphList(30)
		count := 0
		if news != nil {
			count = len(*news)
		}
		logger.SugaredLogger.Info("新闻抓取完成")
		return cronTaskContent{Summary: fmt.Sprintf("新闻抓取完成，共获取 %d 条", count)}, nil
	}
}

func (a *CronTaskApi) executeStockMonitor(ctx context.Context, task *models.CronTask) (cronTaskContent, error) {
	var params struct {
		StockCodes      []string `json:"stock_codes"`
		PriceThreshold  float64  `json:"price_threshold"`
		ChangeThreshold float64  `json:"change_threshold"`
	}

	if task.Params != "" {
		err := json.Unmarshal([]byte(task.Params), &params)
		if err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return cronTaskContent{}, err
		}
	}

	for _, stockCode := range params.StockCodes {
		select {
		case <-ctx.Done():
			return cronTaskContent{}, ctx.Err()
		default:
			logger.SugaredLogger.Infof("监控股票：%s", stockCode)
		}
	}

	return cronTaskContent{Summary: fmt.Sprintf("已检查 %d 只股票的监控条件", len(params.StockCodes))}, nil
}

func (a *CronTaskApi) executeCustomTask(ctx context.Context, task *models.CronTask) (cronTaskContent, error) {
	logger.SugaredLogger.Infof("执行自定义任务：%s", task.Name)
	return cronTaskContent{Summary: "自定义任务执行完成"}, nil
}

func (a *CronTaskApi) executeMarketAnalysis(ctx context.Context, task *models.CronTask) (cronTaskContent, error) {
	logger.SugaredLogger.Infof("执行市场分析任务：%s", task.Name)
	var params struct {
		PromptId    int    `json:"promptId"`
		AiConfigId  int    `json:"aiConfigId"`
		SysPromptId int    `json:"sysPromptId"`
		Thinking    bool   `json:"thinking"`
		AgentMode   string `json:"agentMode"`
	}
	if task.Params != "" {
		err := json.Unmarshal([]byte(task.Params), &params)
		if err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return cronTaskContent{}, err
		}
	}

	prompt := "分析总结市场资讯，找出潜在投资机会"
	prompt = data.NewPromptTemplateApi().GetPromptTemplateByID(params.PromptId)
	persistedContent := &strings.Builder{}
	notificationContent := &strings.Builder{}

	ch := NewStockAiAgentApi().ChatWithContext(ctx, prompt, params.AiConfigId, &params.SysPromptId, false, 0, false, params.AgentMode)
	for msg := range ch {
		if msg.ReasoningContent != "" {
			persistedContent.WriteString(msg.ReasoningContent)
		}
		persistedContent.WriteString(msg.Content)
		notificationContent.WriteString(msg.Content)
	}
	persistedAnalysis := strings.TrimSpace(persistedContent.String())
	data.NewDeepSeekOpenAi(ctx, params.AiConfigId).SaveAIResponseResult("市场分析", "市场分析", persistedAnalysis, "", prompt)
	// reasoning 只沿用历史持久化语义，任务完成通知仅发送模型的最终回答。
	finalAnalysis := strings.TrimSpace(notificationContent.String())
	return cronTaskContent{Summary: "市场分析完成", Markdown: finalAnalysis, PlainText: finalAnalysis}, nil
}

func (a *CronTaskApi) executeGlobalStockIndexCache(ctx context.Context, task *models.CronTask) (cronTaskContent, error) {
	logger.SugaredLogger.Infof("执行全球指数缓存任务：%s", task.Name)
	var params struct {
		CrawlTimeOut uint `json:"crawlTimeOut"`
	}
	if task.Params != "" {
		err := json.Unmarshal([]byte(task.Params), &params)
		if err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return cronTaskContent{}, err
		}
	}
	if params.CrawlTimeOut == 0 {
		params.CrawlTimeOut = 30
	}
	if err := data.NewMarketNewsApi().CacheGlobalStockIndexes(params.CrawlTimeOut); err != nil {
		return cronTaskContent{}, err
	}
	return cronTaskContent{Summary: "全球指数缓存完成"}, nil
}

func (a *CronTaskApi) executeStockChangeSave(ctx context.Context, task *models.CronTask) (cronTaskContent, error) {
	logger.SugaredLogger.Infof("执行异动数据保存任务：%s", task.Name)

	if !isTradingTime() {
		logger.SugaredLogger.Info("当前不在A股交易时间，跳过异动数据保存")
		return cronTaskContent{Summary: "当前不在 A 股交易时间，已跳过异动数据保存"}, nil
	}

	var params struct {
		ChangeTypes []int `json:"changeTypes"`
		DeleteDays  int   `json:"deleteDays"`
	}

	if task.Params != "" {
		err := json.Unmarshal([]byte(task.Params), &params)
		if err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return cronTaskContent{}, err
		}
	}

	if len(params.ChangeTypes) == 0 {
		params.ChangeTypes = []int{
			8201, 8202, 8193, 4, 32, 64, 8207, 8209, 8211, 8213, 8215,
			8204, 8203, 8194, 8, 16, 128, 8208, 8210, 8212, 8214, 8216,
		}
	}

	api := data.NewStockChangesApi()
	result := api.GetStockChanges(params.ChangeTypes, 0, 500)
	if result == nil || len(result.Data) == 0 {
		logger.SugaredLogger.Info("没有获取到异动数据")
		return cronTaskContent{Summary: "没有获取到新的异动数据"}, nil
	}

	savedCount, err := data.NewStockChangeHistoryService().SaveStockChangesWithDedup(result.Data)
	if err != nil {
		logger.SugaredLogger.Errorf("保存异动数据失败：%v", err)
		return cronTaskContent{}, err
	}

	logger.SugaredLogger.Infof("成功保存 %d 条异动数据（去重后）", savedCount)

	if params.DeleteDays > 0 {
		err = data.NewStockChangeHistoryService().DeleteOldData(params.DeleteDays)
		if err != nil {
			logger.SugaredLogger.Warnf("删除旧数据失败：%v", err)
		} else {
			logger.SugaredLogger.Infof("已删除 %d 天前的历史数据", params.DeleteDays)
		}
	}

	return cronTaskContent{Summary: fmt.Sprintf("成功保存 %d 条异动数据（去重后）", savedCount)}, nil
}

func (a *CronTaskApi) executeMottoPush(ctx context.Context, task *models.CronTask) (cronTaskContent, error) {
	select {
	case <-ctx.Done():
		return cronTaskContent{}, ctx.Err()
	default:
	}
	mottos, err := NewMottoApi().Random(3)
	if err != nil {
		return cronTaskContent{}, err
	}
	if len(mottos) == 0 {
		return cronTaskContent{}, newCronTaskPublicError("格言库为空，请先在我的 → 格言中添加格言", nil)
	}
	markdownLines := []string{"## 今日格言"}
	plainLines := []string{"今日格言"}
	for index, motto := range mottos {
		content := strings.Join(strings.Fields(motto.Content), " ")
		markdownLines = append(markdownLines, fmt.Sprintf("%d. %s", index+1, content))
		plainLines = append(plainLines, fmt.Sprintf("%d. %s", index+1, content))
	}
	return cronTaskContent{
		Summary:   fmt.Sprintf("已随机选取 %d 条格言", len(mottos)),
		Markdown:  strings.Join(markdownLines, "\n\n"),
		PlainText: strings.Join(plainLines, "\n"),
	}, nil
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func isTradingTime() bool {
	now := time.Now()
	weekday := now.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return false
	}

	hour, minute := now.Hour(), now.Minute()
	currentTime := hour*100 + minute

	morningStart := 915
	morningEnd := 1130
	afternoonStart := 1300
	afternoonEnd := 1500

	isMorning := currentTime >= morningStart && currentTime <= morningEnd
	isAfternoon := currentTime >= afternoonStart && currentTime <= afternoonEnd

	return isMorning || isAfternoon
}
