package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryPolicy_Delay(t *testing.T) {
	t.Parallel()
	p := DefaultRetryPolicy(3, time.Second)
	tests := []struct {
		name    string
		attempt int
		want    time.Duration
	}{
		{name: "first retry after 1s", attempt: 0, want: time.Second},
		{name: "second retry after 5s", attempt: 1, want: 5 * time.Second},
		{name: "third retry after 25s", attempt: 2, want: 25 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, p.Delay(tt.attempt))
		})
	}
}

func TestRetryMiddleware(t *testing.T) {
	t.Parallel()
	transient := errors.New("db down")
	tests := []struct {
		name       string
		handlerErr error
		retryCount int
		wantRetry  bool
		wantAfter  time.Duration
		wantErr    error
	}{
		{name: "success passes through", handlerErr: nil},
		{name: "transient first failure retries after 1s", handlerErr: transient, retryCount: 0, wantRetry: true, wantAfter: time.Second, wantErr: transient},
		{name: "transient third failure retries after 25s", handlerErr: transient, retryCount: 2, wantRetry: true, wantAfter: 25 * time.Second, wantErr: transient},
		{name: "retries exhausted goes to dlq", handlerErr: transient, retryCount: 3, wantRetry: false, wantErr: transient},
		{name: "unsupported version is permanent", handlerErr: ErrUnsupportedVersion, wantRetry: false, wantErr: ErrUnsupportedVersion},
		{name: "malformed is permanent", handlerErr: ErrMalformed, wantRetry: false, wantErr: ErrMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := Retry(DefaultRetryPolicy(3, time.Second))(func(context.Context, Envelope) error { return tt.handlerErr })
			err := h(context.Background(), Envelope{RetryCount: tt.retryCount})
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
			var rl *RetryLater
			assert.Equal(t, tt.wantRetry, errors.As(err, &rl))
			if tt.wantRetry {
				assert.Equal(t, tt.wantAfter, rl.After)
			}
		})
	}
}

func TestRecoverMiddleware(t *testing.T) {
	t.Parallel()
	h := Recover(testLogger())(func(context.Context, Envelope) error { panic("boom") })
	err := h(context.Background(), Envelope{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestEnvelope_Decode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		data    string
		wantErr error
	}{
		{name: "valid json decodes", data: `{"a":1}`},
		{name: "invalid json is malformed", data: `{`, wantErr: ErrMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var v map[string]any
			err := Envelope{Data: []byte(tt.data)}.Decode(&v)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
