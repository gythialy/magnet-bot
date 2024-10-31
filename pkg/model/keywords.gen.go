// Code generated by gorm.io/gen. DO NOT EDIT.
// Code generated by gorm.io/gen. DO NOT EDIT.
// Code generated by gorm.io/gen. DO NOT EDIT.

package model

import (
	"time"

	"gorm.io/gorm"
)

const TableNameKeyword = "keywords"

// Keyword mapped from table <keywords>
type Keyword struct {
	ID        *int32         `gorm:"column:id;type:INTEGER" json:"id"`
	CreatedAt time.Time      `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt *time.Time     `gorm:"column:updated_at" json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index:idx_keywords_deleted_at,priority:1" json:"deletedAt"`
	Keyword   string         `gorm:"column:keyword;not null" json:"keyword"`
	UserID    int64          `gorm:"column:user_id;not null" json:"userId"`
	Type      int32          `gorm:"column:type;not null" json:"type"`
	Counter   int32          `gorm:"column:counter;not null" json:"counter"`
}

// TableName Keyword's table name
func (*Keyword) TableName() string {
	return TableNameKeyword
}