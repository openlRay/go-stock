package models

import "time"

const CronTaskExecutedEventName = "cronTaskExecuted"

// CronTaskExecutedEvent 在任务结果完成持久化后发送，供 Desktop Wails 与 Web SSE 共用。
type CronTaskExecutedEvent struct {
	TaskID      uint      `json:"taskId"`
	Success     bool      `json:"success"`
	CompletedAt time.Time `json:"completedAt"`
}
