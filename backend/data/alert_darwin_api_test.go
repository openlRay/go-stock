//go:build darwin && !web
// +build darwin,!web

package data

import (
	"testing"
)

// @Author 2lovecode
// @Date 2025/02/06 17:50
// @Desc
// -----------------------------------------------------------------------------------

func TestAlert(t *testing.T) {
	alert := NewAlertWindowsApi("go-stock", "Hello, World!", "This is a notification.", "../../build/appicon.png")
	if alert.AppID != "go-stock" || alert.Title != "Hello, World!" || alert.Content != "This is a notification." {
		t.Fatalf("unexpected alert: %+v", alert)
	}
}
