package tests

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	usersvc "github.com/4jawaly/identity-gateway/internal/modules/user/services"
	"github.com/4jawaly/identity-gateway/internal/modules/user/usercache"
	"github.com/4jawaly/identity-gateway/pkg/cache"
)

// clockStore is an in-memory store with a controllable clock.
type clockStore struct {
	*cache.Memory
	mu  sync.Mutex
	now time.Time
}

func newClockStore() *clockStore {
	cs := &clockStore{now: time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)}
	cs.Memory = cache.NewMemory(cache.WithClock(func() time.Time {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		return cs.now
	}))
	return cs
}

func (c *clockStore) advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

// failStore is a Store whose every call fails. It is the process boundary
// (Redis down), so a hand-written stub is acceptable here.
type failStore struct{ err error }

func (f failStore) Get(context.Context, string) ([]byte, bool, error)        { return nil, false, f.err }
func (f failStore) MGet(context.Context, ...string) ([][]byte, error)        { return nil, f.err }
func (f failStore) Set(context.Context, string, []byte, time.Duration) error { return f.err }
func (f failStore) Del(context.Context, ...string) error                     { return f.err }

func TestUserCache_Introspect(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const h = "abc123"
	report := []byte(`{"ok":true}`)

	tests := []struct {
		name      string
		run       func(t *testing.T, cs *clockStore, uc *usercache.Cache)
		wantState usersvc.State
		wantBytes []byte
	}{
		{name: "empty cache is a miss", run: func(*testing.T, *clockStore, *usercache.Cache) {}, wantState: usersvc.StateMiss},
		{name: "set then lookup is a hit", run: func(t *testing.T, _ *clockStore, uc *usercache.Cache) {
			require.NoError(t, uc.SetIntrospect(ctx, h, report, time.Minute))
		}, wantState: usersvc.StateHit, wantBytes: report},
		{name: "mark inactive then lookup is inactive", run: func(t *testing.T, _ *clockStore, uc *usercache.Cache) {
			require.NoError(t, uc.MarkInactive(ctx, h, 30*time.Second))
		}, wantState: usersvc.StateInactive},
		{name: "set after mark clears the marker", run: func(t *testing.T, _ *clockStore, uc *usercache.Cache) {
			require.NoError(t, uc.MarkInactive(ctx, h, 30*time.Second))
			require.NoError(t, uc.SetIntrospect(ctx, h, report, time.Minute))
		}, wantState: usersvc.StateHit, wantBytes: report},
		{name: "mark after set drops the report so expiry yields a miss not a stale hit", run: func(t *testing.T, cs *clockStore, uc *usercache.Cache) {
			require.NoError(t, uc.SetIntrospect(ctx, h, report, time.Hour))
			require.NoError(t, uc.MarkInactive(ctx, h, 30*time.Second))
			_, st, _ := uc.LookupIntrospect(ctx, h)
			require.Equal(t, usersvc.StateInactive, st)
			cs.advance(31 * time.Second)
		}, wantState: usersvc.StateMiss},
		{name: "both keys present means inactive wins", run: func(t *testing.T, cs *clockStore, _ *usercache.Cache) {
			require.NoError(t, cs.Set(ctx, usercache.IntrospectKey(h), report, 0))
			require.NoError(t, cs.Set(ctx, usercache.InactiveKey(h), []byte("1"), 0))
		}, wantState: usersvc.StateInactive},
		{name: "empty report is a miss", run: func(t *testing.T, _ *clockStore, uc *usercache.Cache) {
			require.NoError(t, uc.SetIntrospect(ctx, h, []byte{}, time.Minute))
		}, wantState: usersvc.StateMiss},
		{name: "report expires", run: func(t *testing.T, cs *clockStore, uc *usercache.Cache) {
			require.NoError(t, uc.SetIntrospect(ctx, h, report, time.Minute))
			cs.advance(61 * time.Second)
		}, wantState: usersvc.StateMiss},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cs := newClockStore()
			uc := usercache.New(cs)
			tt.run(t, cs, uc)
			got, st, err := uc.LookupIntrospect(ctx, h)
			require.NoError(t, err)
			assert.Equal(t, tt.wantState, st)
			assert.Equal(t, tt.wantBytes, got)
		})
	}
}

func TestUserCache_StoreFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	boom := errors.New("redis: connection refused")
	uc := usercache.New(failStore{err: boom})

	tests := []struct {
		name string
		call func() error
	}{
		{name: "lookup is a miss with error", call: func() error {
			b, st, err := uc.LookupIntrospect(ctx, "h")
			assert.Nil(t, b)
			assert.Equal(t, usersvc.StateMiss, st)
			return err
		}},
		{name: "set introspect fails", call: func() error { return uc.SetIntrospect(ctx, "h", []byte("x"), time.Minute) }},
		{name: "mark inactive fails", call: func() error { return uc.MarkInactive(ctx, "h", time.Minute) }},
		{name: "get sync seen fails", call: func() error { _, err := uc.GetSyncSeen(ctx, "s"); return err }},
		{name: "set sync seen fails", call: func() error { return uc.SetSyncSeen(ctx, "s", "h", time.Hour) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, tt.call(), boom)
		})
	}
}

func TestUserCache_SyncSeen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cs := newClockStore()
	uc := usercache.New(cs)

	seen, err := uc.GetSyncSeen(ctx, "355")
	require.NoError(t, err)
	assert.Equal(t, "", seen, "missing sync seen is empty string")
	require.NoError(t, uc.SetSyncSeen(ctx, "355", "hash1", time.Hour))
	seen, _ = uc.GetSyncSeen(ctx, "355")
	assert.Equal(t, "hash1", seen)
	cs.advance(time.Hour + time.Second)
	seen, _ = uc.GetSyncSeen(ctx, "355")
	assert.Equal(t, "", seen, "sync seen expires")
}

func TestUserCache_Keys(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, got, want string }{
		{"introspect", usercache.IntrospectKey("h"), "introspect:h"},
		{"inactive", usercache.InactiveKey("h"), "inactive:h"},
		{"sync seen", usercache.SyncSeenKey("355"), "sync:seen:355"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.got)
		})
	}
}
