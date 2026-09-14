package circuitbreaker

import (
	"context"
	"errors"
	"net/http"
)

// isFailure decides whether an outcome counts against the breaker.
//
//	transport / network error                       -> failure
//	HTTP >= 500                                     -> failure
//	HTTP 4xx                                        -> success (a 401 is a normal revoked token)
//	caller's context cancelled                      -> success (client went away, upstream is fine)
//	our per-request timeout while parent still alive -> failure (slow-but-alive upstream)
//
// parent is the caller's context *before* the per-request timeout was added.
func isFailure(parent context.Context, resp *http.Response, err error) bool {
	if err == nil {
		return resp != nil && resp.StatusCode >= http.StatusInternalServerError
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		// The parent is still alive, so the deadline that fired was ours:
		// the upstream is too slow and that must count.
		return parent.Err() == nil
	}
	return true
}
