package main

import "go-stock/backend/data"

// SetDefaultAIConfig selects the single global default model configuration.
func (a *App) SetDefaultAIConfig(id uint) (*data.AIConfig, error) {
	return data.SetDefaultAIConfig(id)
}
