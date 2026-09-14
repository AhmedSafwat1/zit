// Package httpclient builds tuned *http.Client instances. Resilience wrappers
// (circuit breaker, retries) are applied by the caller at the wiring boundary.
package httpclient

import (
	"net"
	"net/http"
	"time"
)

// Option customises the client.
type Option func(*settings)

type settings struct {
	timeout             time.Duration
	maxIdleConnsPerHost int
	maxConnsPerHost     int
	transport           *http.Transport
}

// WithTimeout sets the overall per-request timeout.
func WithTimeout(d time.Duration) Option { return func(s *settings) { s.timeout = d } }

// WithMaxIdleConnsPerHost sets the idle keep-alive pool size per host.
func WithMaxIdleConnsPerHost(n int) Option { return func(s *settings) { s.maxIdleConnsPerHost = n } }

// WithMaxConnsPerHost caps total connections per host (0 = unlimited).
func WithMaxConnsPerHost(n int) Option { return func(s *settings) { s.maxConnsPerHost = n } }

// WithTransport replaces the base transport (tests).
func WithTransport(t *http.Transport) Option { return func(s *settings) { s.transport = t } }

// New returns a client with sane production defaults.
func New(opts ...Option) *http.Client {
	s := settings{timeout: 5 * time.Second, maxIdleConnsPerHost: 64}
	for _, o := range opts {
		o(&s)
	}
	t := s.transport
	if t == nil {
		t = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          s.maxIdleConnsPerHost * 2,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   3 * time.Second,
			ExpectContinueTimeout: time.Second,
		}
	}
	t.MaxIdleConnsPerHost = s.maxIdleConnsPerHost
	t.MaxConnsPerHost = s.maxConnsPerHost
	return &http.Client{Transport: t, Timeout: s.timeout}
}
