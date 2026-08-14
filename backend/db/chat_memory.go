package db

import (
	"fmt"
	"go-stock/backend/models"
	"time"

	"gorm.io/gorm"
)

type ChatMemory struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	SessionID string    `gorm:"index;size:64" json:"sessionId"`
	Role      string    `gorm:"size:20" json:"role"`
	Content   string    `gorm:"type:text" json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

func (ChatMemory) TableName() string {
	return "chat_memory"
}

func (c *ChatMemory) Save() error {
	return Dao.Create(c).Error
}

func GetChatMemoryList(sessionID string, limit int) ([]ChatMemory, error) {
	var memories []ChatMemory
	err := Dao.Where("session_id = ?", sessionID).
		Order("created_at DESC").
		Limit(limit).
		Find(&memories).Error
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(memories)-1; i < j; i, j = i+1, j-1 {
		memories[i], memories[j] = memories[j], memories[i]
	}
	return memories, nil
}

func GetRecentChatMemory(sessionID string, limit int) ([]ChatMemory, error) {
	var memories []ChatMemory
	var err error
	if sessionID == "" {
		err = Dao.Order("created_at DESC").
			Limit(limit).
			Find(&memories).Error
	} else {
		err = Dao.Where("session_id = ?", sessionID).
			Order("created_at DESC").
			Limit(limit).
			Find(&memories).Error
	}
	for i, j := 0, len(memories)-1; i < j; i, j = i+1, j-1 {
		memories[i], memories[j] = memories[j], memories[i]
	}
	return memories, err
}

func ClearChatMemory(sessionID string) error {
	return Dao.Where("session_id = ?", sessionID).Delete(&ChatMemory{}).Error
}

func AutoMigrate() {
	Dao.AutoMigrate(&ChatMemory{})
	Dao.AutoMigrate(&models.StockChangeHistory{})
	Dao.AutoMigrate(&models.MarketStatistic{})
	Dao.AutoMigrate(&models.StockTransactionCache{})
	Dao.AutoMigrate(&models.StockTransactionCacheMeta{})
	if err := migrateAIConfigDefault(); err != nil {
		fmt.Printf("migrate default AI config failed: %v\n", err)
	}
}

type aiConfigDefaultMigration struct {
	ID        uint `gorm:"primarykey"`
	IsDefault bool `gorm:"column:is_default;not null;default:false"`
}

func (aiConfigDefaultMigration) TableName() string {
	return "ai_config"
}

// migrateAIConfigDefault runs synchronously during db.Init so all later
// settings reads see the new column. It also repairs legacy zero/multiple
// defaults once at startup, keeping the lowest stable ID.
func migrateAIConfigDefault() error {
	if err := Dao.AutoMigrate(&aiConfigDefaultMigration{}); err != nil {
		return err
	}
	return Dao.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE ai_config
			SET is_default = FALSE
			WHERE is_default = TRUE
			  AND id <> (SELECT id FROM ai_config WHERE is_default = TRUE ORDER BY id ASC LIMIT 1)`).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE ai_config
			SET is_default = TRUE
			WHERE id = (SELECT id FROM ai_config ORDER BY id ASC LIMIT 1)
			  AND NOT EXISTS (SELECT 1 FROM ai_config WHERE is_default = TRUE)`).Error
	})
}
