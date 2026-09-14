package dto

import (
	"strings"

	"github.com/4jawaly/identity-gateway/pkg/zitadel"
)

// ReportFromClaims builds the introspection report from decoded claims.
func ReportFromClaims(c *zitadel.Claims) *IntrospectResponse {
	roles := c.Roles
	if roles == nil {
		roles = []string{}
	}
	parents := c.SubAccountOf
	if parents == nil {
		parents = []string{}
	}
	return &IntrospectResponse{
		OK:       true,
		Identity: Identity{Sub: c.Sub, Email: c.Email, Nickname: c.Nickname},
		Roles:    roles,
		IsAdmin:  c.HasRole(AdminRole),
		MainAccount: MainAccount{
			Allowed:          c.MetaBool(zitadel.MetaAddSubAccountAllowed),
			SubAccountsCount: c.MetaInt(zitadel.MetaSubAccountsCount),
			AccountID:        strings.TrimSpace(c.Meta(zitadel.MetaAccountID)),
			StatusName:       strings.TrimSpace(c.Meta(zitadel.MetaStatusName)),
		},
		IsSubAccount: len(parents) > 0,
		SubAccountOf: parents,
	}
}

// SyncPayloadFromClaims builds the queue payload from decoded claims and the
// already-built report.
func SyncPayloadFromClaims(c *zitadel.Claims, r *IntrospectResponse, issuer string) UserSyncPayload {
	return UserSyncPayload{
		Sub:           c.Sub,
		Email:         c.Email,
		Nickname:      c.Nickname,
		EmailVerified: c.EmailVerified,
		Issuer:        issuer,
		Roles:         append([]string(nil), r.Roles...),
		IsAdmin:       r.IsAdmin,
		MainAccount: MainAccountPayload{
			Allowed:          r.MainAccount.Allowed,
			SubAccountsCount: r.MainAccount.SubAccountsCount,
			AccountID:        r.MainAccount.AccountID,
			AccountTypeName:  strings.TrimSpace(c.Meta(zitadel.MetaAccountTypeName)),
			StatusName:       r.MainAccount.StatusName,
		},
		SubAccountOf: append([]string(nil), r.SubAccountOf...),
		Origin:       c.Raw,
	}
}
