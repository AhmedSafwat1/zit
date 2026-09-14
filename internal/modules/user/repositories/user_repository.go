// Package repositories holds the Postgres implementations of the
// repository interfaces declared by services. Import direction:
// repositories -> services (interface types) -> models / dto.
package repositories

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	userdto "github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	usermodel "github.com/4jawaly/identity-gateway/internal/modules/user/models"
	usersvc "github.com/4jawaly/identity-gateway/internal/modules/user/services"
)

// UserRepository implements usersvc.UserRepository on GORM. Writes are raw
// SQL on purpose: conflict targets and arrays fight the query builder.
type UserRepository struct{ db *gorm.DB }

// NewUserRepository builds the repository.
func NewUserRepository(db *gorm.DB) *UserRepository { return &UserRepository{db: db} }

// WithTx returns a repository bound to an existing transaction.
func (r *UserRepository) WithTx(tx *gorm.DB) *UserRepository { return &UserRepository{db: tx} }

// Transaction runs fn inside one database transaction.
func (r *UserRepository) Transaction(ctx context.Context, fn func(usersvc.UserRepository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(r.WithTx(tx))
	})
}

// GetBySub loads one user.
func (r *UserRepository) GetBySub(ctx context.Context, sub string) (*usermodel.User, error) {
	var u usermodel.User
	if err := r.db.WithContext(ctx).Where("sub = ?", sub).Take(&u).Error; err != nil {
		return nil, fmt.Errorf("get user %s: %w", sub, err)
	}
	return &u, nil
}

// upsertUserSQL writes a row only when the message is newer than what is
// stored (stale guard = idempotency) AND the identity actually changed
// (hash guard = no write for repeated identical logins). A placeholder row
// (synced_at IS NULL) always accepts its first real sync.
const upsertUserSQL = `
INSERT INTO users (sub, email, nickname, snapshot, identity_hash, synced_at, created_at, updated_at)
VALUES (?, NULLIF(?, ''), NULLIF(?, ''), ?::jsonb, ?, ?, now(), now())
ON CONFLICT (sub) DO UPDATE SET
    email         = COALESCE(EXCLUDED.email, users.email),
    nickname      = COALESCE(EXCLUDED.nickname, users.nickname),
    snapshot      = EXCLUDED.snapshot,
    identity_hash = EXCLUDED.identity_hash,
    synced_at     = EXCLUDED.synced_at,
    updated_at    = now()
WHERE (users.synced_at IS NULL OR users.synced_at < EXCLUDED.synced_at)
  AND users.identity_hash IS DISTINCT FROM EXCLUDED.identity_hash`

// UpsertUser implements usersvc.UserRepository.
func (r *UserRepository) UpsertUser(ctx context.Context, p userdto.UserSyncPayload, snapshot []byte, hash string, syncedAt time.Time) (bool, error) {
	res := r.db.WithContext(ctx).Exec(upsertUserSQL,
		p.Sub, p.Email, p.Nickname, string(snapshot), hash, syncedAt.UTC(),
	)
	if res.Error != nil {
		return false, fmt.Errorf("upsert user %s: %w", p.Sub, res.Error)
	}
	return res.RowsAffected > 0, nil
}

// ensureUsersSQL creates sub-only rows so a relation can point at a user
// that has never logged in.
const ensureUsersSQL = `
INSERT INTO users (sub, created_at, updated_at)
SELECT s, now(), now() FROM unnest(?::text[]) AS s
ON CONFLICT (sub) DO NOTHING`

// EnsureUsers implements usersvc.UserRepository.
func (r *UserRepository) EnsureUsers(ctx context.Context, subs []string) error {
	if len(subs) == 0 {
		return nil
	}
	if err := r.db.WithContext(ctx).Exec(ensureUsersSQL, textArray(subs)).Error; err != nil {
		return fmt.Errorf("ensure users: %w", err)
	}
	return nil
}

const insertEdgesSQL = `
INSERT INTO sub_accounts (parent_user_id, child_user_id, created_at)
SELECT p.id, c.id, now()
FROM users c
JOIN users p ON p.sub = ANY(?::text[]) AND p.sub <> c.sub
WHERE c.sub = ?
ON CONFLICT DO NOTHING`

const deleteEdgesSQL = `
DELETE FROM sub_accounts sa
USING users c
WHERE sa.child_user_id = c.id
  AND c.sub = ?
  AND sa.parent_user_id NOT IN (SELECT id FROM users WHERE sub = ANY(?::text[]))`

// ReconcileSubAccounts implements usersvc.UserRepository.
func (r *UserRepository) ReconcileSubAccounts(ctx context.Context, childSub string, parentSubs []string) error {
	db := r.db.WithContext(ctx)
	if len(parentSubs) > 0 {
		if err := db.Exec(insertEdgesSQL, textArray(parentSubs), childSub).Error; err != nil {
			return fmt.Errorf("insert sub accounts of %s: %w", childSub, err)
		}
	}
	if err := db.Exec(deleteEdgesSQL, childSub, textArray(parentSubs)).Error; err != nil {
		return fmt.Errorf("delete sub accounts of %s: %w", childSub, err)
	}
	return nil
}

// textArray renders a []string as a Postgres array literal. GORM expands a
// plain slice into "(?, ?)", which is wrong for a text[] parameter; a
// driver.Valuer is passed through untouched.
type textArray []string

// Value implements driver.Valuer.
func (a textArray) Value() (driver.Value, error) {
	if len(a) == 0 {
		return "{}", nil
	}
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	var b strings.Builder
	b.WriteByte('{')
	for i, s := range a {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(esc.Replace(s))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String(), nil
}

var _ usersvc.UserRepository = (*UserRepository)(nil)
