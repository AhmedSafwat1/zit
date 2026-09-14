package cache

import (
	"context"
	"sync"
	"time"
)

// Memory is an in-process Store for tests and local development. Expiry is
// lazy (checked on read); there is no sweeper, so it is not meant for
// long-lived production use.
type Memory struct {
	mu  sync.Mutex
	now func() time.Time
	m   map[string]memEntry
}

type memEntry struct {
	val     []byte
	expires time.Time // zero = never
}

// MemoryOption customises NewMemory.
type MemoryOption func(*Memory)

// WithClock replaces the time source (tests).
func WithClock(now func() time.Time) MemoryOption { return func(m *Memory) { m.now = now } }

// NewMemory builds an empty store.
func NewMemory(opts ...MemoryOption) *Memory {
	m := &Memory{now: time.Now, m: map[string]memEntry{}}
	for _, o := range opts {
		o(m)
	}
	return m
}

func (m *Memory) get(key string, now time.Time) ([]byte, bool) {
	e, ok := m.m[key]
	if !ok {
		return nil, false
	}
	if !e.expires.IsZero() && !now.Before(e.expires) {
		delete(m.m, key)
		return nil, false
	}
	out := make([]byte, len(e.val))
	copy(out, e.val)
	return out, true
}

// Get implements Store.
func (m *Memory) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.get(key, m.now())
	return v, ok, nil
}

// MGet implements Store.
func (m *Memory) MGet(ctx context.Context, keys ...string) ([][]byte, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	out := make([][]byte, len(keys))
	for i, k := range keys {
		if v, ok := m.get(k, now); ok {
			out[i] = v
		}
	}
	return out, nil
}

// Set implements Store.
func (m *Memory) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cp := make([]byte, len(val))
	copy(cp, val)
	e := memEntry{val: cp}
	if ttl > 0 {
		e.expires = m.now().Add(ttl)
	}
	m.mu.Lock()
	m.m[key] = e
	m.mu.Unlock()
	return nil
}

// Del implements Store.
func (m *Memory) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	for _, k := range keys {
		delete(m.m, k)
	}
	m.mu.Unlock()
	return nil
}

// Len returns the number of stored (possibly expired) entries. Tests only.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.m)
}

var _ Store = (*Memory)(nil)
