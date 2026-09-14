package circuitbreaker

import "time"

// Settings configures one breaker. Defaults live in config, not here: when
// Active() and any threshold is zero, New returns ErrInvalidSettings.
type Settings struct {
	Name                string
	Enabled             bool
	MaxRequests         uint32        // requests allowed through while half-open
	Interval            time.Duration // closed-state counter reset interval (0 = never)
	OpenTimeout         time.Duration // how long to stay open before probing
	ConsecutiveFailures uint32        // failures in a row that trip the breaker
}

// Active reports whether a breaker should be built at all.
func (s Settings) Active() bool { return s.Enabled && s.Name != "" }
