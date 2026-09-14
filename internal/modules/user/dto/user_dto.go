package dto

import (
	"encoding/json"
	"time"

	"github.com/4jawaly/identity-gateway/internal/modules/user/models"
	"github.com/4jawaly/identity-gateway/internal/response"
)

// UserResponse is the public view of a users row: the columns plus the
// identity fields decoded from the snapshot. A placeholder row (parent
// referenced by a sub-account that never logged in) has synced_at null.
type UserResponse struct {
	Sub           string             `json:"sub" example:"355646255235338042"`
	Email         string             `json:"email" example:"admin@rawazin.example"`
	Nickname      string             `json:"nickname" example:"شركة روازن الدولية"`
	Roles         []string           `json:"roles" example:"ADMIN"`
	IsAdmin       bool               `json:"is_admin" example:"true"`
	MainAccount   MainAccountPayload `json:"main_account"`
	IsSubAccount  bool               `json:"is_sub_account" example:"false"`
	SubAccountOf  []string           `json:"sub_account_of" example:"355646368934527195"`
	IsPlaceholder bool               `json:"is_placeholder" example:"false"`
	SyncedAt      *time.Time         `json:"synced_at" example:"2026-09-14T10:00:00Z"`
}

// ListUsersQuery is the query string of GET /users.
type ListUsersQuery struct {
	Page    int `form:"page,default=1" binding:"min=1" example:"1"`
	PerPage int `form:"per_page,default=20" binding:"min=1,max=100" example:"20"`
}

// UserFromModel maps a row to its public view.
func UserFromModel(u *models.User) UserResponse {
	var snap UserSyncPayload
	if len(u.Snapshot) > 0 {
		_ = json.Unmarshal(u.Snapshot, &snap)
	}
	if snap.Roles == nil {
		snap.Roles = []string{}
	}
	if snap.SubAccountOf == nil {
		snap.SubAccountOf = []string{}
	}
	return UserResponse{
		Sub: u.Sub, Email: deref(u.Email), Nickname: deref(u.Nickname),
		Roles: snap.Roles, IsAdmin: snap.IsAdmin, MainAccount: snap.MainAccount,
		IsSubAccount: len(snap.SubAccountOf) > 0, SubAccountOf: snap.SubAccountOf,
		IsPlaceholder: u.IsPlaceholder(), SyncedAt: u.SyncedAt,
	}
}

// UsersFromModels maps a slice of rows.
func UsersFromModels(rows []models.User) []UserResponse {
	return response.MapRef(rows, UserFromModel)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
