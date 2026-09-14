package circuitbreaker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func settings(name string) Settings {
	return Settings{Name: name, Enabled: true, MaxRequests: 1, Interval: time.Minute, OpenTimeout: 10 * time.Second, ConsecutiveFailures: 2}
}

func TestNew(t *testing.T) {
	t.Parallel()

	base := &http.Client{Timeout: time.Second}
	tests := []struct {
		name     string
		opts     []Option
		wantSame bool
		wantErr  error
	}{
		{name: "missing client is an error", opts: nil, wantErr: errors.New("circuit breaker: WithHTTPClient is required")},
		{name: "disabled returns base client unchanged", opts: []Option{WithHTTPClient(base), WithSettings(Settings{Name: "x"})}, wantSame: true},
		{name: "enabled without name returns base client unchanged", opts: []Option{WithHTTPClient(base), WithSettings(Settings{Enabled: true})}, wantSame: true},
		{name: "active with zero max requests is invalid", opts: []Option{WithHTTPClient(base), WithSettings(Settings{Name: "x", Enabled: true, ConsecutiveFailures: 1, OpenTimeout: time.Second})}, wantErr: ErrInvalidSettings},
		{name: "active with zero consecutive failures is invalid", opts: []Option{WithHTTPClient(base), WithSettings(Settings{Name: "x", Enabled: true, MaxRequests: 1, OpenTimeout: time.Second})}, wantErr: ErrInvalidSettings},
		{name: "active with zero open timeout is invalid", opts: []Option{WithHTTPClient(base), WithSettings(Settings{Name: "x", Enabled: true, MaxRequests: 1, ConsecutiveFailures: 1})}, wantErr: ErrInvalidSettings},
		{name: "active valid settings wraps the client", opts: []Option{WithHTTPClient(base), WithSettings(settings("x"))}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := New(tt.opts...)
			if tt.wantErr != nil {
				require.Error(t, err)
				if errors.Is(tt.wantErr, ErrInvalidSettings) {
					require.ErrorIs(t, err, ErrInvalidSettings)
				}
				return
			}
			require.NoError(t, err)
			if tt.wantSame {
				assert.Same(t, base, got)
			} else {
				assert.NotSame(t, base, got)
				assert.Zero(t, got.Timeout, "timeout moves into the transport")
			}
		})
	}
}

func TestTransport_Outcomes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		clientTO   time.Duration
		ctx        func() (context.Context, context.CancelFunc)
		calls      int
		wantOpen   bool
		wantStatus int
	}{
		{
			name:     "two 5xx trip the breaker",
			handler:  func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(502) },
			clientTO: time.Second, calls: 3, wantOpen: true, wantStatus: 502,
		},
		{
			name:     "401s never trip the breaker",
			handler:  func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) },
			clientTO: time.Second, calls: 10, wantOpen: false, wantStatus: 401,
		},
		{
			name: "our timeout on a slow upstream trips the breaker",
			handler: func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-time.After(2 * time.Second):
				case <-r.Context().Done():
				}
			},
			clientTO: 20 * time.Millisecond, calls: 3, wantOpen: true,
		},
		{
			name: "caller cancellation never trips the breaker",
			handler: func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-time.After(2 * time.Second):
				case <-r.Context().Done():
				}
			},
			clientTO: time.Second,
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				go func() { time.Sleep(10 * time.Millisecond); cancel() }()
				return ctx, cancel
			},
			calls: 3, wantOpen: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tt.handler)
			t.Cleanup(srv.Close)

			client, err := New(
				WithHTTPClient(&http.Client{Timeout: tt.clientTO}),
				WithSettings(settings(t.Name())),
				WithLogger(zerolog.Nop()),
			)
			require.NoError(t, err)

			var lastStatus int
			var lastErr error
			for i := 0; i < tt.calls; i++ {
				ctx, cancel := context.Background(), context.CancelFunc(func() {})
				if tt.ctx != nil {
					ctx, cancel = tt.ctx()
				}
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
				resp, err := client.Do(req)
				lastErr = err
				if err == nil {
					lastStatus = resp.StatusCode
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
				cancel()
			}
			if tt.wantOpen {
				require.ErrorIs(t, lastErr, ErrOpen)
			} else {
				assert.NotErrorIs(t, lastErr, ErrOpen)
			}
			if tt.wantStatus != 0 && lastErr == nil {
				assert.Equal(t, tt.wantStatus, lastStatus)
			}
		})
	}
}

func TestTransport_5xxIsStillReturnedToCaller(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(500)
		_, _ = w.Write([]byte("boom"))
	}))
	t.Cleanup(srv.Close)
	client, err := New(WithHTTPClient(&http.Client{Timeout: time.Second}), WithSettings(settings("x")))
	require.NoError(t, err)
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, 500, resp.StatusCode)
	assert.Equal(t, "boom", string(body))
	assert.Equal(t, int32(1), hits.Load())
}
