package agent

import (
	"context"
	"errors"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type fakeMottoModel struct {
	response string
	err      error
}

func (f *fakeMottoModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &schema.Message{Role: schema.Assistant, Content: f.response}, nil
}

func (f *fakeMottoModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeMottoModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

func withMottoTestDB(t *testing.T) {
	t.Helper()
	previous := db.Dao
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := database.AutoMigrate(&models.Motto{}, &data.Settings{}, &data.AIConfig{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	db.Dao = database
	t.Cleanup(func() { db.Dao = previous })
}

func TestMottoCRUDAndValidation(t *testing.T) {
	withMottoTestDB(t)
	api := NewMottoApi()

	if _, err := api.Create("   "); !errors.Is(err, ErrMottoContentEmpty) {
		t.Fatalf("Create(empty) error = %v", err)
	}
	if _, err := api.Create(strings.Repeat("格", maxMottoRunes+1)); !errors.Is(err, ErrMottoContentTooLong) {
		t.Fatalf("Create(overlong) error = %v", err)
	}

	first, err := api.Create("  保持耐心  ")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	second, err := api.Create("独立思考")
	if err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	if first.Content != "保持耐心" || first.ID == 0 || second.ID == 0 {
		t.Fatalf("created mottos = %+v, %+v", first, second)
	}

	updated, err := api.Update(first.ID, "  长期主义  ")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Content != "长期主义" {
		t.Fatalf("updated content = %q", updated.Content)
	}
	if _, err := api.Update(99999, "不存在"); !errors.Is(err, ErrMottoNotFound) {
		t.Fatalf("Update(missing) error = %v", err)
	}

	list, err := api.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 || list[0].ID != first.ID {
		t.Fatalf("List() = %+v, want updated record first", list)
	}

	if err := api.Delete(second.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := api.Delete(second.ID); !errors.Is(err, ErrMottoNotFound) {
		t.Fatalf("Delete(missing) error = %v", err)
	}
}

func TestMottoRandomReturnsAtMostThreeUniqueRows(t *testing.T) {
	withMottoTestDB(t)
	api := NewMottoApi()

	rows, err := api.Random(3)
	if err != nil || len(rows) != 0 {
		t.Fatalf("Random(empty) = %+v, %v", rows, err)
	}
	for i := 1; i <= 4; i++ {
		if _, err := api.Create(fmt.Sprintf("格言 %d", i)); err != nil {
			t.Fatalf("seed motto %d: %v", i, err)
		}
		rows, err = api.Random(3)
		if err != nil {
			t.Fatalf("Random(%d rows) error = %v", i, err)
		}
		want := i
		if want > 3 {
			want = 3
		}
		if len(rows) != want {
			t.Fatalf("Random after %d rows returned %d, want %d", i, len(rows), want)
		}
		seen := map[uint]bool{}
		for _, row := range rows {
			if seen[row.ID] {
				t.Fatalf("Random returned duplicate ID %d", row.ID)
			}
			seen[row.ID] = true
		}
	}
}

func TestMottoPolishReturnsDraftWithoutWriting(t *testing.T) {
	withMottoTestDB(t)
	if err := db.Dao.Create(&data.Settings{}).Error; err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	config := &data.AIConfig{
		Name: "test", BaseUrl: "http://localhost:8317/v1", ApiKey: "secret", ModelName: "test-chat",
		ModelType: "chat", ReasoningMode: data.ReasoningModeOn, Thinking: true,
	}
	if err := db.Dao.Create(config).Error; err != nil {
		t.Fatalf("seed AI config: %v", err)
	}

	api := NewMottoApi()
	var captured data.AIConfig
	api.modelFactory = func(_ context.Context, cfg data.AIConfig) (model.ToolCallingChatModel, error) {
		captured = cfg
		return &fakeMottoModel{response: "```text\n慢一点，才能看见更远的风景。\n```"}, nil
	}
	result, err := api.Polish(context.Background(), MottoPolishRequest{Content: "慢一点 看得更远", AIConfigID: int(config.ID)})
	if err != nil {
		t.Fatalf("Polish() error = %v", err)
	}
	if result.Content != "慢一点，才能看见更远的风景。" {
		t.Fatalf("Polish() content = %q", result.Content)
	}
	if captured.Thinking || captured.ReasoningMode != data.ReasoningModeOff {
		t.Fatalf("request config should disable thinking: %+v", captured)
	}
	var count int64
	if err := db.Dao.Model(&models.Motto{}).Count(&count).Error; err != nil {
		t.Fatalf("count mottos: %v", err)
	}
	if count != 0 {
		t.Fatalf("Polish() wrote %d motto rows; want 0", count)
	}
}

func TestMottoPolishRejectsModelFailureAndEmptyResponse(t *testing.T) {
	withMottoTestDB(t)
	if err := db.Dao.Create(&data.Settings{}).Error; err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	config := &data.AIConfig{Name: "test", BaseUrl: "http://localhost:8317/v1", ApiKey: "secret", ModelName: "test-chat", ModelType: "chat"}
	if err := db.Dao.Create(config).Error; err != nil {
		t.Fatalf("seed AI config: %v", err)
	}

	api := NewMottoApi()
	if _, err := api.Polish(context.Background(), MottoPolishRequest{Content: "原文", AIConfigID: 99999}); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("Polish(missing config) error = %v", err)
	}
	api.modelFactory = func(context.Context, data.AIConfig) (model.ToolCallingChatModel, error) {
		return &fakeMottoModel{err: errors.New("provider secret response")}, nil
	}
	if _, err := api.Polish(context.Background(), MottoPolishRequest{Content: "原文", AIConfigID: int(config.ID)}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("Polish(model error) should return safe error, got %v", err)
	}

	api.modelFactory = func(context.Context, data.AIConfig) (model.ToolCallingChatModel, error) {
		return &fakeMottoModel{response: "   "}, nil
	}
	if _, err := api.Polish(context.Background(), MottoPolishRequest{Content: "原文", AIConfigID: int(config.ID)}); err == nil {
		t.Fatal("Polish(empty response) should fail")
	}
}
