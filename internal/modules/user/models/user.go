// Package models holds the GORM row types. Schema lives in migrations/;
// these tags are for reads and writes only, never AutoMigrate.
package models

import (
	"time"

	"gorm.io/datatypes"
)

// User is a row of the users table. Only Sub is guaranteed: a row created
// as the parent of a sub-account has every other field NULL until that
// user authenticates.
type User struct {
	ID           int64          `gorm:"column:id;primaryKey"`
	Sub          string         `gorm:"column:sub"`
	Email        *string        `gorm:"column:email"`
	Nickname     *string        `gorm:"column:nickname"`
	Snapshot     datatypes.JSON `gorm:"column:snapshot"`
	IdentityHash *string        `gorm:"column:identity_hash"`
	SyncedAt     *time.Time     `gorm:"column:synced_at"`
	CreatedAt    time.Time      `gorm:"column:created_at"`
	UpdatedAt    time.Time      `gorm:"column:updated_at"`
}

// TableName implements gorm's Tabler.
func (User) TableName() string { return "users" }

// IsPlaceholder reports whether the row was created as a parent reference
// and has never been synced itself.
func (u *User) IsPlaceholder() bool { return u.SyncedAt == nil }
