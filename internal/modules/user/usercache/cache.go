package usercache

import (
	"context"
	"fmt"
	"time"

	usersync "github.com/4jawaly/identity-gateway/internal/modules/user/jobs/sync_user"
	usersvc "github.com/4jawaly/identity-gateway/internal/modules/user/services"
	"github.com/4jawaly/identity-gateway/pkg/cache"
)

// Cache implements usersvc.Cache and usersync.DedupeCache on a cache.Store.
//
// The report and the inactive marker for one token are two keys. Reads give
// the inactive marker precedence, and every compound write stores the key
// that wins on read first, so a partial failure can only make the cache
// more conservative, never serve a revoked token.
type Cache struct{ s cache.Store }

// New builds the cache on any Store.
func New(s cache.Store) *Cache { return &Cache{s: s} }

// LookupIntrospect reads the report and the inactive marker in one round trip.
func (c *Cache) LookupIntrospect(ctx context.Context, hash string) ([]byte, usersvc.State, error) {
	vals, err := c.s.MGet(ctx, IntrospectKey(hash), InactiveKey(hash))
	if err != nil {
		return nil, usersvc.StateMiss, err
	}
	if len(vals) != 2 {
		return nil, usersvc.StateMiss, fmt.Errorf("lookup introspect: expected 2 values, got %d", len(vals))
	}
	if vals[1] != nil {
		return nil, usersvc.StateInactive, nil
	}
	if len(vals[0]) > 0 {
		return vals[0], usersvc.StateHit, nil
	}
	return nil, usersvc.StateMiss, nil
}

// SetIntrospect stores the report, then clears any inactive marker.
func (c *Cache) SetIntrospect(ctx context.Context, hash string, report []byte, ttl time.Duration) error {
	if err := c.s.Set(ctx, IntrospectKey(hash), report, ttl); err != nil {
		return err
	}
	return c.s.Del(ctx, InactiveKey(hash))
}

// MarkInactive stores the negative marker, then drops any cached report.
func (c *Cache) MarkInactive(ctx context.Context, hash string, ttl time.Duration) error {
	if err := c.s.Set(ctx, InactiveKey(hash), []byte("1"), ttl); err != nil {
		return err
	}
	return c.s.Del(ctx, IntrospectKey(hash))
}

// GetSyncSeen returns the last published hash for sub, or "" when absent.
func (c *Cache) GetSyncSeen(ctx context.Context, sub string) (string, error) {
	v, _, err := c.s.Get(ctx, SyncSeenKey(sub))
	return string(v), err
}

// SetSyncSeen records the published hash for sub.
func (c *Cache) SetSyncSeen(ctx context.Context, sub, hash string, ttl time.Duration) error {
	return c.s.Set(ctx, SyncSeenKey(sub), []byte(hash), ttl)
}

var (
	_ usersvc.Cache        = (*Cache)(nil)
	_ usersync.DedupeCache = (*Cache)(nil)
)
