package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/4jawaly/identity-gateway/pkg/cache"
)

func TestRedis_TTL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		jitter float64
		rnd    float64
		in     time.Duration
		want   time.Duration
	}{
		{name: "no jitter returns input", jitter: 0, rnd: 0.9, in: 100 * time.Second, want: 100 * time.Second},
		{name: "rand 0 gives minus jitter", jitter: 0.15, rnd: 0, in: 100 * time.Second, want: 85 * time.Second},
		{name: "rand 0.5 gives no shift", jitter: 0.15, rnd: 0.5, in: 100 * time.Second, want: 100 * time.Second},
		{name: "rand 1 gives plus jitter", jitter: 0.15, rnd: 1, in: 100 * time.Second, want: 115 * time.Second},
		{name: "zero ttl stays zero", jitter: 0.15, rnd: 1, in: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := cache.NewRedis(nil, cache.WithJitter(tt.jitter), cache.WithRand(func() float64 { return tt.rnd }))
			assert.InDelta(t, float64(tt.want), float64(r.TTL(tt.in)), float64(time.Millisecond))
		})
	}
}

func TestRedis_PrefixAndJitterOnWire(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	a := cache.NewRedis(rdb, cache.WithKeyPrefix("a:"), cache.WithJitter(0.5), cache.WithRand(func() float64 { return 1 }))
	b := cache.NewRedis(rdb, cache.WithKeyPrefix("b:"))

	require.NoError(t, a.Set(ctx, "k", []byte("va"), 100*time.Second))
	require.NoError(t, b.Set(ctx, "k", []byte("vb"), 0))

	assert.True(t, srv.Exists("a:k"), "raw key carries the prefix")
	assert.True(t, srv.Exists("b:k"))
	assert.False(t, srv.Exists("k"))
	assert.InDelta(t, float64(150*time.Second), float64(srv.TTL("a:k")), float64(time.Second), "jitter applied to the stored TTL")

	va, _, _ := a.Get(ctx, "k")
	vb, _, _ := b.Get(ctx, "k")
	assert.Equal(t, "va", string(va), "stores with different prefixes are isolated")
	assert.Equal(t, "vb", string(vb))
}
