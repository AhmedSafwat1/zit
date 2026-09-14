package models

import "time"

// SubAccount is one parent -> child edge between two users.
type SubAccount struct {
	ParentUserID int64     `gorm:"column:parent_user_id;primaryKey"`
	ChildUserID  int64     `gorm:"column:child_user_id;primaryKey"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

// TableName implements gorm's Tabler.
func (SubAccount) TableName() string { return "sub_accounts" }
