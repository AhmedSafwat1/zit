package circuitbreaker

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsFailure(t *testing.T) {
	t.Parallel()

	alive := context.Background()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	t.Cleanup(cancelExpired)

	tests := []struct {
		name   string
		parent context.Context
		resp   *http.Response
		err    error
		want   bool
	}{
		{name: "transport error counts as failure", parent: alive, err: errors.New("dial tcp: connection refused"), want: true},
		{name: "http 500 counts as failure", parent: alive, resp: &http.Response{StatusCode: 500}, want: true},
		{name: "http 503 counts as failure", parent: alive, resp: &http.Response{StatusCode: 503}, want: true},
		{name: "http 401 is not a failure", parent: alive, resp: &http.Response{StatusCode: 401}, want: false},
		{name: "http 403 is not a failure", parent: alive, resp: &http.Response{StatusCode: 403}, want: false},
		{name: "http 200 is not a failure", parent: alive, resp: &http.Response{StatusCode: 200}, want: false},
		{name: "caller cancelled is not a failure", parent: cancelled, err: context.Canceled, want: false},
		{name: "wrapped caller cancel is not a failure", parent: cancelled, err: errors.Join(errors.New("x"), context.Canceled), want: false},
		{name: "our timeout with parent alive is a failure", parent: alive, err: context.DeadlineExceeded, want: true},
		{name: "parent deadline expired is not a failure", parent: expired, err: context.DeadlineExceeded, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isFailure(tt.parent, tt.resp, tt.err))
		})
	}
}
