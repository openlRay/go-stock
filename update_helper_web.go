//go:build web

package main

import "errors"

func IsRunningAsAdmin() bool {
	return false
}

func (a *App) RestartAsAdmin() error {
	return errors.New("Web 模式不支持以管理员身份重启")
}
