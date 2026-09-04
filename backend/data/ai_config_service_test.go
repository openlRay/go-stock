package data

import (
	"context"
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"sync"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func withAIConfigTestDB(t *testing.T) {
	t.Helper()
	previous := db.Dao
	database, err := gorm.Open(sqlite.New(sqlite.Config{DriverName: "sqlite", DSN: "file:" + t.Name() + "?mode=memory&cache=shared"}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := database.AutoMigrate(&AIConfig{}, &Settings{}, &models.CronTask{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	db.Dao = database
	t.Cleanup(func() {
		db.Dao = previous
	})
}

func validTestAIConfig(name string) *AIConfig {
	return &AIConfig{
		Name: name, BaseUrl: "http://localhost:8317/v1", ApiKey: "secret", ModelName: "gpt-5.6-sol",
		ModelType: "chat", MaxTokens: 8192, ContextWindow: 128000,
		Temperature: 0, TemperatureConfigured: true, TimeOut: 300,
		TopP: pointer(0.8), StopSequences: []string{"END", "DONE"}, ResponseFormat: "json_object",
		ReasoningMode: ReasoningModeOn, ReasoningEffort: "high",
		ExtraHeaders: `{"x-team-id":"team"}`, EmbeddingModel: "text-embedding-3-small",
	}
}

func TestAIConfigCRUDRoundTrip(t *testing.T) {
	withAIConfigTestDB(t)

	created, err := createAIConfig(validTestAIConfig("primary"))
	if err != nil {
		t.Fatalf("createAIConfig() error = %v", err)
	}
	if created.ID == 0 {
		t.Fatal("created config has no ID")
	}
	if !created.IsDefault {
		t.Fatal("first created config should become the default")
	}

	var stored AIConfig
	if err := db.Dao.First(&stored, created.ID).Error; err != nil {
		t.Fatalf("read created config: %v", err)
	}
	if !stored.TemperatureConfigured || stored.Temperature != 0 || len(stored.StopSequences) != 2 || stored.StopSequences[1] != "DONE" {
		t.Fatalf("optional values did not round-trip: %+v", stored)
	}
	if stored.ModelType != "chat" || stored.ContextWindow != 128000 || stored.ExtraHeaders != `{"x-team-id":"team"}` || stored.EmbeddingModel != "text-embedding-3-small" {
		t.Fatalf("upstream AI config fields did not round-trip: %+v", stored)
	}

	stored.TopP = nil
	stored.StopSequences = []string{"STOP"}
	stored.ReasoningEffort = "low"
	updated, err := updateAIConfig(&stored)
	if err != nil {
		t.Fatalf("updateAIConfig() error = %v", err)
	}
	if updated.TopP != nil || len(updated.StopSequences) != 1 || updated.StopSequences[0] != "STOP" || updated.ReasoningEffort != "low" {
		t.Fatalf("updated optional values did not round-trip: %+v", updated)
	}

	copyOne, err := copyAIConfig(created.ID)
	if err != nil {
		t.Fatalf("copyAIConfig() error = %v", err)
	}
	copyTwo, err := copyAIConfig(created.ID)
	if err != nil {
		t.Fatalf("second copyAIConfig() error = %v", err)
	}
	if copyOne.Name != "primary-副本" || copyTwo.Name != "primary-副本2" {
		t.Fatalf("unexpected copy names: %q, %q", copyOne.Name, copyTwo.Name)
	}
	if copyOne.IsDefault || copyTwo.IsDefault {
		t.Fatal("copied configs must never inherit the default flag")
	}

	deleted, err := deleteAIConfig(copyOne.ID)
	if err != nil || !deleted.Success {
		t.Fatalf("deleteAIConfig() = %+v, %v", deleted, err)
	}
}

func TestEmbeddingAIConfigDoesNotParticipateInChatDefault(t *testing.T) {
	withAIConfigTestDB(t)

	embedding := validTestAIConfig("embedding")
	embedding.ModelType = "embedding"
	embedding.ModelName = "text-embedding-3-small"
	embedding.MaxTokens = 0
	createdEmbedding, err := createAIConfig(embedding)
	if err != nil {
		t.Fatalf("create embedding config: %v", err)
	}
	if createdEmbedding.IsDefault {
		t.Fatal("embedding config must not become the default chat model")
	}

	chat, err := createAIConfig(validTestAIConfig("chat"))
	if err != nil {
		t.Fatalf("create chat config: %v", err)
	}
	if !chat.IsDefault {
		t.Fatal("first chat config should become the default")
	}
	if _, err := setDefaultAIConfig(createdEmbedding.ID); err == nil {
		t.Fatal("embedding config should be rejected as the default chat model")
	}

	configs := []*AIConfig{createdEmbedding, chat}
	resolved, ok := ResolveAIConfig(configs, int(createdEmbedding.ID))
	if !ok || resolved.ID != chat.ID {
		t.Fatalf("embedding explicit ID should fall back to chat default: %+v, %v", resolved, ok)
	}
}

func TestValidateAIConfigRejectsInvalidExtraHeaders(t *testing.T) {
	config := validTestAIConfig("invalid headers")
	config.ExtraHeaders = `{"x-test":"line\nbreak"}`
	if err := ValidateAIConfig(config); err == nil {
		t.Fatal("header values containing newlines should fail")
	}
}

func TestSetDefaultAIConfigAndResolverOrder(t *testing.T) {
	withAIConfigTestDB(t)
	first, err := createAIConfig(validTestAIConfig("first"))
	if err != nil {
		t.Fatalf("create first config: %v", err)
	}
	second, err := createAIConfig(validTestAIConfig("second"))
	if err != nil {
		t.Fatalf("create second config: %v", err)
	}
	if second.IsDefault {
		t.Fatal("only the first created config should be the automatic default")
	}

	selected, err := setDefaultAIConfig(second.ID)
	if err != nil {
		t.Fatalf("setDefaultAIConfig() error = %v", err)
	}
	if !selected.IsDefault {
		t.Fatal("selected config was not returned as default")
	}

	var configs []*AIConfig
	if err := db.Dao.Order("id ASC").Find(&configs).Error; err != nil {
		t.Fatalf("list configs: %v", err)
	}
	var defaultCount int
	for _, config := range configs {
		if config.IsDefault {
			defaultCount++
		}
	}
	if defaultCount != 1 {
		t.Fatalf("default count = %d, want 1", defaultCount)
	}

	if resolved, ok := ResolveAIConfig(configs, int(first.ID)); !ok || resolved.ID != first.ID {
		t.Fatalf("explicit config was not preserved: %+v, %v", resolved, ok)
	}
	if resolved, ok := ResolveAIConfig(configs, 0); !ok || resolved.ID != second.ID {
		t.Fatalf("default config was not resolved: %+v, %v", resolved, ok)
	}
	if resolved, ok := ResolveAIConfig(configs, 999999); !ok || resolved.ID != second.ID {
		t.Fatalf("missing explicit ID should fall back to default: %+v, %v", resolved, ok)
	}

	for _, config := range configs {
		config.IsDefault = false
	}
	if resolved, ok := ResolveAIConfig(configs, 0); !ok || resolved.ID != first.ID {
		t.Fatalf("legacy first config fallback failed: %+v, %v", resolved, ok)
	}
}

func TestDeleteDefaultAIConfigPromotesStableFallback(t *testing.T) {
	withAIConfigTestDB(t)
	first, err := createAIConfig(validTestAIConfig("first"))
	if err != nil {
		t.Fatalf("create first config: %v", err)
	}
	second, err := createAIConfig(validTestAIConfig("second"))
	if err != nil {
		t.Fatalf("create second config: %v", err)
	}
	third, err := createAIConfig(validTestAIConfig("third"))
	if err != nil {
		t.Fatalf("create third config: %v", err)
	}
	if _, err := setDefaultAIConfig(third.ID); err != nil {
		t.Fatalf("set third default: %v", err)
	}

	deleted, err := deleteAIConfig(third.ID)
	if err != nil || !deleted.Success {
		t.Fatalf("delete default config = %+v, %v", deleted, err)
	}
	var configs []*AIConfig
	if err := db.Dao.Order("id ASC").Find(&configs).Error; err != nil {
		t.Fatalf("list configs: %v", err)
	}
	resolved, ok := ResolveAIConfig(configs, 0)
	if !ok || resolved.ID != first.ID || resolved.ID == second.ID {
		t.Fatalf("stable fallback should be lowest remaining ID %d, got %+v", first.ID, resolved)
	}
}

func TestEnsureSingleDefaultAIConfigRepairsLegacyRows(t *testing.T) {
	withAIConfigTestDB(t)
	first := validTestAIConfig("first")
	first.IsDefault = true
	second := validTestAIConfig("second")
	second.IsDefault = true
	if err := db.Dao.Create(first).Error; err != nil {
		t.Fatalf("create first legacy row: %v", err)
	}
	if err := db.Dao.Create(second).Error; err != nil {
		t.Fatalf("create second legacy row: %v", err)
	}
	if err := ensureSingleDefaultAIConfig(db.Dao); err != nil {
		t.Fatalf("ensureSingleDefaultAIConfig() error = %v", err)
	}
	var configs []*AIConfig
	if err := db.Dao.Order("id ASC").Find(&configs).Error; err != nil {
		t.Fatalf("list configs: %v", err)
	}
	if !configs[0].IsDefault || configs[1].IsDefault {
		t.Fatalf("legacy defaults were not normalized stably: %+v", configs)
	}
}

func TestSetDefaultAIConfigRejectsInvalidIDs(t *testing.T) {
	withAIConfigTestDB(t)
	if _, err := setDefaultAIConfig(0); err == nil {
		t.Fatal("zero default ID should fail")
	}
	if _, err := setDefaultAIConfig(999); err == nil {
		t.Fatal("missing default ID should fail")
	}
}

func TestSetDefaultAIConfigConcurrentCallsKeepOneDefault(t *testing.T) {
	withAIConfigTestDB(t)
	first, err := createAIConfig(validTestAIConfig("first"))
	if err != nil {
		t.Fatalf("create first config: %v", err)
	}
	second, err := createAIConfig(validTestAIConfig("second"))
	if err != nil {
		t.Fatalf("create second config: %v", err)
	}

	ids := []uint{first.ID, second.ID, first.ID, second.ID}
	errors := make(chan error, len(ids))
	var waitGroup sync.WaitGroup
	for _, id := range ids {
		waitGroup.Add(1)
		go func(configID uint) {
			defer waitGroup.Done()
			_, setErr := setDefaultAIConfig(configID)
			errors <- setErr
		}(id)
	}
	waitGroup.Wait()
	close(errors)
	for setErr := range errors {
		if setErr != nil {
			t.Fatalf("concurrent setDefaultAIConfig() error = %v", setErr)
		}
	}

	var count int64
	if err := db.Dao.Model(&AIConfig{}).Where("is_default = ?", true).Count(&count).Error; err != nil {
		t.Fatalf("count defaults: %v", err)
	}
	if count != 1 {
		t.Fatalf("default count = %d, want 1", count)
	}
}

func TestNewDeepSeekOpenAiUsesExplicitThenDefaultConfig(t *testing.T) {
	withAIConfigTestDB(t)
	first, err := createAIConfig(validTestAIConfig("first"))
	if err != nil {
		t.Fatalf("create first config: %v", err)
	}
	secondConfig := validTestAIConfig("second")
	secondConfig.ModelName = "gpt-5.6-terra"
	second, err := createAIConfig(secondConfig)
	if err != nil {
		t.Fatalf("create second config: %v", err)
	}
	if _, err := setDefaultAIConfig(second.ID); err != nil {
		t.Fatalf("set second default: %v", err)
	}

	if client := NewDeepSeekOpenAi(context.Background(), int(first.ID)); client.Model != first.ModelName {
		t.Fatalf("explicit model = %q, want %q", client.Model, first.ModelName)
	}
	if client := NewDeepSeekOpenAi(context.Background(), 0); client.Model != second.ModelName {
		t.Fatalf("default model = %q, want %q", client.Model, second.ModelName)
	}
	if client := NewDeepSeekOpenAi(context.Background(), 999999); client.Model != second.ModelName {
		t.Fatalf("missing explicit ID should fall back to %q, got %q", second.ModelName, client.Model)
	}
}

func TestCopyAIConfigConcurrentNamesRemainUnique(t *testing.T) {
	withAIConfigTestDB(t)
	created, err := createAIConfig(validTestAIConfig("concurrent"))
	if err != nil {
		t.Fatalf("createAIConfig() error = %v", err)
	}

	const copies = 6
	names := make(chan string, copies)
	errors := make(chan error, copies)
	var waitGroup sync.WaitGroup
	for range copies {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			copied, copyErr := copyAIConfig(created.ID)
			if copyErr != nil {
				errors <- copyErr
				return
			}
			names <- copied.Name
		}()
	}
	waitGroup.Wait()
	close(errors)
	close(names)

	for copyErr := range errors {
		t.Fatalf("copyAIConfig() error = %v", copyErr)
	}
	seen := make(map[string]bool, copies)
	for name := range names {
		if seen[name] {
			t.Fatalf("duplicate copy name %q", name)
		}
		seen[name] = true
	}
	if len(seen) != copies {
		t.Fatalf("got %d unique names, want %d: %+v", len(seen), copies, seen)
	}
}

func TestAIConfigCRUDRejectsDuplicateNamesAndInvalidIDs(t *testing.T) {
	withAIConfigTestDB(t)
	created, err := createAIConfig(validTestAIConfig("Unique Name"))
	if err != nil {
		t.Fatalf("createAIConfig() error = %v", err)
	}
	if _, err := createAIConfig(validTestAIConfig("unique name")); err == nil {
		t.Fatal("case-insensitive duplicate name should fail")
	}
	createWithID := validTestAIConfig("create with id")
	createWithID.ID = 99
	if _, err := createAIConfig(createWithID); err == nil {
		t.Fatal("create with an existing ID should fail")
	}

	duplicateUpdate := validTestAIConfig("UNIQUE NAME")
	duplicateUpdate.ID = created.ID + 999
	if _, err := updateAIConfig(duplicateUpdate); err == nil {
		t.Fatal("duplicate update name should fail")
	}
	if _, err := updateAIConfig(validTestAIConfig("missing id")); err == nil {
		t.Fatal("update without ID should fail")
	}
	missingRow := validTestAIConfig("missing row")
	missingRow.ID = created.ID + 999
	if _, err := updateAIConfig(missingRow); err == nil {
		t.Fatal("update for a missing row should fail")
	}
	if _, err := copyAIConfig(0); err == nil {
		t.Fatal("copy without ID should fail")
	}
	if _, err := deleteAIConfig(0); err == nil {
		t.Fatal("delete without ID should fail")
	}
}

func TestDeleteAIConfigReturnsReferences(t *testing.T) {
	withAIConfigTestDB(t)
	created, err := createAIConfig(validTestAIConfig("referenced"))
	if err != nil {
		t.Fatalf("createAIConfig() error = %v", err)
	}
	if err := db.Dao.Create(&Settings{FeishuBotAiConfigId: int(created.ID)}).Error; err != nil {
		t.Fatalf("create settings: %v", err)
	}
	if err := db.Dao.Create(&models.CronTask{Name: "日报", CronExpr: "0 0 * * *", TaskType: "stock_analysis", Params: fmt.Sprintf(`{"aiConfigId":%d}`, created.ID)}).Error; err != nil {
		t.Fatalf("create cron task: %v", err)
	}

	result, err := deleteAIConfig(created.ID)
	if err != nil {
		t.Fatalf("delete referenced config should return structured result, got error: %v", err)
	}
	if result.Success || len(result.References) != 2 {
		t.Fatalf("unexpected delete result: %+v", result)
	}
	var count int64
	if err := db.Dao.Model(&AIConfig{}).Where("id = ?", created.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("referenced config was deleted: count=%d err=%v", count, err)
	}
}

func TestDeleteAIConfigFindsLegacyCronReferenceKey(t *testing.T) {
	withAIConfigTestDB(t)
	created, err := createAIConfig(validTestAIConfig("legacy-cron-reference"))
	if err != nil {
		t.Fatalf("createAIConfig() error = %v", err)
	}
	params := fmt.Sprintf(`{"ai_config_id":"%d"}`, created.ID)
	if err := db.Dao.Create(&models.CronTask{Name: "旧任务", CronExpr: "0 0 * * *", TaskType: "stock_analysis", Params: params}).Error; err != nil {
		t.Fatalf("create cron task: %v", err)
	}

	result, err := deleteAIConfig(created.ID)
	if err != nil {
		t.Fatalf("delete referenced config should return structured result, got error: %v", err)
	}
	if result.Success || len(result.References) != 1 || result.References[0].SourceName != "旧任务" {
		t.Fatalf("legacy cron reference was not reported: %+v", result)
	}
}

func TestDeleteAIConfigRejectsAmbiguousMalformedCronParams(t *testing.T) {
	withAIConfigTestDB(t)
	created, err := createAIConfig(validTestAIConfig("malformed"))
	if err != nil {
		t.Fatalf("createAIConfig() error = %v", err)
	}
	if err := db.Dao.Create(&models.CronTask{Name: "异常任务", CronExpr: "0 0 * * *", TaskType: "custom", Params: `{"ai_config_id":`}).Error; err != nil {
		t.Fatalf("create cron task: %v", err)
	}
	if _, err := deleteAIConfig(created.ID); err == nil {
		t.Fatal("malformed possible reference must prevent deletion")
	}
}
