// Package services holds the business logic of the user module. It declares
// the interfaces it consumes; providers return concrete types.
package services

import (
	"context"
	"time"

	"github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	"github.com/4jawaly/identity-gateway/internal/modules/user/models"
	"github.com/4jawaly/identity-gateway/pkg/cache"
	"github.com/4jawaly/identity-gateway/pkg/zitadel"
)

// ZitadelClient fetches and decodes userinfo. Errors are already catalog AppErrors.
type ZitadelClient interface {
	UserInfo(ctx context.Context, token string) (*zitadel.Claims, error)
}

// SyncNotifier hands a fresh identity to the async sync pipeline.
type SyncNotifier interface {
	Notify(ctx context.Context, p dto.UserSyncPayload) error
}

// State is the outcome of an introspection cache lookup.
type State int

const (
	StateMiss     State = iota // nothing cached, call upstream
	StateHit                   // a report is cached
	StateInactive              // upstream said 400/401 recently; do not call it again
)

func (s State) String() string {
	switch s {
	case StateHit:
		return "hit"
	case StateInactive:
		return "inactive"
	default:
		return "miss"
	}
}

// Cache is the hot-path cache as the service needs it. Keys are already
// hashed; never pass a raw token. A backend error is never fatal: the
// implementation returns StateMiss plus the error.
type Cache interface {
	LookupIntrospect(ctx context.Context, hash string) (report []byte, state State, err error)
	SetIntrospect(ctx context.Context, hash string, report []byte, ttl time.Duration) error
	MarkInactive(ctx context.Context, hash string, ttl time.Duration) error
	GetUser(ctx context.Context, sub string) ([]byte, error)
	SetUser(ctx context.Context, sub string, data []byte, ttl time.Duration) error
}

// TokenHash derives the cache key material from a raw bearer token. It is
// the only function in the module that may see a raw token.
func TokenHash(token string) string { return cache.SHA256Hex(token) }

// UserRepository is the Postgres access of the module. Reads return
// gorm.ErrRecordNotFound for a missing row. Writes are meant to be composed
// inside Transaction, which hands back a repository bound to the tx.
type UserRepository interface {
	GetBySub(ctx context.Context, sub string) (*models.User, error)
	List(ctx context.Context, offset, limit int) ([]models.User, int64, error)
	ListSubAccounts(ctx context.Context, parentSub string) ([]models.User, error)

	// UpsertUser writes the row and reports whether anything was written
	// (false when the stale guard or the hash guard rejected it).
	UpsertUser(ctx context.Context, p dto.UserSyncPayload, snapshot []byte, hash string, syncedAt time.Time) (bool, error)
	// EnsureUsers creates sub-only rows for every sub that does not exist yet.
	EnsureUsers(ctx context.Context, subs []string) error
	// ReconcileSubAccounts makes parentSubs the exact parent set of childSub.
	ReconcileSubAccounts(ctx context.Context, childSub string, parentSubs []string) error

	// Transaction runs fn atomically. fn receives a repository bound to the
	// transaction; a non-nil error rolls back.
	Transaction(ctx context.Context, fn func(UserRepository) error) error
}
