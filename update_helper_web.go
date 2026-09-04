//go:build web

package main

import "errors"

func IsRunningAsAdmin() bool {
	return false
}

func (a *App) RestartAsAdmin() error {
	return errors.New("Web 模式不支持以管理员身份重启")
}

// ApplyMacUpdate 仅用于满足共享更新流程的跨平台编译；Web 入口会在调用更新逻辑前拒绝。
func ApplyMacUpdate(string) error {
	return errors.New("Web 模式不支持客户端自更新")
}
