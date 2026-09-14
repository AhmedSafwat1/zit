// Package dto holds the wire types of the user module: API responses, the
// queue payload and the Zitadel-derived report.
package dto

// Identity is the minimal identity block of an introspection report.
type Identity struct {
	Sub      string `json:"sub" example:"355646255235338042"`
	Email    string `json:"email" example:"admin@rawazin.example"`
	Nickname string `json:"nickname" example:"شركة روازن الدولية"`
}

// MainAccount describes the account-level metadata.
type MainAccount struct {
	Allowed          bool   `json:"allowed" example:"true"`
	SubAccountsCount int    `json:"sub_accounts_count" example:"34"`
	AccountID        string `json:"account_id" example:"1"`
	StatusName       string `json:"status_name" example:"Active"`
}

// IntrospectResponse is the identity report returned by POST /users/introspect
// and the document cached under introspect:<hash>.
type IntrospectResponse struct {
	OK           bool        `json:"ok" example:"true"`
	Identity     Identity    `json:"identity"`
	Roles        []string    `json:"roles" example:"ADMIN,jawaly_ai_super_admin"`
	IsAdmin      bool        `json:"is_admin" example:"true"`
	MainAccount  MainAccount `json:"main_account"`
	IsSubAccount bool        `json:"is_sub_account" example:"true"`
	SubAccountOf []string    `json:"sub_account_of" example:"355646368934527195,355646755028599003"`
}

// IsActive reports whether the account may use the API.
func (r *IntrospectResponse) IsActive() bool { return r.MainAccount.StatusName == StatusActive }

// StatusActive is the only status_name that is allowed through.
const StatusActive = "Active"

// AdminRole is the role name that sets is_admin.
const AdminRole = "ADMIN"
