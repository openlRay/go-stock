//go:build web

package main

import (
	"context"
	"errors"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/db"
	log "go-stock/backend/logger"
	"go-stock/backend/machineid"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := runWeb(); err != nil {
		log.SugaredLogger.Fatal(err)
	}
}

func runWeb() error {
	for _, dir := range []string{"data", "logs", "skills"} {
		checkDir(dir)
	}

	machineid.Init(BuildKey)
	data.SetAppIcon(icon)
	db.Init("")
	data.InitAnalyzeSentiment()
	AutoMigrate()

	hub := newWebEventHub()
	app := NewApp()
	app.webMode = true
	app.setEventEmitter(hub)
	data.SetAppEventEmitter(hub.Emit)
	defer func() {
		app.shutdownWeb()
		data.SetAppEventEmitter(nil)
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app.startup(ctx)

	addr := os.Getenv("GO_STOCK_WEB_ADDR")
	if addr == "" {
		addr = ":18888"
	}
	server, err := newWebHTTPServer(addr, app, hub)
	if err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		log.SugaredLogger.Infof("go-stock Web Server started at %s", addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("关闭 Web Server 失败: %w", err)
	}
	return nil
}
