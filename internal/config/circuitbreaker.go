package config

import (
	"time"

	"github.com/4jawaly/identity-gateway/pkg/circuitbreaker"
)

// CircuitBreaker holds the thresholds shared by every outbound HTTP client
// plus one enable flag per integration.
type CircuitBreaker struct {
	MaxRequests         uint32        `env:"HTTP_CIRCUIT_BREAKER_MAX_REQUESTS" envDefault:"1"`
	Interval            time.Duration `env:"HTTP_CIRCUIT_BREAKER_INTERVAL" envDefault:"60s"`
	OpenTimeout         time.Duration `env:"HTTP_CIRCUIT_BREAKER_OPEN_TIMEOUT" envDefault:"15s"`
	ConsecutiveFailures uint32        `env:"HTTP_CIRCUIT_BREAKER_CONSECUTIVE_FAILURES" envDefault:"5"`

	ZitadelEnabled bool `env:"ZITADEL_CIRCUIT_BREAKER_ENABLED" envDefault:"false"`
}

func (c CircuitBreaker) integration(name string, enabled bool) circuitbreaker.Settings {
	return circuitbreaker.Settings{
		Name:                name,
		Enabled:             enabled,
		MaxRequests:         c.MaxRequests,
		Interval:            c.Interval,
		OpenTimeout:         c.OpenTimeout,
		ConsecutiveFailures: c.ConsecutiveFailures,
	}
}

// Zitadel returns the breaker settings for the Zitadel client.
func (c CircuitBreaker) Zitadel() circuitbreaker.Settings {
	return c.integration("zitadel", c.ZitadelEnabled)
}
