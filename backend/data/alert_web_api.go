//go:build web

package data

// AlertWindowsApi 在 Web 模式下通过前端事件代替操作系统通知。
type AlertWindowsApi struct {
	AppID   string
	Title   string
	Content string
	Icon    string
}

func NewAlertWindowsApi(appID, title, content, icon string) *AlertWindowsApi {
	return &AlertWindowsApi{
		AppID:   appID,
		Title:   title,
		Content: content,
		Icon:    icon,
	}
}

func (a AlertWindowsApi) SendNotification() bool {
	if !GetSettingConfig().LocalPushEnable {
		return false
	}
	EmitAppEvent("browserNotification", map[string]any{
		"title":   a.Title,
		"content": a.Content,
	})
	return true
}
