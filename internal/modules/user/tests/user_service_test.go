package tests

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	"github.com/4jawaly/identity-gateway/internal/modules/user/services"
	"github.com/4jawaly/identity-gateway/internal/modules/user/tests/mocks"
	"github.com/4jawaly/identity-gateway/internal/response"
	"github.com/4jawaly/identity-gateway/pkg/circuitbreaker"
	"github.com/4jawaly/identity-gateway/pkg/zitadel"
)

func TestUserService_Introspect(t *testing.T) {
	t.Parallel()

	const token = "tok_valid"
	hash := services.TokenHash(token)

	tests := []struct {
		name       string
		token      string
		setup      func(t *testing.T, c *mocks.Cache, z *mocks.ZitadelClient, n *mocks.SyncNotifier)
		want       *dto.IntrospectResponse
		wantCached bool
		wantErr    error
		wantReport bool // report returned alongside wantErr
		notErr     error
	}{
		{
			name:  "cache hit returns without calling zitadel",
			token: token,
			setup: func(t *testing.T, c *mocks.Cache, z *mocks.ZitadelClient, n *mocks.SyncNotifier) {
				c.EXPECT().LookupIntrospect(mock.Anything, hash).
					Return(mustJSON(t, loadFixture[dto.IntrospectResponse](t, "report_admin.json")), services.StateHit, nil).Once()
				// z and n get NO expectations — any call to them fails the test
			},
			want:       adminReport(),
			wantCached: true,
		},
		{
			name:  "inactive hit returns TOKEN_INACTIVE without calling zitadel",
			token: token,
			setup: func(t *testing.T, c *mocks.Cache, z *mocks.ZitadelClient, n *mocks.SyncNotifier) {
				c.EXPECT().LookupIntrospect(mock.Anything, hash).Return(nil, services.StateInactive, nil).Once()
			},
			wantErr: response.ErrTokenInactive,
		},
		{
			name:  "miss populates cache and notifies",
			token: token,
			setup: func(t *testing.T, c *mocks.Cache, z *mocks.ZitadelClient, n *mocks.SyncNotifier) {
				c.EXPECT().LookupIntrospect(mock.Anything, hash).Return(nil, services.StateMiss, nil).Once()
				z.EXPECT().UserInfo(mock.Anything, token).Return(loadClaims(t, "userinfo_admin.json"), nil).Once()
				c.EXPECT().SetIntrospect(mock.Anything, hash, mustJSON(t, adminReport()), 120*time.Second).Return(nil).Once()
				n.EXPECT().Notify(mock.Anything, mock.MatchedBy(func(p dto.UserSyncPayload) bool {
					return p.Sub == "355646255235338042" && p.Issuer == testIssuer && len(p.Origin) > 0 &&
						p.MainAccount.AccountTypeName == "Company"
				})).Return(nil).Once()
			},
			want: adminReport(),
		},
		{
			name:  "zitadel 401 marks inactive and writes nothing else",
			token: token,
			setup: func(t *testing.T, c *mocks.Cache, z *mocks.ZitadelClient, n *mocks.SyncNotifier) {
				c.EXPECT().LookupIntrospect(mock.Anything, hash).Return(nil, services.StateMiss, nil).Once()
				z.EXPECT().UserInfo(mock.Anything, token).Return(nil, response.ErrTokenInactive.With(errors.New("401"), nil)).Once()
				c.EXPECT().MarkInactive(mock.Anything, hash, 30*time.Second).Return(nil).Once()
			},
			wantErr: response.ErrTokenInactive,
		},
		{
			name:  "zitadel 503 writes nothing",
			token: token,
			setup: func(t *testing.T, c *mocks.Cache, z *mocks.ZitadelClient, n *mocks.SyncNotifier) {
				c.EXPECT().LookupIntrospect(mock.Anything, hash).Return(nil, services.StateMiss, nil).Once()
				z.EXPECT().UserInfo(mock.Anything, token).Return(nil, response.ErrZitadelUnavailable.With(errors.New("503"), nil)).Once()
			},
			wantErr: response.ErrZitadelUnavailable,
		},
		{
			name:  "breaker open writes nothing and is never TOKEN_INACTIVE",
			token: token,
			setup: func(t *testing.T, c *mocks.Cache, z *mocks.ZitadelClient, n *mocks.SyncNotifier) {
				c.EXPECT().LookupIntrospect(mock.Anything, hash).Return(nil, services.StateMiss, nil).Once()
				z.EXPECT().UserInfo(mock.Anything, token).
					Return(nil, response.ErrZitadelUnavailable.With(circuitbreaker.ErrOpen, nil)).Once()
				// no SetIntrospect, no MarkInactive, no Notify expected
			},
			wantErr: response.ErrZitadelUnavailable,
			notErr:  response.ErrTokenInactive,
		},
		{
			name:  "issuer mismatch is rejected and nothing is written",
			token: token,
			setup: func(t *testing.T, c *mocks.Cache, z *mocks.ZitadelClient, n *mocks.SyncNotifier) {
				c.EXPECT().LookupIntrospect(mock.Anything, hash).Return(nil, services.StateMiss, nil).Once()
				claims := loadClaims(t, "userinfo_admin.json")
				claims.Iss = "https://evil.example"
				z.EXPECT().UserInfo(mock.Anything, token).Return(claims, nil).Once()
			},
			wantErr: response.ErrIssuerMismatch,
		},
		{
			name:  "issuer with trailing slash is accepted",
			token: token,
			setup: func(t *testing.T, c *mocks.Cache, z *mocks.ZitadelClient, n *mocks.SyncNotifier) {
				c.EXPECT().LookupIntrospect(mock.Anything, hash).Return(nil, services.StateMiss, nil).Once()
				claims := loadClaims(t, "userinfo_admin.json")
				claims.Iss = testIssuer + "/"
				z.EXPECT().UserInfo(mock.Anything, token).Return(claims, nil).Once()
				c.EXPECT().SetIntrospect(mock.Anything, hash, mock.Anything, mock.Anything).Return(nil).Once()
				n.EXPECT().Notify(mock.Anything, mock.Anything).Return(nil).Once()
			},
			want: adminReport(),
		},
		{
			name:  "suspended account is cached, notified and returns ACCOUNT_SUSPENDED with the report",
			token: token,
			setup: func(t *testing.T, c *mocks.Cache, z *mocks.ZitadelClient, n *mocks.SyncNotifier) {
				c.EXPECT().LookupIntrospect(mock.Anything, hash).Return(nil, services.StateMiss, nil).Once()
				z.EXPECT().UserInfo(mock.Anything, token).Return(loadClaims(t, "userinfo_suspended.json"), nil).Once()
				c.EXPECT().SetIntrospect(mock.Anything, hash, mock.Anything, mock.Anything).Return(nil).Once()
				n.EXPECT().Notify(mock.Anything, mock.MatchedBy(func(p dto.UserSyncPayload) bool {
					return p.MainAccount.StatusName == "Suspended"
				})).Return(nil).Once()
			},
			wantErr:    response.ErrAccountSuspended,
			wantReport: true,
		},
		{
			name:  "suspended report served from cache still returns ACCOUNT_SUSPENDED",
			token: token,
			setup: func(t *testing.T, c *mocks.Cache, z *mocks.ZitadelClient, n *mocks.SyncNotifier) {
				r := adminReport()
				r.MainAccount.StatusName = "Suspended"
				c.EXPECT().LookupIntrospect(mock.Anything, hash).Return(mustJSON(t, r), services.StateHit, nil).Once()
			},
			wantErr:    response.ErrAccountSuspended,
			wantReport: true,
		},
		{
			name:    "empty token is rejected before any mock is touched",
			token:   "",
			setup:   func(*testing.T, *mocks.Cache, *mocks.ZitadelClient, *mocks.SyncNotifier) {},
			wantErr: response.ErrTokenInvalid,
		},
		{
			name:    "oversize token is rejected before any mock is touched",
			token:   strings.Repeat("a", 8*1024+1),
			setup:   func(*testing.T, *mocks.Cache, *mocks.ZitadelClient, *mocks.SyncNotifier) {},
			wantErr: response.ErrTokenInvalid,
		},
		{
			name:    "token with characters outside the charset is rejected before any mock is touched",
			token:   "tok<script>",
			setup:   func(*testing.T, *mocks.Cache, *mocks.ZitadelClient, *mocks.SyncNotifier) {},
			wantErr: response.ErrTokenInvalid,
		},
		{
			name:  "redis error falls through to zitadel instead of failing",
			token: token,
			setup: func(t *testing.T, c *mocks.Cache, z *mocks.ZitadelClient, n *mocks.SyncNotifier) {
				c.EXPECT().LookupIntrospect(mock.Anything, hash).Return(nil, services.StateMiss, errors.New("redis: connection refused")).Once()
				z.EXPECT().UserInfo(mock.Anything, token).Return(loadClaims(t, "userinfo_admin.json"), nil).Once()
				c.EXPECT().SetIntrospect(mock.Anything, hash, mock.Anything, mock.Anything).Return(errors.New("redis: still down")).Once()
				n.EXPECT().Notify(mock.Anything, mock.Anything).Return(nil).Once()
			},
			want: adminReport(),
		},
		{
			name:  "corrupt cached report is refetched",
			token: token,
			setup: func(t *testing.T, c *mocks.Cache, z *mocks.ZitadelClient, n *mocks.SyncNotifier) {
				c.EXPECT().LookupIntrospect(mock.Anything, hash).Return([]byte("{not json"), services.StateHit, nil).Once()
				z.EXPECT().UserInfo(mock.Anything, token).Return(loadClaims(t, "userinfo_admin.json"), nil).Once()
				c.EXPECT().SetIntrospect(mock.Anything, hash, mock.Anything, mock.Anything).Return(nil).Once()
				n.EXPECT().Notify(mock.Anything, mock.Anything).Return(nil).Once()
			},
			want: adminReport(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, c, z, n := newTestService(t)
			tt.setup(t, c, z, n)

			got, cached, err := svc.Introspect(context.Background(), tt.token)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				if tt.notErr != nil {
					assert.NotErrorIs(t, err, tt.notErr)
				}
				if tt.wantReport {
					assert.NotNil(t, got)
				} else {
					assert.Nil(t, got)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantCached, cached)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUserService_Introspect_ConcurrentSameToken(t *testing.T) {
	t.Parallel()

	const token = "tok_shared"
	const workers = 25
	hash := services.TokenHash(token)

	svc, c, z, n := newTestService(t)

	var arrived sync.WaitGroup
	arrived.Add(workers)
	release := make(chan struct{})

	c.EXPECT().LookupIntrospect(mock.Anything, hash).
		RunAndReturn(func(context.Context, string) ([]byte, services.State, error) {
			arrived.Done()
			return nil, services.StateMiss, nil
		}).Times(workers)
	z.EXPECT().UserInfo(mock.Anything, token).
		RunAndReturn(func(context.Context, string) (*zitadel.Claims, error) {
			<-release
			return loadClaims(t, "userinfo_admin.json"), nil
		}).Once()
	c.EXPECT().SetIntrospect(mock.Anything, hash, mock.Anything, mock.Anything).Return(nil).Once()
	n.EXPECT().Notify(mock.Anything, mock.Anything).Return(nil).Once()

	go func() {
		arrived.Wait()
		time.Sleep(20 * time.Millisecond) // let every goroutine reach singleflight
		close(release)
	}()

	var wg sync.WaitGroup
	results := make([]*dto.IntrospectResponse, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _, errs[i] = svc.Introspect(context.Background(), token)
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		require.NoError(t, errs[i])
		assert.Equal(t, adminReport(), results[i])
	}
}

func TestUserService_Authenticate(t *testing.T) {
	t.Parallel()
	const token = "tok_auth"
	hash := services.TokenHash(token)

	tests := []struct {
		name    string
		setup   func(t *testing.T, c *mocks.Cache)
		wantSub string
		wantErr error
	}{
		{
			name: "cached report becomes a principal",
			setup: func(t *testing.T, c *mocks.Cache) {
				c.EXPECT().LookupIntrospect(mock.Anything, hash).Return(mustJSON(t, adminReport()), services.StateHit, nil).Once()
			},
			wantSub: "355646255235338042",
		},
		{
			name: "inactive token is rejected",
			setup: func(t *testing.T, c *mocks.Cache) {
				c.EXPECT().LookupIntrospect(mock.Anything, hash).Return(nil, services.StateInactive, nil).Once()
			},
			wantErr: response.ErrTokenInactive,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, c, _, _ := newTestService(t)
			tt.setup(t, c)
			p, err := svc.Authenticate(context.Background(), token)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantSub, p.Sub)
			assert.True(t, p.IsAdmin)
			assert.True(t, p.HasRole("ADMIN"))
		})
	}
}
