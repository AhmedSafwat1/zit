package circuitbreaker

import (
	"errors"
	"fmt"
)

var (
	// ErrOpen is returned (wrapped with the breaker name) when the breaker
	// rejects a request. Detect with errors.Is(err, ErrOpen).
	ErrOpen = errors.New("circuit breaker is open")
	// ErrInvalidSettings is returned by New for an active breaker with a zero threshold.
	ErrInvalidSettings = errors.New("circuit breaker: invalid settings")
)

func openError(name string) error { return fmt.Errorf("%w: %s", ErrOpen, name) }
