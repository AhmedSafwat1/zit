package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/singleflight"

	userdto "github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	"github.com/4jawaly/identity-gateway/internal/response"
	"github.com/4jawaly/identity-gateway/internal/types"
)

const maxTokenBytes = 8 * 1024

// tokenByteOK is a 256-entry lookup of the RFC 6750 b64token alphabet so
// ValidateToken is one array index per byte, no allocation, no rune decode.
var tokenByteOK = func() (t [256]bool) {
	for _, c := range []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~+/=") {
		t[c] = true
	}
	return t
}()

// UserService is the API-side service: introspection (hot path) and the
// read-through Postgres reads.
type UserService struct {
	zitadel  ZitadelClient
	cache    Cache
	notifier SyncNotifier
	log      zerolog.Logger

	issuer        string
	introspectTTL time.Duration
	inactiveTTL   time.Duration
	syncBudget    time.Duration
	run           func(func())

	sf singleflight.Group
}

// ServiceOption customises NewUserService.
type ServiceOption func(*UserService)

// WithSyncRunner overrides how post-response work is scheduled. Default: go f().
func WithSyncRunner(run func(func())) ServiceOption {
	return func(s *UserService) { s.run = run }
}

// WithIssuer sets the expected issuer; an empty issuer disables the check.
func WithIssuer(iss string) ServiceOption {
	return func(s *UserService) { s.issuer = strings.TrimRight(iss, "/") }
}

// WithTTLs sets the cache TTLs.
func WithTTLs(introspect, inactive time.Duration) ServiceOption {
	return func(s *UserService) { s.introspectTTL, s.inactiveTTL = introspect, inactive }
}

// WithSyncBudget bounds the detached cache-write + notify work.
func WithSyncBudget(d time.Duration) ServiceOption { return func(s *UserService) { s.syncBudget = d } }

// WithLogger sets the logger.
func WithLogger(l zerolog.Logger) ServiceOption { return func(s *UserService) { s.log = l } }

// NewUserService wires the hot path.
func NewUserService(z ZitadelClient, c Cache, n SyncNotifier, opts ...ServiceOption) *UserService {
	s := &UserService{
		zitadel: z, cache: c, notifier: n, log: zerolog.Nop(),
		introspectTTL: 120 * time.Second, inactiveTTL: 30 * time.Second,
		syncBudget: 500 * time.Millisecond,
		run:        func(f func()) { go f() },
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Pre-built rejections: this path is hit by garbage traffic at full rate,
// so it must not allocate.
var (
	errTokenEmpty   = response.ErrTokenInvalid.With(nil, map[string]any{"reason": "empty"})
	errTokenTooLong = response.ErrTokenInvalid.With(nil, map[string]any{"reason": "too_long"})
	errTokenCharset = response.ErrTokenInvalid.With(nil, map[string]any{"reason": "charset"})
)

// ValidateToken rejects structurally invalid tokens before any I/O.
func ValidateToken(token string) error {
	switch {
	case token == "":
		return errTokenEmpty
	case len(token) > maxTokenBytes:
		return errTokenTooLong
	}
	for i := 0; i < len(token); i++ {
		if !tokenByteOK[token[i]] {
			return errTokenCharset
		}
	}
	return nil
}

type introspectResult struct {
	report *userdto.IntrospectResponse
	err    error
}

// Introspect validates token and returns the identity report. cached is
// true when the report came from Redis. A suspended account returns the
// report together with ACCOUNT_SUSPENDED.
func (s *UserService) Introspect(ctx context.Context, token string) (*userdto.IntrospectResponse, bool, error) {
	if err := ValidateToken(token); err != nil {
		return nil, false, err
	}
	h := TokenHash(token)

	raw, state, err := s.cache.LookupIntrospect(ctx, h)
	if err != nil {
		s.log.Warn().Err(err).Msg("introspect cache lookup failed, continuing as miss")
	}
	switch state {
	case StateInactive:
		return nil, false, response.ErrTokenInactive.With(nil, map[string]any{"reason": "negative_cache"})
	case StateHit:
		var report userdto.IntrospectResponse
		if uerr := json.Unmarshal(raw, &report); uerr == nil {
			if !report.IsActive() {
				return &report, true, response.ErrAccountSuspended
			}
			return &report, true, nil
		}
		s.log.Warn().Str("hash", h).Msg("corrupt cached report, refetching")
	}

	v, _, _ := s.sf.Do(h, func() (any, error) {
		return s.fetch(ctx, token, h), nil
	})
	res := v.(introspectResult)
	if res.err != nil {
		return res.report, false, res.err
	}
	return res.report, false, nil
}

// fetch is the cache-miss path shared by all concurrent callers of one token.
func (s *UserService) fetch(ctx context.Context, token, h string) introspectResult {
	claims, err := s.zitadel.UserInfo(ctx, token)
	if err != nil {
		if errors.Is(err, response.ErrTokenInactive) {
			// Only a definitive 400/401 writes the negative marker.
			s.detached(ctx, func(bg context.Context) {
				if cerr := s.cache.MarkInactive(bg, h, s.inactiveTTL); cerr != nil {
					s.log.Warn().Err(cerr).Msg("mark inactive failed")
				}
			})
		}
		// ZITADEL_UNAVAILABLE / breaker open: write nothing at all.
		return introspectResult{err: err}
	}

	if s.issuer != "" && claims.Iss != "" && strings.TrimRight(claims.Iss, "/") != s.issuer {
		return introspectResult{err: response.ErrIssuerMismatch.With(nil, map[string]any{"issuer": claims.Iss})}
	}

	report := userdto.ReportFromClaims(claims)
	payload := userdto.SyncPayloadFromClaims(claims, report, s.issuer)
	encoded, err := json.Marshal(report)
	if err != nil {
		return introspectResult{err: fmt.Errorf("encode report: %w", err)}
	}

	// A client disconnect must not skip the cache write or the notify.
	s.detached(ctx, func(bg context.Context) {
		if cerr := s.cache.SetIntrospect(bg, h, encoded, s.introspectTTL); cerr != nil {
			s.log.Warn().Err(cerr).Msg("cache report failed")
		}
		if nerr := s.notifier.Notify(bg, payload); nerr != nil {
			s.log.Warn().Err(nerr).Str("sub", claims.Sub).Msg("sync notify failed")
		}
	})

	if !report.IsActive() {
		return introspectResult{report: report, err: response.ErrAccountSuspended.With(nil, map[string]any{"status_name": report.MainAccount.StatusName})}
	}
	return introspectResult{report: report}
}

// detached schedules f on a context that survives the caller's cancellation
// but is bounded by the sync budget.
func (s *UserService) detached(ctx context.Context, f func(context.Context)) {
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.syncBudget)
	s.run(func() {
		defer cancel()
		f(bg)
	})
}

// Authenticate implements middleware.Authenticator on top of Introspect.
func (s *UserService) Authenticate(ctx context.Context, token string) (types.AuthUser, error) {
	report, _, err := s.Introspect(ctx, token)
	if err != nil {
		return types.AuthUser{}, err
	}
	return types.AuthUser{
		Sub: report.Identity.Sub, Email: report.Identity.Email,
		Roles: report.Roles, IsAdmin: report.IsAdmin,
	}, nil
}
