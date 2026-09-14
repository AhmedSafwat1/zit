// Package queue is the RabbitMQ transport: one envelope for every message,
// a confirm-mode publisher with reconnect, and a consumer with retry/DLQ
// routing. Idempotency is the handler's job (stale-guarded upserts).
package queue

import (
	"context"
	"time"
)

// Publisher is what the API side holds. Publish must never block a request
// beyond the configured publish timeout.
type Publisher interface {
	Publish(ctx context.Context, routingKey string, e Envelope) error
	Close(ctx context.Context) error
}

// Handler processes one envelope. Return nil to ack.
type Handler func(ctx context.Context, e Envelope) error

// Middleware wraps a Handler.
type Middleware func(Handler) Handler

// Chain applies middlewares so the first one listed is outermost.
func Chain(h Handler, mws ...Middleware) Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// RetryPolicy describes the delayed-retry schedule: delay(attempt) = Base * Factor^attempt.
type RetryPolicy struct {
	MaxRetries int
	Base       time.Duration
	Factor     float64
}

// DefaultRetryPolicy is 1s -> 5s -> 25s.
func DefaultRetryPolicy(maxRetries int, base time.Duration) RetryPolicy {
	if base <= 0 {
		base = time.Second
	}
	return RetryPolicy{MaxRetries: maxRetries, Base: base, Factor: 5}
}

// Delay returns the wait before the given (zero-based) retry attempt.
func (p RetryPolicy) Delay(attempt int) time.Duration {
	d := float64(p.Base)
	for i := 0; i < attempt; i++ {
		d *= p.Factor
	}
	return time.Duration(d)
}
