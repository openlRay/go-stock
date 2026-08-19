package main

import (
	"context"
	"fmt"
	"go-stock/backend/agent"
	"go-stock/backend/data"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"strings"
)

type cronTaskResultPusher func(*models.CronTask, *agent.CronTaskExecutionResult) error

type cronTaskNotificationPayload struct {
	Title       string
	Markdown    string
	PlainText   string
	CompletedAt string
}

type cronTaskFeishuNotification struct {
	Title   string
	Message string
	Options data.FeishuCardOptions
}

// executeCronTask 是调度器和立即执行共享的唯一完成通知入口。
func (a *App) executeCronTask(task *models.CronTask) error {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	result, executeErr := agent.NewCronTaskApi().ExecuteTask(ctx, task)
	if task != nil && result != nil {
		// ExecuteTask 返回前已经写回运行次数和最终结果；完成事件只能在该持久化边界之后发送。
		a.emit(models.CronTaskExecutedEventName, models.CronTaskExecutedEvent{
			TaskID:      task.ID,
			Success:     result.Success,
			CompletedAt: result.CompletedAt,
		})
	}
	if task != nil && task.NotifyOnCompletion && result != nil {
		pusher := a.cronResultPusher
		if pusher == nil {
			pusher = a.pushCronTaskResult
		}
		// 通知属于任务完成后的旁路副作用，其失败不得覆盖业务执行结果。
		if err := pusher(task, result); err != nil {
			logger.SugaredLogger.Warnf("定时任务结果通知失败，task_id=%d: %v", task.ID, err)
		}
	}
	return executeErr
}

func (a *App) pushCronTaskResult(task *models.CronTask, result *agent.CronTaskExecutionResult) error {
	if task == nil || result == nil {
		return fmt.Errorf("任务结果为空")
	}
	settings := data.GetSettingConfig()
	if settings == nil || settings.Settings == nil {
		return fmt.Errorf("通知设置不可用")
	}
	if !settings.LocalPushEnable && !settings.FeishuPushEnable && !settings.DingPushEnable {
		logger.SugaredLogger.Infof("定时任务未启用任何全局通知渠道，跳过推送，task_id=%d", task.ID)
		return nil
	}

	payload := buildCronTaskNotificationPayload(task, result)

	if settings.LocalPushEnable {
		// macOS 的现有通知适配器通过 AppleScript 拼接文本；移除双引号避免用户格言破坏脚本字符串。
		localTitle := strings.ReplaceAll(payload.Title, `"`, "'")
		localContent := strings.ReplaceAll(payload.PlainText, `"`, "'")
		go func() {
			if ok := data.NewAlertWindowsApi("go-stock定时任务", localTitle, localContent, "").SendNotification(); !ok {
				logger.SugaredLogger.Warnf("发送定时任务本地通知失败，task_id=%d", task.ID)
			}
		}()
		go a.emit("newsPush", map[string]any{
			"time":    payload.CompletedAt,
			"isRed":   !result.Success,
			"source":  "go-stock定时任务",
			"content": payload.PlainText,
		})
	}
	if settings.FeishuPushEnable {
		feishu := buildCronTaskFeishuNotification(task, result, payload.CompletedAt)
		go func() {
			response := data.NewFeishuAPI().SendToFeishuWithOptions(feishu.Title, feishu.Message, feishu.Options)
			if strings.Contains(response, "失败") || strings.Contains(response, "未配置") {
				// 第三方响应可能包含上游诊断正文，只记录渠道和任务 ID。
				logger.SugaredLogger.Warnf("发送定时任务飞书通知失败，task_id=%d", task.ID)
			}
		}()
	}
	if settings.DingPushEnable {
		go func() {
			response := data.NewDingDingAPI().SendToDingDing(payload.Title, payload.Markdown)
			if strings.Contains(response, "失败") {
				logger.SugaredLogger.Warnf("发送定时任务钉钉通知失败，task_id=%d", task.ID)
			}
		}()
	}
	return nil
}

func buildCronTaskNotificationPayload(task *models.CronTask, result *agent.CronTaskExecutionResult) cronTaskNotificationPayload {
	status := "成功"
	if !result.Success {
		status = "失败"
	}
	title := fmt.Sprintf("定时任务%s：%s", status, task.Name)
	completedAt := result.CompletedAt.In(agent.CronTaskLocation()).Format("2006-01-02 15:04:05")
	markdown := fmt.Sprintf("## %s\n\n- **状态**：%s\n- **完成时间**：%s\n- **结果摘要**：%s", title, status, completedAt, result.Summary)
	plain := fmt.Sprintf("%s\n状态：%s\n完成时间：%s\n结果摘要：%s", title, status, completedAt, result.Summary)
	if detail := strings.TrimSpace(result.Markdown); detail != "" && detail != result.Summary {
		markdown += "\n\n### 详情\n\n" + detail
	}
	if detail := strings.TrimSpace(result.PlainText); detail != "" && detail != result.Summary {
		plain += "\n详情：\n" + detail
	}
	return cronTaskNotificationPayload{
		Title:       title,
		Markdown:    markdown,
		PlainText:   plain,
		CompletedAt: completedAt,
	}
}

func buildCronTaskFeishuNotification(task *models.CronTask, result *agent.CronTaskExecutionResult, completedAt string) cronTaskFeishuNotification {
	titlePrefix := "任务完成"
	headerTemplate := "green"
	if task.TaskType == agent.CronTaskTypeStrategyScreening {
		titlePrefix = "策略选股完成"
	}
	message := strings.TrimSpace(result.Markdown)
	if !result.Success {
		titlePrefix = "任务失败"
		if task.TaskType == agent.CronTaskTypeStrategyScreening {
			titlePrefix = "策略选股失败"
		}
		headerTemplate = "red"
		message = "**失败原因**：" + strings.TrimSpace(result.Summary)
	} else {
		if message == "" || message == strings.TrimSpace(result.Summary) {
			message = strings.TrimSpace(result.Summary)
		}
		message = trimLeadingMarkdownHeading(message)
	}

	footer := []string{"完成时间：" + completedAt}
	if task.TaskType == agent.CronTaskTypeStrategyScreening {
		footer = append(footer,
			"数据源：东方财富",
			"*选股结果仅供参考，不构成投资建议*",
		)
	}
	message = strings.TrimSpace(message) + "\n\n---\n" + strings.Join(footer, "  ·  ")
	return cronTaskFeishuNotification{
		Title:   titlePrefix + "｜" + task.Name,
		Message: message,
		Options: data.FeishuCardOptions{
			HeaderTemplate: headerTemplate,
			MentionAll:     false,
		},
	}
}

func trimLeadingMarkdownHeading(markdown string) string {
	lines := strings.Split(strings.TrimSpace(markdown), "\n")
	if len(lines) == 0 || !isMarkdownHeadingLine(lines[0]) {
		return strings.TrimSpace(markdown)
	}
	lines = lines[1:]
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func isMarkdownHeadingLine(line string) bool {
	line = strings.TrimSpace(line)
	headingLevel := 0
	for headingLevel < len(line) && headingLevel < 6 && line[headingLevel] == '#' {
		headingLevel++
	}
	return headingLevel > 0 && headingLevel < len(line) && line[headingLevel] == ' '
}
