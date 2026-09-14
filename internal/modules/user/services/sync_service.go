package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	userdto "github.com/4jawaly/identity-gateway/internal/modules/user/dto"
)

// SyncService is the worker-side writer.
type SyncService struct {
	repo UserRepository
	log  zerolog.Logger
}

// SyncOption customises NewSyncService.
type SyncOption func(*SyncService)

// WithSyncLogger sets the logger.
func WithSyncLogger(l zerolog.Logger) SyncOption { return func(s *SyncService) { s.log = l } }

// NewSyncService builds the service.
func NewSyncService(repo UserRepository, opts ...SyncOption) *SyncService {
	s := &SyncService{repo: repo, log: zerolog.Nop()}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Sync applies one user.sync payload in a single transaction:
//
//  1. upsert the user (stale + hash guard)
//  2. create missing parent rows from sub_account_of (one batched insert)
//  3. reconcile sub_accounts edges for this child
//
// Steps 2-3 run only when step 1 wrote, so a stale or unchanged message is
// a complete no-op.
func (s *SyncService) Sync(ctx context.Context, p userdto.UserSyncPayload, occurredAt time.Time) error {
	if p.Sub == "" {
		return fmt.Errorf("sync: empty sub")
	}
	if p.Roles == nil {
		p.Roles = []string{}
	}
	parents := dedupe(p.SubAccountOf, p.Sub)
	p.SubAccountOf = parents
	snapshot, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("sync: encode snapshot: %w", err)
	}

	return s.repo.Transaction(ctx, func(tx UserRepository) error {
		written, err := tx.UpsertUser(ctx, p, snapshot, p.Hash(), occurredAt)
		if err != nil {
			return fmt.Errorf("sync: %w", err)
		}
		if !written {
			s.log.Debug().Str("sub", p.Sub).Time("occurred_at", occurredAt).Msg("sync skipped: stale or unchanged")
			return nil
		}
		if err := tx.EnsureUsers(ctx, parents); err != nil {
			return fmt.Errorf("sync: %w", err)
		}
		if err := tx.ReconcileSubAccounts(ctx, p.Sub, parents); err != nil {
			return fmt.Errorf("sync: %w", err)
		}
		return nil
	})
}

// dedupe returns in-order unique non-empty values excluding self.
func dedupe(in []string, self string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, v := range in {
		if v == "" || v == self {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
