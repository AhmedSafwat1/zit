package cache

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis implements Store on go-redis. The client is owned by the caller.
type Redis struct {
	rdb    redis.UniversalClient
	prefix string
	jitter float64
	rnd    func() float64
}

// RedisOption customises NewRedis.
type RedisOption func(*Redis)

// WithJitter spreads every write TTL by ±fraction (0.15 = ±15%) so keys
// written together do not expire together.
func WithJitter(fraction float64) RedisOption { return func(r *Redis) { r.jitter = fraction } }

// WithRand replaces the jitter source (tests).
func WithRand(f func() float64) RedisOption { return func(r *Redis) { r.rnd = f } }

// WithKeyPrefix namespaces every key ("zit:"), so one Redis can be shared
// between applications.
func WithKeyPrefix(p string) RedisOption { return func(r *Redis) { r.prefix = p } }

// NewRedis builds the store.
func NewRedis(rdb redis.UniversalClient, opts ...RedisOption) *Redis {
	r := &Redis{rdb: rdb, rnd: rand.Float64}
	for _, o := range opts {
		o(r)
	}
	return r
}

// TTL applies the configured jitter to d. d <= 0 is returned unchanged (no expiry).
func (r *Redis) TTL(d time.Duration) time.Duration {
	if r.jitter <= 0 || d <= 0 {
		return d
	}
	spread := (r.rnd()*2 - 1) * r.jitter // [-jitter, +jitter)
	return time.Duration(float64(d) * (1 + spread))
}

func (r *Redis) key(k string) string { return r.prefix + k }

// Get implements Store.
func (r *Redis) Get(ctx context.Context, key string) ([]byte, bool, error) {
	v, err := r.rdb.Get(ctx, r.key(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cache get %s: %w", key, err)
	}
	if v == nil {
		v = []byte{}
	}
	return v, true, nil
}

// MGet implements Store: one round trip for all keys.
func (r *Redis) MGet(ctx context.Context, keys ...string) ([][]byte, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = r.key(k)
	}
	vals, err := r.rdb.MGet(ctx, full...).Result()
	if err != nil {
		return nil, fmt.Errorf("cache mget: %w", err)
	}
	if len(vals) != len(keys) {
		return nil, fmt.Errorf("cache mget: expected %d values, got %d", len(keys), len(vals))
	}
	out := make([][]byte, len(keys))
	for i, v := range vals {
		if s, ok := v.(string); ok {
			b := make([]byte, len(s))
			copy(b, s)
			out[i] = b
		}
	}
	return out, nil
}

// Set implements Store. The TTL is jittered.
func (r *Redis) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	if err := r.rdb.Set(ctx, r.key(key), val, r.TTL(ttl)).Err(); err != nil {
		return fmt.Errorf("cache set %s: %w", key, err)
	}
	return nil
}

// Del implements Store.
func (r *Redis) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = r.key(k)
	}
	if err := r.rdb.Del(ctx, full...).Err(); err != nil {
		return fmt.Errorf("cache del: %w", err)
	}
	return nil
}

var _ Store = (*Redis)(nil)
