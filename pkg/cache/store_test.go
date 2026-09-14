package cache_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/4jawaly/identity-gateway/pkg/cache"
)

// fixture is one Store under test plus a way to move its clock forward.
type fixture struct {
	store   cache.Store
	advance func(time.Duration)
}

func newMemory(t *testing.T) fixture {
	t.Helper()
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	return fixture{
		store:   cache.NewMemory(cache.WithClock(clock)),
		advance: func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() },
	}
}

func newRedis(t *testing.T) fixture {
	t.Helper()
	srv := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return fixture{
		store:   cache.NewRedis(rdb, cache.WithKeyPrefix("t:")),
		advance: srv.FastForward,
	}
}

var backends = map[string]func(t *testing.T) fixture{
	"memory": newMemory,
	"redis":  newRedis,
}

func TestStore_Conformance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name string
		run  func(t *testing.T, f fixture)
	}{
		{name: "get missing is a clean miss", run: func(t *testing.T, f fixture) {
			v, ok, err := f.store.Get(ctx, "nope")
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Nil(t, v)
		}},
		{name: "set then get returns the bytes", run: func(t *testing.T, f fixture) {
			require.NoError(t, f.store.Set(ctx, "k", []byte("v1"), time.Minute))
			v, ok, err := f.store.Get(ctx, "k")
			require.NoError(t, err)
			assert.True(t, ok)
			assert.Equal(t, []byte("v1"), v)
		}},
		{name: "set overwrites", run: func(t *testing.T, f fixture) {
			require.NoError(t, f.store.Set(ctx, "k", []byte("v1"), time.Minute))
			require.NoError(t, f.store.Set(ctx, "k", []byte("v2"), time.Minute))
			v, _, _ := f.store.Get(ctx, "k")
			assert.Equal(t, []byte("v2"), v)
		}},
		{name: "ttl expires the key", run: func(t *testing.T, f fixture) {
			require.NoError(t, f.store.Set(ctx, "k", []byte("v"), 10*time.Second))
			f.advance(9 * time.Second)
			_, ok, _ := f.store.Get(ctx, "k")
			assert.True(t, ok, "before ttl")
			f.advance(2 * time.Second)
			_, ok, _ = f.store.Get(ctx, "k")
			assert.False(t, ok, "after ttl")
		}},
		{name: "ttl zero never expires", run: func(t *testing.T, f fixture) {
			require.NoError(t, f.store.Set(ctx, "k", []byte("v"), 0))
			f.advance(24 * 365 * time.Hour)
			_, ok, _ := f.store.Get(ctx, "k")
			assert.True(t, ok)
		}},
		{name: "get returns a copy, caller cannot mutate the stored value", run: func(t *testing.T, f fixture) {
			in := []byte("abc")
			require.NoError(t, f.store.Set(ctx, "k", in, time.Minute))
			in[0] = 'z'
			v, _, _ := f.store.Get(ctx, "k")
			assert.Equal(t, []byte("abc"), v)
			v[0] = 'y'
			again, _, _ := f.store.Get(ctx, "k")
			assert.Equal(t, []byte("abc"), again)
		}},
		{name: "mget keeps order, nil for miss, non-nil for empty value", run: func(t *testing.T, f fixture) {
			require.NoError(t, f.store.Set(ctx, "a", []byte("1"), time.Minute))
			require.NoError(t, f.store.Set(ctx, "empty", []byte{}, time.Minute))
			vals, err := f.store.MGet(ctx, "a", "missing", "empty")
			require.NoError(t, err)
			require.Len(t, vals, 3)
			assert.Equal(t, []byte("1"), vals[0])
			assert.Nil(t, vals[1])
			assert.NotNil(t, vals[2])
			assert.Empty(t, vals[2])
		}},
		{name: "mget with no keys is nil nil", run: func(t *testing.T, f fixture) {
			vals, err := f.store.MGet(ctx)
			require.NoError(t, err)
			assert.Nil(t, vals)
		}},
		{name: "del missing, multiple and none", run: func(t *testing.T, f fixture) {
			require.NoError(t, f.store.Del(ctx, "missing"))
			require.NoError(t, f.store.Del(ctx))
			require.NoError(t, f.store.Set(ctx, "a", []byte("1"), 0))
			require.NoError(t, f.store.Set(ctx, "b", []byte("2"), 0))
			require.NoError(t, f.store.Del(ctx, "a", "b"))
			vals, _ := f.store.MGet(ctx, "a", "b")
			assert.Equal(t, [][]byte{nil, nil}, vals)
		}},
		{name: "cancelled context is a miss with error", run: func(t *testing.T, f fixture) {
			c, cancel := context.WithCancel(ctx)
			cancel()
			_, ok, err := f.store.Get(c, "k")
			assert.False(t, ok)
			assert.Error(t, err)
			assert.Error(t, f.store.Set(c, "k", nil, 0))
		}},
		{name: "parallel set get del is race free", run: func(t *testing.T, f fixture) {
			var wg sync.WaitGroup
			for i := 0; i < 16; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 50; j++ {
						_ = f.store.Set(ctx, "p", []byte("x"), time.Minute)
						_, _, _ = f.store.Get(ctx, "p")
						_, _ = f.store.MGet(ctx, "p", "q")
						_ = f.store.Del(ctx, "q")
					}
				}()
			}
			wg.Wait()
		}},
	}
	for name, newFixture := range backends {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					tt.run(t, newFixture(t))
				})
			}
		})
	}
}

func TestGetSetJSON(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	type doc struct {
		Sub   string   `json:"sub"`
		Roles []string `json:"roles"`
	}
	tests := []struct {
		name  string
		setup func(s cache.Store)
		want  doc
		found bool
		err   bool
	}{
		{name: "round trip", setup: func(s cache.Store) {
			_ = cache.SetJSON(ctx, s, "d", doc{Sub: "1", Roles: []string{"A"}}, 0)
		}, want: doc{Sub: "1", Roles: []string{"A"}}, found: true},
		{name: "missing is a miss", setup: func(cache.Store) {}},
		{name: "corrupt entry is a miss with error", setup: func(s cache.Store) {
			_ = s.Set(ctx, "d", []byte("{nope"), 0)
		}, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := cache.NewMemory()
			tt.setup(s)
			got, found, err := cache.GetJSON[doc](ctx, s, "d")
			assert.Equal(t, tt.found, found)
			assert.Equal(t, tt.want, got)
			if tt.err {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestKeyAndHash(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "key joins with colon", got: cache.Key("introspect", "h"), want: "introspect:h"},
		{name: "key with three parts", got: cache.Key("sync", "seen", "355"), want: "sync:seen:355"},
		{name: "sha256 of empty", got: cache.SHA256Hex(""), want: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{name: "sha256 known vector", got: cache.SHA256Hex("abc"), want: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.got)
		})
	}
}
