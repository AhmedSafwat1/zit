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

	"github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	"github.com/4jawaly/identity-gateway/internal/modules/user/models"
	"github.com/4jawaly/identity-gateway/internal/modules/user/services"
)

// UserRepository implements services.UserRepository on GORM. Writes are raw
// SQL on purpose: conflict targets and arrays fight the query builder.
type UserRepository struct{ db *gorm.DB }

// NewUserRepository builds the repository.
func NewUserRepository(db *gorm.DB) *UserRepository { return &UserRepository{db: db} }

// WithTx returns a repository bound to an existing transaction.
func (r *UserRepository) WithTx(tx *gorm.DB) *UserRepository { return &UserRepository{db: tx} }

// Transaction runs fn inside one database transaction.
func (r *UserRepository) Transaction(ctx context.Context, fn func(services.UserRepository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(r.WithTx(tx))
	})
}

// GetBySub loads one user.
func (r *UserRepository) GetBySub(ctx context.Context, sub string) (*models.User, error) {
	var u models.User
	if err := r.db.WithContext(ctx).Where("sub = ?", sub).Take(&u).Error; err != nil {
		return nil, fmt.Errorf("get user %s: %w", sub, err)
	}
	return &u, nil
}

// List pages through users ordered by id.
func (r *UserRepository) List(ctx context.Context, offset, limit int) ([]models.User, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&models.User{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}
	var rows []models.User
	if err := r.db.WithContext(ctx).Order("id").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	return rows, total, nil
}

// ListSubAccounts returns the children of parentSub through sub_accounts.
func (r *UserRepository) ListSubAccounts(ctx context.Context, parentSub string) ([]models.User, error) {
	var rows []models.User
	err := r.db.WithContext(ctx).
		Joins("JOIN sub_accounts sa ON sa.child_user_id = users.id").
		Joins("JOIN users p ON p.id = sa.parent_user_id").
		Where("p.sub = ?", parentSub).
		Order("users.id").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list sub accounts of %s: %w", parentSub, err)
	}
	return rows, nil
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

// UpsertUser implements services.UserRepository.
func (r *UserRepository) UpsertUser(ctx context.Context, p dto.UserSyncPayload, snapshot []byte, hash string, syncedAt time.Time) (bool, error) {
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

// EnsureUsers implements services.UserRepository.
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

// ReconcileSubAccounts implements services.UserRepository.
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

var _ services.UserRepository = (*UserRepository)(nil)
