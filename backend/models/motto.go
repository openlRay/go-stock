package models

import "time"

// Motto 是用户维护的格言正文及其持久化时间信息。
type Motto struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Content   string    `json:"content" gorm:"type:text;not null"`
}

// TableName 固定格言表名，避免未来类型重命名改变持久化契约。
func (Motto) TableName() string {
	return "mottos"
}
