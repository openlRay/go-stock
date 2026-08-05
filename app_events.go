package main

import "github.com/wailsapp/wails/v2/pkg/runtime"

// EventEmitter 统一桌面 Wails 事件和 Web SSE 事件的发送边界。
type EventEmitter interface {
	Emit(name string, data ...any)
}

func (a *App) setEventEmitter(emitter EventEmitter) {
	a.eventEmitter = emitter
}

func (a *App) emit(name string, data ...any) {
	if a == nil {
		return
	}
	if a.eventEmitter != nil {
		a.eventEmitter.Emit(name, data...)
		return
	}
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, name, data...)
	}
}
