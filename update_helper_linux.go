//go:build linux && !web
// +build linux,!web

package main

import "os"

func IsRunningAsAdmin() bool {
	return os.Geteuid() == 0
}

func (a *App) RestartAsAdmin() error {
	return nil
}
