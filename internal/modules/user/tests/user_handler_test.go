package tests

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/4jawaly/identity-gateway/internal/middleware"
	cmocks "github.com/4jawaly/identity-gateway/internal/mocks"
	"github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	"github.com/4jawaly/identity-gateway/internal/modules/user/handlers"
	"github.com/4jawaly/identity-gateway/internal/modules/user/routes"
	"github.com/4jawaly/identity-gateway/internal/modules/user/tests/mocks"
	"github.com/4jawaly/identity-gateway/internal/response"
	"github.com/4jawaly/identity-gateway/internal/types"
)

func newRouter(t *testing.T) (*gin.Engine, *mocks.UserService, *cmocks.Authenticator) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := mocks.NewUserService(t)
	authn := cmocks.NewAuthenticator(t)
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Recovery())
	routes.Register(r.Group("/api/v1"), handlers.NewUserHandler(svc), authn, true)
	return r, svc, authn
}

func do(t *testing.T, r http.Handler, method, path, bearer string) (*httptest.ResponseRecorder, response.Envelope) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var env response.Envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env), rec.Body.String())
	assert.NotEmpty(t, rec.Header().Get(middleware.HeaderRequestID))
	assert.NotContains(t, rec.Body.String(), `"request_id"`)
	assert.NotContains(t, rec.Body.String(), `"timestamp"`)
	return rec, env
}

func adminPrincipal() types.AuthUser {
	return types.AuthUser{Sub: "355646255235338042", Roles: []string{"ADMIN"}, IsAdmin: true}
}

func userPrincipal() types.AuthUser {
	return types.AuthUser{Sub: "355646999999999999", Roles: []string{"USER"}}
}

func sampleUser() dto.UserResponse {
	return dto.UserResponse{Sub: "355646255235338042", Email: "admin@rawazin.example", Roles: []string{"ADMIN"}, IsAdmin: true, MainAccount: dto.MainAccountPayload{StatusName: "Active"}}
}

func TestUserHandler_Introspect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		bearer     string
		setup      func(svc *mocks.UserService)
		wantStatus int
		wantCode   string
		wantCached *bool
		wantData   bool
	}{
		{name: "missing header is TOKEN_MISSING and never calls the service", bearer: "", setup: func(*mocks.UserService) {}, wantStatus: 401, wantCode: "TOKEN_MISSING"},
		{name: "invalid token is TOKEN_INVALID", bearer: "bad", setup: func(s *mocks.UserService) {
			s.EXPECT().Introspect(mock.Anything, "bad").Return(nil, false, response.ErrTokenInvalid).Once()
		}, wantStatus: 401, wantCode: "TOKEN_INVALID"},
		{name: "inactive token is TOKEN_INACTIVE", bearer: "tok", setup: func(s *mocks.UserService) {
			s.EXPECT().Introspect(mock.Anything, "tok").Return(nil, false, response.ErrTokenInactive).Once()
		}, wantStatus: 401, wantCode: "TOKEN_INACTIVE"},
		{name: "issuer mismatch is ISSUER_MISMATCH", bearer: "tok", setup: func(s *mocks.UserService) {
			s.EXPECT().Introspect(mock.Anything, "tok").Return(nil, false, response.ErrIssuerMismatch).Once()
		}, wantStatus: 401, wantCode: "ISSUER_MISMATCH"},
		{name: "suspended account is 403 ACCOUNT_SUSPENDED with the report", bearer: "tok", setup: func(s *mocks.UserService) {
			s.EXPECT().Introspect(mock.Anything, "tok").Return(adminReport(), false, response.ErrAccountSuspended).Once()
		}, wantStatus: 403, wantCode: "ACCOUNT_SUSPENDED", wantData: true},
		{name: "zitadel down is 503 ZITADEL_UNAVAILABLE", bearer: "tok", setup: func(s *mocks.UserService) {
			s.EXPECT().Introspect(mock.Anything, "tok").Return(nil, false, response.ErrZitadelUnavailable).Once()
		}, wantStatus: 503, wantCode: "ZITADEL_UNAVAILABLE"},
		{name: "unknown error is 500 INTERNAL_ERROR without leaking the cause", bearer: "tok", setup: func(s *mocks.UserService) {
			s.EXPECT().Introspect(mock.Anything, "tok").Return(nil, false, errors.New("secret detail")).Once()
		}, wantStatus: 500, wantCode: "INTERNAL_ERROR"},
		{name: "cached report is 200 with meta.cached true", bearer: "tok", setup: func(s *mocks.UserService) {
			s.EXPECT().Introspect(mock.Anything, "tok").Return(adminReport(), true, nil).Once()
		}, wantStatus: 200, wantCached: ptr(true), wantData: true},
		{name: "fresh report is 200 with meta.cached false", bearer: "tok", setup: func(s *mocks.UserService) {
			s.EXPECT().Introspect(mock.Anything, "tok").Return(adminReport(), false, nil).Once()
		}, wantStatus: 200, wantCached: ptr(false), wantData: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, svc, _ := newRouter(t)
			tt.setup(svc)
			rec, env := do(t, r, http.MethodPost, "/api/v1/users/introspect", tt.bearer)
			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.NotContains(t, rec.Body.String(), "secret detail")
			if tt.wantCode != "" {
				require.NotNil(t, env.Error)
				assert.Equal(t, tt.wantCode, env.Error.Code)
				assert.False(t, env.Success)
			} else {
				assert.Nil(t, env.Error)
				assert.True(t, env.Success)
			}
			if tt.wantCached != nil {
				require.NotNil(t, env.Meta)
				require.NotNil(t, env.Meta.Cached)
				assert.Equal(t, *tt.wantCached, *env.Meta.Cached)
			}
			if tt.wantData {
				require.NotNil(t, env.Data)
				data, _ := env.Data.(map[string]any)
				assert.Equal(t, "355646255235338042", data["identity"].(map[string]any)["sub"])
			} else {
				assert.Nil(t, env.Data)
			}
		})
	}
}

