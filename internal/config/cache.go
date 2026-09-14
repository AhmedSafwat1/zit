package config

import "time"

// Cache holds the key namespace and the TTLs written by the introspection path.
type Cache struct {
	// KeyPrefix namespaces every key so one Redis can be shared. Changing it
	// is a one-time cold cache.
	KeyPrefix     string        `env:"KEY_PREFIX" envDefault:"zit:"`
	IntrospectTTL time.Duration `env:"INTROSPECT_TTL" envDefault:"120s"`
	InactiveTTL   time.Duration `env:"INACTIVE_TTL" envDefault:"30s"`
	SyncDedupeTTL time.Duration `env:"SYNC_DEDUPE_TTL" envDefault:"1h"`
	UserTTL       time.Duration `env:"USER_TTL" envDefault:"300s"`
	TTLJitter     float64       `env:"TTL_JITTER" envDefault:"0.15"`
}
