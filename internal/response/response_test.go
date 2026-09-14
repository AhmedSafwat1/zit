package response_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/4jawaly/identity-gateway/internal/reqctx"
	"github.com/4jawaly/identity-gateway/internal/response"
)

func run(t *testing.T, h gin.HandlerFunc) (*httptest.ResponseRecorder, response.Envelope) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request = req.WithContext(reqctx.WithRequestID(req.Context(), "req-1"))
	h(c)
	var env response.Envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	return rec, env
}

func TestFail(t *testing.T) {
	// Not parallel: TestFail_Debug flips the package-level debug switch.
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantDetails map[string]any
	}{
		{name: "TOKEN_MISSING is 401", err: response.ErrTokenMissing, wantStatus: 401, wantCode: "TOKEN_MISSING"},
		{name: "TOKEN_INVALID is 401", err: response.ErrTokenInvalid, wantStatus: 401, wantCode: "TOKEN_INVALID"},
		{name: "TOKEN_INACTIVE is 401", err: response.ErrTokenInactive, wantStatus: 401, wantCode: "TOKEN_INACTIVE"},
		{name: "ISSUER_MISMATCH is 401", err: response.ErrIssuerMismatch, wantStatus: 401, wantCode: "ISSUER_MISMATCH"},
		{name: "ACCOUNT_SUSPENDED is 403", err: response.ErrAccountSuspended, wantStatus: 403, wantCode: "ACCOUNT_SUSPENDED"},
		{name: "FORBIDDEN is 403", err: response.ErrForbidden, wantStatus: 403, wantCode: "FORBIDDEN"},
		{name: "VALIDATION_ERROR is 422", err: response.ErrValidation, wantStatus: 422, wantCode: "VALIDATION_ERROR"},
		{name: "NOT_FOUND is 404", err: response.ErrNotFound, wantStatus: 404, wantCode: "NOT_FOUND"},
		{name: "CONFLICT is 409", err: response.ErrConflict, wantStatus: 409, wantCode: "CONFLICT"},
		{name: "RATE_LIMITED is 429", err: response.ErrRateLimited, wantStatus: 429, wantCode: "RATE_LIMITED"},
		{name: "ZITADEL_UNAVAILABLE is 503", err: response.ErrZitadelUnavailable, wantStatus: 503, wantCode: "ZITADEL_UNAVAILABLE"},
		{name: "INTERNAL_ERROR is 500", err: response.ErrInternal, wantStatus: 500, wantCode: "INTERNAL_ERROR"},
		{name: "unknown error becomes INTERNAL_ERROR and hides the cause", err: errors.New("secret db password leaked"), wantStatus: 500, wantCode: "INTERNAL_ERROR"},
		{name: "wrapped app error keeps code and details", err: errors.Join(errors.New("ctx"), response.ErrValidation.With(nil, map[string]any{"field": "email"})), wantStatus: 422, wantCode: "VALIDATION_ERROR", wantDetails: map[string]any{"field": "email"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, env := run(t, func(c *gin.Context) { response.Fail(c, tt.err) })
			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.False(t, env.Success)
			require.NotNil(t, env.Error)
			assert.Equal(t, tt.wantCode, env.Error.Code)
			assert.Nil(t, env.Error.Debug, "no debug block unless enabled")
			assert.NotContains(t, rec.Body.String(), "secret")
			assert.NotContains(t, rec.Body.String(), `"request_id"`)
			assert.NotContains(t, rec.Body.String(), `"timestamp"`)
			if tt.wantDetails != nil {
				assert.Equal(t, tt.wantDetails, env.Error.Details)
			}
		})
	}
}

func TestFail_Debug(t *testing.T) {
	response.SetDebug(true)
	t.Cleanup(func() { response.SetDebug(false) })

	tests := []struct {
		name      string
		h         gin.HandlerFunc
		wantCause string
		wantStack bool
	}{
		{name: "internal cause is echoed", h: func(c *gin.Context) { response.Fail(c, errors.New("dial tcp: refused")) }, wantCause: "dial tcp: refused"},
		{name: "5xx app error cause is echoed", h: func(c *gin.Context) {
			response.Fail(c, response.ErrZitadelUnavailable.With(errors.New("503 upstream"), nil))
		}, wantCause: "ZITADEL_UNAVAILABLE: identity provider is unavailable: 503 upstream"},
		{name: "panic carries the stack", h: func(c *gin.Context) {
			response.FailPanic(c, response.ErrInternal.With(errors.New("panic: boom"), nil), "goroutine 1 [running]")
		}, wantCause: "INTERNAL_ERROR: internal error: panic: boom", wantStack: true},
	}
	t.Run("4xx never carries debug even when enabled", func(t *testing.T) {
		_, env := run(t, func(c *gin.Context) {
			response.Fail(c, response.ErrTokenInactive.With(errors.New("userinfo status 401"), nil))
		})
		require.NotNil(t, env.Error)
		assert.Nil(t, env.Error.Debug)
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, env := run(t, tt.h)
			require.NotNil(t, env.Error)
			require.NotNil(t, env.Error.Debug)
			assert.Equal(t, tt.wantCause, env.Error.Debug.Cause)
			if tt.wantStack {
				assert.NotEmpty(t, env.Error.Debug.Stack)
			} else {
				assert.Empty(t, env.Error.Debug.Stack)
			}
		})
	}
}

func TestSuccessHelpers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		h          gin.HandlerFunc
		wantStatus int
		wantMeta   func(t *testing.T, m *response.Meta)
	}{
		{name: "OK is 200", h: func(c *gin.Context) { response.OK(c, "x") }, wantStatus: 200},
		{name: "Created is 201", h: func(c *gin.Context) { response.Created(c, "x") }, wantStatus: 201},
		{name: "Cached sets meta.cached", h: func(c *gin.Context) { response.Cached(c, "x", true) }, wantStatus: 200,
			wantMeta: func(t *testing.T, m *response.Meta) { require.NotNil(t, m.Cached); assert.True(t, *m.Cached) }},
		{name: "Paginated computes total pages", h: func(c *gin.Context) { response.Paginated(c, []int{}, 2, 20, 41) }, wantStatus: 200,
			wantMeta: func(t *testing.T, m *response.Meta) {
				assert.Equal(t, 2, m.Page)
				assert.Equal(t, 20, m.PerPage)
				assert.Equal(t, int64(41), m.Total)
				assert.Equal(t, 3, m.TotalPages)
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec, env := run(t, tt.h)
			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.True(t, env.Success)
			assert.Nil(t, env.Error)
			assert.NotContains(t, rec.Body.String(), `"request_id"`)
			if tt.wantMeta != nil {
				require.NotNil(t, env.Meta)
				tt.wantMeta(t, env.Meta)
			}
		})
	}
}

func TestAppError_With(t *testing.T) {
	t.Parallel()
	cause := errors.New("boom")
	derived := response.ErrNotFound.With(cause, map[string]any{"id": 1})

	assert.Nil(t, response.ErrNotFound.Cause(), "sentinel is never mutated")
	assert.Nil(t, response.ErrNotFound.Details, "sentinel is never mutated")
	assert.ErrorIs(t, derived, response.ErrNotFound)
	assert.ErrorIs(t, derived, cause)
	assert.Equal(t, "NOT_FOUND", derived.Code)
	assert.NotErrorIs(t, derived, response.ErrConflict)
}
