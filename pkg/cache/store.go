// Package cache is a small byte cache abstraction with a Redis and an
// in-memory implementation. It knows nothing about the domain: key naming
// and value semantics belong to the caller (see internal/modules/user/usercache).
//
// Contract: a backend failure is never fatal. Implementations return a miss
// plus the error so callers can log and fall through to the source of truth.
package cache

import (
	"context"
	"time"
)

// Store is the generic byte cache every module builds on.
type Store interface {
	// Get returns the value and true on a hit, (nil, false, nil) on a miss.
	Get(ctx context.Context, key string) (val []byte, found bool, err error)
	// MGet returns one entry per key, in order. A nil entry is a miss; a hit
	// is never nil, even for an empty stored value.
	MGet(ctx context.Context, keys ...string) ([][]byte, error)
	// Set stores val. ttl <= 0 stores without expiry.
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	// Del removes keys; missing keys are not an error.
	Del(ctx context.Context, keys ...string) error
}
