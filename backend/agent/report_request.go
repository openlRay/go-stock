package agent

import (
	"fmt"
	"strings"
	"time"
)

// NormalizeReportDate 在入库和查询行情前校验日期，空值按调度器使用的业务时区取当天。
func NormalizeReportDate(date string) (string, error) {
	date = strings.TrimSpace(date)
	if date == "" {
		return time.Now().In(cronTaskLocation()).Format("2006-01-02"), nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", date, cronTaskLocation())
	if err != nil || parsed.Format("2006-01-02") != date {
		return "", fmt.Errorf("日期必须为有效的 YYYY-MM-DD 格式")
	}
	return date, nil
}

func reportPagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if page > 10000 {
		page = 10000
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}
