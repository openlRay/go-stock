//go:build web

package tools

import "os/exec"

// applyHiddenWindow 在 Web 模式下为空实现。
// Web 进程没有桌面窗口，不能携带 Windows 的 HideWindow/CREATE_NO_WINDOW 配置。
func applyHiddenWindow(_ *exec.Cmd) {}
