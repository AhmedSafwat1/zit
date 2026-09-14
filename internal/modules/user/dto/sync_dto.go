package dto

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// MainAccountPayload mirrors MainAccount plus the account type.
type MainAccountPayload struct {
	Allowed          bool   `json:"allowed"`
	SubAccountsCount int    `json:"sub_accounts_count"`
	AccountID        string `json:"account_id"`
	AccountTypeName  string `json:"account_type_name"`
	StatusName       string `json:"status_name"`
}

// UserSyncPayload is the Data of a user.sync envelope. Envelope fields
// (event id, occurred_at) are deliberately absent so Hash is stable.
type UserSyncPayload struct {
	Sub           string             `json:"sub"`
	Email         string             `json:"email"`
	Nickname      string             `json:"nickname"`
	EmailVerified bool               `json:"email_verified"`
	Issuer        string             `json:"issuer"`
	Roles         []string           `json:"roles"`
	IsAdmin       bool               `json:"is_admin"`
	MainAccount   MainAccountPayload `json:"main_account"`
	SubAccountOf  []string           `json:"sub_account_of"`
	Origin        json.RawMessage    `json:"origin,omitempty"`
}

// Hash is sha256 over sub | email | nickname | sorted(roles) |
// sorted(sub_account_of) | status_name | sub_accounts_count. It is the
// dedupe key for sync:seen:<sub>.
func (p *UserSyncPayload) Hash() string {
	roles := append([]string(nil), p.Roles...)
	sort.Strings(roles)
	parents := append([]string(nil), p.SubAccountOf...)
	sort.Strings(parents)
	parts := []string{
		p.Sub, p.Email, p.Nickname,
		strings.Join(roles, ","),
		strings.Join(parents, ","),
		p.MainAccount.StatusName,
		strconv.Itoa(p.MainAccount.SubAccountsCount),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

// IsMain reports whether this account owns sub-accounts.
func (p *UserSyncPayload) IsMain() bool {
	return p.MainAccount.Allowed || p.MainAccount.SubAccountsCount > 0
}
