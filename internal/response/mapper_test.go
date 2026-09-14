package response_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/4jawaly/identity-gateway/internal/response"
)

func TestMap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []int
		want []string
	}{
		{name: "maps every element", in: []int{1, 2}, want: []string{"1", "2"}},
		{name: "empty input yields empty not nil", in: nil, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := response.Map(tt.in, strconv.Itoa)
			assert.Equal(t, tt.want, got)
			assert.NotNil(t, got)
		})
	}
}

func TestMapRef(t *testing.T) {
	t.Parallel()
	type row struct{ N int }
	got := response.MapRef([]row{{1}, {2}}, func(r *row) int { return r.N * 10 })
	assert.Equal(t, []int{10, 20}, got)
	assert.NotNil(t, response.MapRef([]row(nil), func(r *row) int { return r.N }))
}

func TestMapErr(t *testing.T) {
	t.Parallel()
	errBad := errors.New("bad")
	tests := []struct {
		name    string
		in      []string
		want    []int
		wantErr error
	}{
		{name: "all succeed", in: []string{"1", "2"}, want: []int{1, 2}},
		{name: "stops at first failure", in: []string{"1", "x", "3"}, wantErr: errBad},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := response.MapErr(tt.in, func(s string) (int, error) {
				n, err := strconv.Atoi(s)
				if err != nil {
					return 0, errBad
				}
				return n, nil
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
