package models

// AIConfigTestResult 只返回可展示的安全摘要，不包含鉴权信息、完整请求或上游响应正文。
type AIConfigTestResult struct {
	Success         bool   `json:"success"`
	Message         string `json:"message"`
	LatencyMillis   int64  `json:"latencyMillis"`
	ResponsePreview string `json:"responsePreview,omitempty"`
}
