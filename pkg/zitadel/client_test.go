package zitadel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/4jawaly/identity-gateway/internal/response"
	"github.com/4jawaly/identity-gateway/pkg/circuitbreaker"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClient_UserInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		body      string
		transport error
		wantErr   error
		wantCalls int32
		wantSub   string
	}{
		{name: "200 decodes claims", status: 200, body: `{"sub":"1"}`, wantCalls: 1, wantSub: "1"},
		{name: "401 maps to TOKEN_INACTIVE without retry", status: 401, wantErr: response.ErrTokenInactive, wantCalls: 1},
		{name: "400 maps to TOKEN_INACTIVE without retry", status: 400, wantErr: response.ErrTokenInactive, wantCalls: 1},
		{name: "403 is NOT inactive, maps to ZITADEL_UNAVAILABLE without retry", status: 403, wantErr: response.ErrZitadelUnavailable, wantCalls: 1},
		{name: "404 maps to ZITADEL_UNAVAILABLE without retry", status: 404, wantErr: response.ErrZitadelUnavailable, wantCalls: 1},
		{name: "503 maps to ZITADEL_UNAVAILABLE and retries once", status: 503, wantErr: response.ErrZitadelUnavailable, wantCalls: 2},
		{name: "bad json maps to ZITADEL_UNAVAILABLE", status: 200, body: `{`, wantErr: response.ErrZitadelUnavailable, wantCalls: 1},
		{name: "transport error maps to ZITADEL_UNAVAILABLE and retries once", transport: errors.New("dial tcp"), wantErr: response.ErrZitadelUnavailable, wantCalls: 2},
		{name: "breaker open maps to ZITADEL_UNAVAILABLE without retry", transport: circuitbreaker.ErrOpen, wantErr: response.ErrZitadelUnavailable, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			var hc *http.Client
			if tt.transport != nil {
				hc = &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
					calls.Add(1)
					return nil, tt.transport
				})}
			} else {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				}))
				t.Cleanup(srv.Close)
				hc = &http.Client{Timeout: time.Second}
				hc.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
					r.URL.Scheme, r.URL.Host = "http", srv.Listener.Addr().String()
					return http.DefaultTransport.RoundTrip(r)
				})
			}
			c := NewClient(hc, "https://zauth.4ja.org/", WithRetries(1))
			claims, err := c.UserInfo(context.Background(), "tok")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, claims)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantSub, claims.Sub)
			}
			assert.Equal(t, tt.wantCalls, calls.Load())
		})
	}
}

func TestClient_UserInfo_NoRetryWhenContextDone(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	hc := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, context.Canceled
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient(hc, "https://x", WithRetries(3)).UserInfo(ctx, "tok")
	require.ErrorIs(t, err, response.ErrZitadelUnavailable)
	assert.Equal(t, int32(1), calls.Load())
}
