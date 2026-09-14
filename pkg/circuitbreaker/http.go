// Package circuitbreaker wraps an *http.Client's transport with a
// sony/gobreaker breaker. Wiring happens in main.go, never in a client's
// constructor, so every integration is wrapped the same way.
package circuitbreaker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"github.com/sony/gobreaker/v2"
)

// Option customises New.
type Option func(*builder)

type builder struct {
	base     *http.Client
	settings Settings
	log      zerolog.Logger
}

// WithHTTPClient sets the client whose Transport gets wrapped. Required.
func WithHTTPClient(c *http.Client) Option { return func(b *builder) { b.base = c } }

// WithSettings sets the breaker thresholds.
func WithSettings(s Settings) Option { return func(b *builder) { b.settings = s } }

// WithLogger sets the logger for state-change events.
func WithLogger(l zerolog.Logger) Option { return func(b *builder) { b.log = l } }

// New returns the base client wrapped with a breaker. When the settings are
// not Active() the base client is returned unchanged: zero overhead.
//
// The base client's Timeout is moved into the wrapped transport as a
// per-request context deadline so the failure predicate can tell "our
// timeout fired" apart from "the caller went away".
func New(opts ...Option) (*http.Client, error) {
	b := builder{log: zerolog.Nop()}
	for _, o := range opts {
		o(&b)
	}
	if b.base == nil {
		return nil, errors.New("circuit breaker: WithHTTPClient is required")
	}
	if !b.settings.Active() {
		return b.base, nil
	}
	s := b.settings
	if s.MaxRequests == 0 || s.ConsecutiveFailures == 0 || s.OpenTimeout <= 0 {
		return nil, ErrInvalidSettings
	}

	log := b.log.With().Str("integration", s.Name).Logger()
	cb := gobreaker.NewCircuitBreaker[*http.Response](gobreaker.Settings{
		Name:        s.Name,
		MaxRequests: s.MaxRequests,
		Interval:    s.Interval,
		Timeout:     s.OpenTimeout,
		ReadyToTrip: func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= s.ConsecutiveFailures },
		OnStateChange: func(_ string, from, to gobreaker.State) {
			ev := log.Info()
			if to == gobreaker.StateOpen {
				ev = log.Warn()
			}
			ev.Str("from", from.String()).Str("to", to.String()).Msg("circuit breaker state changed")
		},
		IsSuccessful: func(err error) bool {
			var f *failure
			return !errors.As(err, &f)
		},
	})

	next := b.base.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	wrapped := *b.base
	wrapped.Timeout = 0
	wrapped.Transport = &transport{next: next, cb: cb, name: s.Name, timeout: b.base.Timeout, log: log}
	return &wrapped, nil
}

// failure marks an outcome the breaker must count. err is nil for a 5xx.
type failure struct{ err error }

func (f *failure) Error() string {
	if f.err == nil {
		return "upstream returned 5xx"
	}
	return f.err.Error()
}
func (f *failure) Unwrap() error { return f.err }

type transport struct {
	next    http.RoundTripper
	cb      *gobreaker.CircuitBreaker[*http.Response]
	name    string
	timeout time.Duration
	log     zerolog.Logger
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	parent := req.Context()
	resp, err := t.cb.Execute(func() (*http.Response, error) {
		r := req
		cancel := context.CancelFunc(func() {})
		if t.timeout > 0 {
			var ctx context.Context
			ctx, cancel = context.WithTimeout(parent, t.timeout)
			r = req.WithContext(ctx)
		}
		resp, err := t.next.RoundTrip(r)
		if err != nil {
			cancel()
		} else {
			// The deadline must also bound the body read; release it on Close.
			resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
		}
		if isFailure(parent, resp, err) {
			return resp, &failure{err: err}
		}
		return resp, err
	})
	if err == nil {
		return resp, nil
	}
	var f *failure
	if errors.As(err, &f) {
		if f.err == nil {
			return resp, nil // a 5xx is still a response for the caller
		}
		return nil, f.err
	}
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		t.log.Debug().Str("state", t.cb.State().String()).Msg("circuit breaker rejected request")
		return nil, openError(t.name)
	}
	return nil, err
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}
