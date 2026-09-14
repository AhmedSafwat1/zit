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
	userdto "github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	userhandler "github.com/4jawaly/identity-gateway/internal/modules/user/handlers"
	userroute "github.com/4jawaly/identity-gateway/internal/modules/user/routes"
	"github.com/4jawaly/identity-gateway/internal/modules/user/tests/mocks"
	"github.com/4jawaly/identity-gateway/internal/response"
)

func newRouter(t *testing.T) (*gin.Engine, *mocks.UserService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := mocks.NewUserService(t)
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Recovery())
	userroute.Register(r.Group("/api/v1"), userhandler.NewUserHandler(svc))
	return r, svc
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
			r, svc := newRouter(t)
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

func ptr[T any](v T) *T { return &v }
