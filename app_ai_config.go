package main

import (
	"context"
	"go-stock/backend/agent"
	"go-stock/backend/data"
	"go-stock/backend/models"
)

// TestAIConfig 使用前端传入的完整配置副本执行一次可用性测试，不持久化草稿。
func (a *App) TestAIConfig(config *data.AIConfig) *models.AIConfigTestResult {
	ctx := context.Background()
	if a != nil && a.ctx != nil {
		ctx = a.ctx
	}
	return agent.NewAIConfigTestService().Test(ctx, config)
}
