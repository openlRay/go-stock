package main

import (
	"context"
	"errors"
	"fmt"
	"go-stock/backend/agent"
	"go-stock/backend/logger"
	"go-stock/backend/models"
)

// GetMottos 返回用户格言列表。
func (a *App) GetMottos() ([]models.Motto, error) {
	mottos, err := agent.NewMottoApi().List()
	if err != nil {
		logger.SugaredLogger.Errorf("读取格言列表失败: %v", err)
		return nil, fmt.Errorf("读取格言列表失败，请稍后重试")
	}
	return mottos, nil
}

// CreateMotto 创建格言。
func (a *App) CreateMotto(content string) (*models.Motto, error) {
	motto, err := agent.NewMottoApi().Create(content)
	if err != nil {
		return nil, mottoRPCError("创建格言", err)
	}
	return motto, nil
}

// UpdateMotto 更新格言。
func (a *App) UpdateMotto(id uint, content string) (*models.Motto, error) {
	motto, err := agent.NewMottoApi().Update(id, content)
	if err != nil {
		return nil, mottoRPCError("更新格言", err)
	}
	return motto, nil
}

// DeleteMotto 删除格言。
func (a *App) DeleteMotto(id uint) error {
	if err := agent.NewMottoApi().Delete(id); err != nil {
		return mottoRPCError("删除格言", err)
	}
	return nil
}

// PolishMotto 润色当前草稿；该方法不写入格言表。
func (a *App) PolishMotto(req agent.MottoPolishRequest) (*agent.MottoPolishResult, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := agent.NewMottoApi().Polish(ctx, req)
	if err != nil {
		// MottoApi 已把配置、模型和输入错误转换为安全且可操作的用户提示。
		return nil, err
	}
	return result, nil
}

func mottoRPCError(action string, err error) error {
	switch {
	case errors.Is(err, agent.ErrMottoContentEmpty),
		errors.Is(err, agent.ErrMottoContentTooLong),
		errors.Is(err, agent.ErrMottoNotFound):
		return err
	default:
		// 依赖层错误只进入日志，RPC 返回稳定中文提示，避免暴露数据库或模型细节。
		logger.SugaredLogger.Errorf("%s失败: %v", action, err)
		return fmt.Errorf("%s失败，请稍后重试", action)
	}
}
