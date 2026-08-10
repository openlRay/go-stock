//go:build darwin && !web
// +build darwin,!web

package main

func IsRunningAsAdmin() bool {
	return true
}

func (a *App) RestartAsAdmin() error {
	return nil
}
