// Package zitadel is the userinfo client. It maps every upstream outcome to
// a catalog AppError so callers never inspect HTTP statuses themselves.
package zitadel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/4jawaly/identity-gateway/internal/response"
	"github.com/4jawaly/identity-gateway/pkg/circuitbreaker"
)

const maxBody = 1 << 20 // 1 MiB; userinfo payloads are a few KB

// Client calls the userinfo endpoint.
type Client struct {
	http         *http.Client
	issuer       string
	userinfoPath string
	retries      int
	log          zerolog.Logger
}

// Option customises NewClient.
type Option func(*Client)

// WithLogger sets the logger.
func WithLogger(l zerolog.Logger) Option { return func(c *Client) { c.log = l } }

// WithRetries sets how many extra attempts are made on 5xx/network errors.
func WithRetries(n int) Option { return func(c *Client) { c.retries = n } }

// WithUserinfoPath overrides the userinfo path (default /oidc/v1/userinfo).
func WithUserinfoPath(p string) Option { return func(c *Client) { c.userinfoPath = p } }

// NewClient builds a client. httpClient is already wrapped by the caller
// (circuit breaker, timeouts); this constructor adds nothing to it.
func NewClient(httpClient *http.Client, issuer string, opts ...Option) *Client {
	c := &Client{
		http:         httpClient,
		issuer:       strings.TrimRight(issuer, "/"),
		userinfoPath: "/oidc/v1/userinfo",
		retries:      1,
		log:          zerolog.Nop(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Issuer returns the configured issuer without a trailing slash.
func (c *Client) Issuer() string { return c.issuer }

// UserInfo fetches and decodes the claims for token.
//
//	400 / 401                                     -> TOKEN_INACTIVE
//	403 (proxy / firewall, NOT a revoked token)   -> ZITADEL_UNAVAILABLE
//	other status, transport error, timeout, bad JSON, breaker open -> ZITADEL_UNAVAILABLE
//
// Only 5xx and network errors are retried, never 4xx.
func (c *Client) UserInfo(ctx context.Context, token string) (*Claims, error) {
	var last error
	for attempt := 0; attempt <= c.retries; attempt++ {
		claims, err, retry := c.once(ctx, token)
		fmt.Println("claims", claims)
		fmt.Println("err", err)
		fmt.Println("retry", retry)
		if err == nil {
			return claims, nil
		}
		last = err
		if !retry || ctx.Err() != nil {
			break
		}
		c.log.Debug().Err(err).Int("attempt", attempt+1).Msg("zitadel userinfo retry")
	}
	return nil, last
}

// once performs one attempt; retry reports whether the failure is retryable.
func (c *Client) once(ctx context.Context, token string) (claims *Claims, err error, retry bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.issuer+c.userinfoPath, nil)
	if err != nil {
		return nil, response.ErrZitadelUnavailable.With(fmt.Errorf("build request: %w", err), nil), false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, circuitbreaker.ErrOpen) {
			return nil, response.ErrZitadelUnavailable.With(err, map[string]any{"reason": "circuit_open"}), false
		}
		return nil, response.ErrZitadelUnavailable.With(fmt.Errorf("userinfo request: %w", err), nil), true
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, response.ErrZitadelUnavailable.With(fmt.Errorf("read userinfo body: %w", err), nil), true
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		claims, err := ParseClaims(body)
		if err != nil {
			return nil, response.ErrZitadelUnavailable.With(err, nil), false
		}
		return claims, nil, false
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized:
		return nil, response.ErrTokenInactive.With(fmt.Errorf("userinfo status %d", resp.StatusCode), nil), false
	case resp.StatusCode == http.StatusForbidden:
		// A 403 from the userinfo endpoint is a proxy or firewall, not a
		// revoked token. Fail closed without caching anything.
		return nil, response.ErrZitadelUnavailable.With(fmt.Errorf("userinfo status 403"), map[string]any{"reason": "forbidden_upstream"}), false
	case resp.StatusCode >= http.StatusInternalServerError:
		return nil, response.ErrZitadelUnavailable.With(fmt.Errorf("userinfo status %d", resp.StatusCode), nil), true
	default:
		return nil, response.ErrZitadelUnavailable.With(fmt.Errorf("userinfo status %d", resp.StatusCode), nil), false
	}
}

// Ping checks that the issuer answers OIDC discovery. Used at boot only.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("zitadel discovery: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("zitadel discovery: status %d", resp.StatusCode)
	}
	return nil
}