func TestUserHandler_AuthenticatedReads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		path       string
		bearer     string
		setup      func(svc *mocks.UserService, a *cmocks.Authenticator)
		wantStatus int
		wantCode   string
	}{
		// /users/me
		{name: "me without token is TOKEN_MISSING", method: "GET", path: "/api/v1/users/me", setup: func(*mocks.UserService, *cmocks.Authenticator) {}, wantStatus: 401, wantCode: "TOKEN_MISSING"},
		{name: "me with inactive token is TOKEN_INACTIVE", method: "GET", path: "/api/v1/users/me", bearer: "tok", setup: func(_ *mocks.UserService, a *cmocks.Authenticator) {
			a.EXPECT().Authenticate(mock.Anything, "tok").Return(types.AuthUser{}, response.ErrTokenInactive).Once()
		}, wantStatus: 401, wantCode: "TOKEN_INACTIVE"},
		{name: "me with suspended account is ACCOUNT_SUSPENDED", method: "GET", path: "/api/v1/users/me", bearer: "tok", setup: func(_ *mocks.UserService, a *cmocks.Authenticator) {
			a.EXPECT().Authenticate(mock.Anything, "tok").Return(types.AuthUser{}, response.ErrAccountSuspended).Once()
		}, wantStatus: 403, wantCode: "ACCOUNT_SUSPENDED"},
		{name: "me when zitadel is down is ZITADEL_UNAVAILABLE", method: "GET", path: "/api/v1/users/me", bearer: "tok", setup: func(_ *mocks.UserService, a *cmocks.Authenticator) {
			a.EXPECT().Authenticate(mock.Anything, "tok").Return(types.AuthUser{}, response.ErrZitadelUnavailable).Once()
		}, wantStatus: 503, wantCode: "ZITADEL_UNAVAILABLE"},
		{name: "me not synced yet is NOT_FOUND", method: "GET", path: "/api/v1/users/me", bearer: "tok", setup: func(s *mocks.UserService, a *cmocks.Authenticator) {
			a.EXPECT().Authenticate(mock.Anything, "tok").Return(userPrincipal(), nil).Once()
			s.EXPECT().GetUser(mock.Anything, userPrincipal().Sub).Return(nil, false, response.ErrNotFound).Once()
		}, wantStatus: 404, wantCode: "NOT_FOUND"},
		{name: "me returns the caller's row", method: "GET", path: "/api/v1/users/me", bearer: "tok", setup: func(s *mocks.UserService, a *cmocks.Authenticator) {
			a.EXPECT().Authenticate(mock.Anything, "tok").Return(adminPrincipal(), nil).Once()
			u := sampleUser()
			s.EXPECT().GetUser(mock.Anything, adminPrincipal().Sub).Return(&u, true, nil).Once()
		}, wantStatus: 200},
		// /users
		{name: "list as non-admin is FORBIDDEN", method: "GET", path: "/api/v1/users", bearer: "tok", setup: func(_ *mocks.UserService, a *cmocks.Authenticator) {
			a.EXPECT().Authenticate(mock.Anything, "tok").Return(userPrincipal(), nil).Once()
		}, wantStatus: 403, wantCode: "FORBIDDEN"},
		{name: "list with per_page over 100 is VALIDATION_ERROR", method: "GET", path: "/api/v1/users?per_page=1000", bearer: "tok", setup: func(_ *mocks.UserService, a *cmocks.Authenticator) {
			a.EXPECT().Authenticate(mock.Anything, "tok").Return(adminPrincipal(), nil).Once()
		}, wantStatus: 422, wantCode: "VALIDATION_ERROR"},
		{name: "list as admin is paginated", method: "GET", path: "/api/v1/users?page=2&per_page=10", bearer: "tok", setup: func(s *mocks.UserService, a *cmocks.Authenticator) {
			a.EXPECT().Authenticate(mock.Anything, "tok").Return(adminPrincipal(), nil).Once()
			s.EXPECT().ListUsers(mock.Anything, dto.ListUsersQuery{Page: 2, PerPage: 10}).Return([]dto.UserResponse{sampleUser()}, 25, nil).Once()
		}, wantStatus: 200},
		{name: "list db failure is INTERNAL_ERROR", method: "GET", path: "/api/v1/users", bearer: "tok", setup: func(s *mocks.UserService, a *cmocks.Authenticator) {
			a.EXPECT().Authenticate(mock.Anything, "tok").Return(adminPrincipal(), nil).Once()
			s.EXPECT().ListUsers(mock.Anything, mock.Anything).Return(nil, 0, errDB).Once()
		}, wantStatus: 500, wantCode: "INTERNAL_ERROR"},
		// /users/:sub
		{name: "get as non-admin is FORBIDDEN", method: "GET", path: "/api/v1/users/355646255235338042", bearer: "tok", setup: func(_ *mocks.UserService, a *cmocks.Authenticator) {
			a.EXPECT().Authenticate(mock.Anything, "tok").Return(userPrincipal(), nil).Once()
		}, wantStatus: 403, wantCode: "FORBIDDEN"},
		{name: "get unknown sub is NOT_FOUND", method: "GET", path: "/api/v1/users/0", bearer: "tok", setup: func(s *mocks.UserService, a *cmocks.Authenticator) {
			a.EXPECT().Authenticate(mock.Anything, "tok").Return(adminPrincipal(), nil).Once()
			s.EXPECT().GetUser(mock.Anything, "0").Return(nil, false, response.ErrNotFound).Once()
		}, wantStatus: 404, wantCode: "NOT_FOUND"},
		{name: "get as admin returns the row", method: "GET", path: "/api/v1/users/355646255235338042", bearer: "tok", setup: func(s *mocks.UserService, a *cmocks.Authenticator) {
			a.EXPECT().Authenticate(mock.Anything, "tok").Return(adminPrincipal(), nil).Once()
			u := sampleUser()
			s.EXPECT().GetUser(mock.Anything, "355646255235338042").Return(&u, false, nil).Once()
		}, wantStatus: 200},
		// /users/:sub/sub-accounts
		{name: "sub-accounts without token is TOKEN_MISSING", method: "GET", path: "/api/v1/users/1/sub-accounts", setup: func(*mocks.UserService, *cmocks.Authenticator) {}, wantStatus: 401, wantCode: "TOKEN_MISSING"},
		{name: "sub-accounts of unknown sub is NOT_FOUND", method: "GET", path: "/api/v1/users/0/sub-accounts", bearer: "tok", setup: func(s *mocks.UserService, a *cmocks.Authenticator) {
			a.EXPECT().Authenticate(mock.Anything, "tok").Return(userPrincipal(), nil).Once()
			s.EXPECT().ListSubAccounts(mock.Anything, "0").Return(nil, response.ErrNotFound).Once()
		}, wantStatus: 404, wantCode: "NOT_FOUND"},
		{name: "sub-accounts lists children for any authenticated user", method: "GET", path: "/api/v1/users/1/sub-accounts", bearer: "tok", setup: func(s *mocks.UserService, a *cmocks.Authenticator) {
			a.EXPECT().Authenticate(mock.Anything, "tok").Return(userPrincipal(), nil).Once()
			s.EXPECT().ListSubAccounts(mock.Anything, "1").Return([]dto.UserResponse{sampleUser()}, nil).Once()
		}, wantStatus: 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, svc, authn := newRouter(t)
			tt.setup(svc, authn)
			rec, env := do(t, r, tt.method, tt.path, tt.bearer)
			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			if tt.wantCode != "" {
				require.NotNil(t, env.Error)
				assert.Equal(t, tt.wantCode, env.Error.Code)
				assert.False(t, env.Success)
				return
			}
			assert.True(t, env.Success)
			assert.NotNil(t, env.Data)
		})
	}
}

func TestUserHandler_ListMeta(t *testing.T) {
	t.Parallel()
	r, svc, authn := newRouter(t)
	authn.EXPECT().Authenticate(mock.Anything, "tok").Return(adminPrincipal(), nil).Once()
	svc.EXPECT().ListUsers(mock.Anything, dto.ListUsersQuery{Page: 3, PerPage: 10}).Return([]dto.UserResponse{}, 25, nil).Once()
	_, env := do(t, r, http.MethodGet, "/api/v1/users?page=3&per_page=10", "tok")
	require.NotNil(t, env.Meta)
	assert.Equal(t, 3, env.Meta.Page)
	assert.Equal(t, 10, env.Meta.PerPage)
	assert.Equal(t, int64(25), env.Meta.Total)
	assert.Equal(t, 3, env.Meta.TotalPages)
}

func ptr[T any](v T) *T { return &v }
