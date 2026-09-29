//go:build web

package main

import (
	"encoding/json"
	"testing"
)

func TestWebUpstreamMethodsAndValidation(t *testing.T) {
	methods, err := loadWebBindingMethods()
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{
		"ChatWithAgent", "SummaryStockNews", "UploadImageToImageBed", "CreatePromptBacktestTask",
		"GetPromptBacktestTaskList", "GetPromptBacktestTaskDetail", "GetPromptBacktestPicks", "DeletePromptBacktestTask",
		"GetRecommendBacktestStats", "ListRecommendBacktest", "ListRecommendBacktestByTemplate", "ListRecommendBacktestBySkill",
		"GetPromptTemplateBacktestStats", "GetPromptTemplateBacktestDetail", "GetAiRecommendStocksTodayStats",
		"NotifySignal", "SaveSignalRecords", "GetSignalRecordPage", "GetSignalStats", "ClearSignalRecords",
		"TestDingDingNotice", "TestFeishuNotice", "VacuumDatabase",
	} {
		if _, ok := methods[method]; !ok {
			t.Errorf("Web RPC 缺少新绑定 %s", method)
		}
	}
	app := &App{webMode: true}
	for _, tc := range []struct {
		method string
		args   []json.RawMessage
	}{
		{"UploadImageToImageBed", []json.RawMessage{json.RawMessage(`"bad-base64"`), json.RawMessage(`"x.png"`)}},
		{"CreatePromptBacktestTask", []json.RawMessage{json.RawMessage(`{}`)}},
		{"ChatWithAgent", nil},
		{"SummaryStockNews", nil},
	} {
		if _, err := callWebMethod(app, tc.method, tc.args); err == nil {
			t.Errorf("%s 应拒绝非法参数", tc.method)
		}
	}
}

func TestWebSignalEventsUseSSE(t *testing.T) {
	hub := newWebEventHub()
	client := hub.subscribe()
	defer hub.unsubscribe(client)
	app := &App{webMode: true, eventEmitter: hub}
	app.signalMonitorTick()
	select {
	case payload := <-client:
		var event webEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Name != "signalMonitorTick" {
			t.Fatalf("错误事件: %+v", event)
		}
	default:
		t.Fatal("信号节拍未到达 SSE")
	}
	if result := app.NotifySignal("测试", "内容", "提醒", []string{NotifyChannelApp}); result != "ok" {
		t.Fatal(result)
	}
	select {
	case payload := <-client:
		var event webEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Name != "newsPush" || len(event.Data) != 1 {
			t.Fatalf("信号提醒未到达 SSE: %+v", event)
		}
	default:
		t.Fatal("信号提醒未到达 SSE")
	}
}
