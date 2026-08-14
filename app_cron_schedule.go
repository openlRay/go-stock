package main

import (
	"go-stock/backend/agent"
)

// ParseCronScheduleText uses the selected AI model to fill the common schedule
// editor. The returned rule is validated but is not persisted automatically.
func (a *App) ParseCronScheduleText(req agent.CronScheduleParseRequest) (*agent.CronScheduleParseResult, error) {
	return agent.ParseCronScheduleText(a.ctx, req)
}
