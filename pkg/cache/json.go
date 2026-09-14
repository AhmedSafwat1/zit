package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// GetJSON reads key and decodes it into T. A decode failure is reported as
// a miss with the error, so a corrupt entry is treated like an absent one.
func GetJSON[T any](ctx context.Context, s Store, key string) (T, bool, error) {
	var v T
	raw, ok, err := s.Get(ctx, key)
	if err != nil || !ok {
		return v, false, err
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, false, fmt.Errorf("decode %s: %w", key, err)
	}
	return v, true, nil
}

// SetJSON encodes v and stores it under key.
func SetJSON(ctx context.Context, s Store, key string, v any, ttl time.Duration) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s: %w", key, err)
	}
	return s.Set(ctx, key, raw, ttl)
}
