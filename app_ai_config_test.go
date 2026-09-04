package main

import (
	"context"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAppTestAIConfigDraftDoesNotPersist(t *testing.T) {
	previous := db.Dao
	database, err := gorm.Open(sqlite.New(sqlite.Config{DriverName: "sqlite", DSN: "file:" + t.Name() + "?mode=memory&cache=shared"}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := database.AutoMigrate(&data.AIConfig{}); err != nil {
		t.Fatalf("migrate AI config: %v", err)
	}
	db.Dao = database
	t.Cleanup(func() { db.Dao = previous })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2]}]}`))
	}))
	defer server.Close()

	draft := &data.AIConfig{
		Name: "未保存草稿", BaseUrl: server.URL + "/v1", ApiKey: "draft-secret",
		ModelName: "draft-embedding", ModelType: "embedding", TimeOut: 2,
	}
	result := (&App{ctx: context.Background()}).TestAIConfig(draft)
	if result == nil || !result.Success {
		t.Fatalf("TestAIConfig() = %+v", result)
	}
	var count int64
	if err := db.Dao.Model(&data.AIConfig{}).Count(&count).Error; err != nil {
		t.Fatalf("count AI configs: %v", err)
	}
	if count != 0 || draft.ID != 0 {
		t.Fatalf("draft test persisted data: count=%d draft=%+v", count, draft)
	}
}
