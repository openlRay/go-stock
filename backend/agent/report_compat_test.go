package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/models"
)

func TestReportEventsUseSharedEmitterInOrder(t *testing.T) {
	var names []string
	var payloads []any
	data.SetAppEventEmitter(func(name string, args ...any) {
		names = append(names, name)
		payloads = append(payloads, args[0])
	})
	t.Cleanup(func() { data.SetAppEventEmitter(nil) })
	emitter := newProgressEmitter(context.Background(), "dailyReviewProgress", "2026-09-07")
	emitter.write("第一段")
	emitter.write("第二段")
	emitter.flush()
	NewDailyReviewApi().emitEvent(context.Background(), "2026-09-07", models.DailyReview{Status: "success"})
	NewMorningStrategyApi().emitEvent(context.Background(), "2026-09-07", models.MorningStrategy{Status: "failed"})
	if !reflect.DeepEqual(names, []string{"dailyReviewProgress", "dailyReviewGenerated", "morningStrategyGenerated"}) {
		t.Fatalf("events lost or reordered: %v", names)
	}
	if got := payloads[0].(map[string]any)["chunk"]; got != "第一段第二段" {
		t.Fatalf("stream chunks = %v", got)
	}
}

func TestReportDefaultAIConfigUsesPersistedChatDefault(t *testing.T) {
	withCronTaskTestDB(t)
	if err := db.Dao.AutoMigrate(&data.AIConfig{}, &data.Settings{}); err != nil {
		t.Fatal(err)
	}
	configs := []data.AIConfig{
		{Name: "向量", ModelType: "embedding"},
		{Name: "早期对话", ModelType: "chat"},
		{Name: "默认对话", ModelType: "chat", IsDefault: true},
	}
	if err := db.Dao.Create(&configs).Error; err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ requested, want int }{
		{0, int(configs[2].ID)},
		{int(configs[1].ID), int(configs[1].ID)},
		{int(configs[0].ID), int(configs[2].ID)},
	} {
		if got := FirstAiConfigId(tt.requested); got != tt.want {
			t.Fatalf("config(%d)=%d, want %d", tt.requested, got, tt.want)
		}
	}
}

func TestReportInvalidDateDoesNotAccessDatabase(t *testing.T) {
	previous := db.Dao
	db.Dao = nil
	t.Cleanup(func() { db.Dao = previous })
	for _, date := range []string{"2026-02-30", "2026-9-7", "2026-09-07&sort=x"} {
		if _, err := NewDailyReviewApi().GenerateDailyReview(context.Background(), date, 0, 0, false, "", "manual"); err == nil {
			t.Errorf("daily review accepted %q", date)
		}
		if _, err := NewMorningStrategyApi().GenerateMorningStrategy(context.Background(), date, 0, 0, false, "", "manual"); err == nil {
			t.Errorf("morning strategy accepted %q", date)
		}
	}
}

func TestReportCronErrorsPreserveExecutionResult(t *testing.T) {
	withCronTaskTestDB(t)
	api := NewCronTaskApi()
	for _, kind := range []string{"daily_review", "morning_strategy"} {
		task := &models.CronTask{Name: kind, TaskType: kind, Params: "{", CronExpr: "0 0 9 * * 1-5"}
		if err := api.Create(task); err != nil {
			t.Fatal(err)
		}
		result, err := api.ExecuteTask(context.Background(), task)
		if err == nil || result == nil || result.Success {
			t.Fatalf("invalid report params did not preserve failure result: %+v, %v", result, err)
		}
		stored, err := api.GetByID(task.ID)
		if err != nil || stored.RunCount != 1 || !strings.HasPrefix(stored.LastRunResult, "失败:") {
			t.Fatalf("report failure was not persisted: %+v, %v", stored, err)
		}
	}
}
