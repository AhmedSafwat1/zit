package usercache

import (
	"context"
	"fmt"
	"time"

	"github.com/4jawaly/identity-gateway/internal/modules/user/jobs/sync_user"
	"github.com/4jawaly/identity-gateway/internal/modules/user/services"
	"github.com/4jawaly/identity-gateway/pkg/cache"
)

// Cache implements services.Cache and sync_user.DedupeCache on a cache.Store.
//
// The report and the inactive marker for one token are two keys. Reads give
// the inactive marker precedence, and every compound write stores the key
// that wins on read first, so a partial failure can only make the cache
// more conservative, never serve a revoked token.
type Cache struct{ s cache.Store }

// New builds the cache on any Store.
func New(s cache.Store) *Cache { return &Cache{s: s} }

// LookupIntrospect reads the report and the inactive marker in one round trip.
func (c *Cache) LookupIntrospect(ctx context.Context, hash string) ([]byte, services.State, error) {
	vals, err := c.s.MGet(ctx, IntrospectKey(hash), InactiveKey(hash))
	if err != nil {
		return nil, services.StateMiss, err
	}
	if len(vals) != 2 {
		return nil, services.StateMiss, fmt.Errorf("lookup introspect: expected 2 values, got %d", len(vals))
	}
	if vals[1] != nil {
		return nil, services.StateInactive, nil
	}
	if len(vals[0]) > 0 {
		return vals[0], services.StateHit, nil
	}
	return nil, services.StateMiss, nil
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

// GetUser returns the cached user document, or nil when absent.
func (c *Cache) GetUser(ctx context.Context, sub string) ([]byte, error) {
	v, ok, err := c.s.Get(ctx, UserKey(sub))
	if err != nil || !ok {
		return nil, err
	}
	return v, nil
}

// SetUser stores a user document.
func (c *Cache) SetUser(ctx context.Context, sub string, data []byte, ttl time.Duration) error {
	return c.s.Set(ctx, UserKey(sub), data, ttl)
}

var (
	_ services.Cache        = (*Cache)(nil)
	_ sync_user.DedupeCache = (*Cache)(nil)
)
