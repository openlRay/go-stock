package data

import (
	"go-stock/backend/logger"
	"os"
	"strings"
	"sync"
)

type browserPathResolver struct {
	once   sync.Once
	path   string
	detect func() (string, bool)
}

func newBrowserPathResolver(detect func() (string, bool)) *browserPathResolver {
	return &browserPathResolver{detect: detect}
}

var defaultBrowserPathResolver = newBrowserPathResolver(CheckBrowser)

func resolveBrowserPath(configuredPath string) string {
	return defaultBrowserPathResolver.resolve(configuredPath)
}

func (r *browserPathResolver) resolve(configuredPath string) string {
	if path := strings.TrimSpace(configuredPath); path != "" {
		return path
	}

	r.once.Do(func() {
		if path := strings.TrimSpace(os.Getenv("CHROME_BIN")); path != "" {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				r.path = path
				logger.SugaredLogger.Infof("使用 CHROME_BIN 浏览器：%s", path)
				return
			}
		}

		if path, ok := r.detect(); ok {
			r.path = strings.TrimSpace(path)
		}
	})

	return r.path
}
