//go:build web

package main

import (
	"context"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"os"
	"time"

	"github.com/duke-git/lancet/v2/convertor"
	"github.com/duke-git/lancet/v2/strutil"
	"github.com/wailsapp/wails/v2/pkg/options"
)

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	data.ConfigureFromSettings(data.GetSettingConfig())
	data.SetAppEventEmitter(a.emit)
	if os.Getenv("GO_STOCK_WEB_DISABLE_JOBS") == "1" {
		logger.SugaredLogger.Info("Web background jobs are disabled")
		return
	}
	a.InitCronTasks()
	preCacheTradingDays()
	go a.domReady(ctx)
	logger.SugaredLogger.Infof("Web application startup Version:%s", Version)
}

func (a *App) beforeClose(context.Context) bool {
	return false
}

func (a *App) shutdownWeb() {
	a.stopFeishuBotInternal()
	if a.cron != nil {
		a.cron.Stop()
	}
}

func OnSecondInstanceLaunch(options.SecondInstanceData) {}

func getScreenResolution() (int, int, int, int, error) {
	return 1412, 834, 900, 600, nil
}

// MonitorStockPrices 在 Web 模式下只推送浏览器事件，不调用托盘或系统通知。
func MonitorStockPrices(a *App) {
	isAStockOpen := isTradingTime(time.Now())
	isHKStockOpen := IsHKTradingTime(time.Now())
	isUSStockOpen := IsUSTradingTime(time.Now())
	if !isAStockOpen && !isHKStockOpen && !isUSStockOpen {
		return
	}

	followed := &[]data.FollowedStock{}
	db.Dao.Model(&data.FollowedStock{}).Find(followed)
	stockInfos := GetStockInfos(*followed...)
	total := float64(0)
	for _, stockInfo := range *stockInfos {
		if strutil.HasPrefixAny(stockInfo.Code, []string{"SZ", "SH", "sh", "sz"}) && !isTradingTime(time.Now()) {
			continue
		}
		if strutil.HasPrefixAny(stockInfo.Code, []string{"hk", "HK"}) && !IsHKTradingTime(time.Now()) {
			continue
		}
		if strutil.HasPrefixAny(stockInfo.Code, []string{"us", "US", "gb_"}) && !IsUSTradingTime(time.Now()) {
			continue
		}
		total += stockInfo.ProfitAmountToday
		price, _ := convertor.ToFloat(stockInfo.Price)
		if stockInfo.PrePrice != price {
			a.emit("stock_price", stockInfo)
		}
	}
	a.emit("realtime_profit", fmt.Sprintf("  %.2f", total))
}
