package queue

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/rs/zerolog"
)

// Recover converts a handler panic into an error so the message is retried
// or dead-lettered instead of killing the worker.
func Recover(log zerolog.Logger) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, e Envelope) (err error) {
			defer func() {
				if r := recover(); r != nil {
					log.Error().Interface("panic", r).Str("event_id", e.EventID).
						Bytes("stack", debug.Stack()).Msg("handler panicked")
					err = fmt.Errorf("handler panic: %v", r)
				}
			}()
			return next(ctx, e)
		}
	}
}

// RetryLater is returned by the Retry middleware when a message should be
// re-queued after a delay. The consumer turns it into a delayed publish.
type RetryLater struct {
	After time.Duration
	Err   error
}

func (r *RetryLater) Error() string { return fmt.Sprintf("retry after %s: %v", r.After, r.Err) }
func (r *RetryLater) Unwrap() error { return r.Err }

// Retry classifies a handler error: permanent errors and exhausted retries
// pass through unchanged (the consumer dead-letters them); everything else
// becomes a RetryLater with the policy's backoff.
func Retry(p RetryPolicy) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, e Envelope) error {
			err := next(ctx, e)
			if err == nil || IsPermanent(err) {
				return err
			}
			var rl *RetryLater
			if errors.As(err, &rl) {
				return err
			}
			if e.RetryCount >= p.MaxRetries {
				return err
			}
			return &RetryLater{After: p.Delay(e.RetryCount), Err: err}
		}
	}
}
