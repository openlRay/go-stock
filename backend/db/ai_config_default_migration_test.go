package db

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateAIConfigDefaultAddsColumnAndRepairsLegacyRows(t *testing.T) {
	previous := Dao
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	Dao = database
	t.Cleanup(func() { Dao = previous })

	if err := Dao.Exec(`CREATE TABLE ai_config (id integer PRIMARY KEY AUTOINCREMENT, name text)`).Error; err != nil {
		t.Fatalf("create legacy ai_config table: %v", err)
	}
	if err := Dao.Exec(`INSERT INTO ai_config (name) VALUES ('first'), ('second')`).Error; err != nil {
		t.Fatalf("insert legacy configs: %v", err)
	}
	if err := migrateAIConfigDefault(); err != nil {
		t.Fatalf("migrateAIConfigDefault() error = %v", err)
	}
	if !Dao.Migrator().HasColumn(&aiConfigDefaultMigration{}, "IsDefault") {
		t.Fatal("is_default column was not added")
	}

	var defaults []aiConfigDefaultMigration
	if err := Dao.Where("is_default = ?", true).Order("id ASC").Find(&defaults).Error; err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	if len(defaults) != 1 || defaults[0].ID != 1 {
		t.Fatalf("defaults = %+v, want only lowest ID 1", defaults)
	}

	if err := Dao.Model(&aiConfigDefaultMigration{}).Where("id IN ?", []uint{1, 2}).Update("is_default", true).Error; err != nil {
		t.Fatalf("seed duplicate defaults: %v", err)
	}
	if err := migrateAIConfigDefault(); err != nil {
		t.Fatalf("second migrateAIConfigDefault() error = %v", err)
	}
	defaults = nil
	if err := Dao.Where("is_default = ?", true).Order("id ASC").Find(&defaults).Error; err != nil {
		t.Fatalf("read repaired defaults: %v", err)
	}
	if len(defaults) != 1 || defaults[0].ID != 1 {
		t.Fatalf("repaired defaults = %+v, want only lowest ID 1", defaults)
	}
}

func TestMigrateAIConfigDefaultSkipsEmbeddingRows(t *testing.T) {
	previous := Dao
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	Dao = database
	t.Cleanup(func() { Dao = previous })

	if err := Dao.AutoMigrate(&aiConfigDefaultMigration{}); err != nil {
		t.Fatalf("migrate test table: %v", err)
	}
	if err := Dao.Create(&aiConfigDefaultMigration{ModelType: "embedding", IsDefault: true}).Error; err != nil {
		t.Fatalf("create embedding config: %v", err)
	}
	if err := Dao.Create(&aiConfigDefaultMigration{ModelType: "chat"}).Error; err != nil {
		t.Fatalf("create chat config: %v", err)
	}
	if err := migrateAIConfigDefault(); err != nil {
		t.Fatalf("migrateAIConfigDefault() error = %v", err)
	}

	var defaults []aiConfigDefaultMigration
	if err := Dao.Where("is_default = ?", true).Find(&defaults).Error; err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	if len(defaults) != 1 || defaults[0].ModelType != "chat" {
		t.Fatalf("defaults = %+v, want only chat config", defaults)
	}
}
