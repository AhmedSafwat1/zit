package tests

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	userdto "github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	usersync "github.com/4jawaly/identity-gateway/internal/modules/user/jobs/sync_user"
	"github.com/4jawaly/identity-gateway/internal/modules/user/tests/mocks"
	"github.com/4jawaly/identity-gateway/pkg/queue"
)

func TestSyncConsumer_Handle(t *testing.T) {
	t.Parallel()

	occurred := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	valid := samplePayload()

	tests := []struct {
		name    string
		env     queue.Envelope
		setup   func(s *mocks.Syncer)
		wantErr error
	}{
		{
			name:    "unsupported version is permanent and never reaches the service",
			env:     queue.Envelope{EventID: "1", EventType: usersync.EventType, Version: 2, OccurredAt: occurred, Data: json.RawMessage(`{}`)},
			setup:   func(*mocks.Syncer) {},
			wantErr: queue.ErrUnsupportedVersion,
		},
		{
			name:    "malformed body is permanent and never reaches the service",
			env:     queue.Envelope{EventID: "1", EventType: usersync.EventType, Version: usersync.SchemaVersion, OccurredAt: occurred, Data: json.RawMessage(`{"sub":`)},
			setup:   func(*mocks.Syncer) {},
			wantErr: queue.ErrMalformed,
		},
		{
			name:    "empty sub is malformed",
			env:     queue.Envelope{EventID: "1", EventType: usersync.EventType, Version: usersync.SchemaVersion, OccurredAt: occurred, Data: json.RawMessage(`{"sub":""}`)},
			setup:   func(*mocks.Syncer) {},
			wantErr: queue.ErrMalformed,
		},
		{
			name: "valid envelope calls Sync with the payload and OccurredAt",
			env:  queue.Envelope{EventID: "1", EventType: usersync.EventType, Version: usersync.SchemaVersion, OccurredAt: occurred, Data: mustJSON(t, valid)},
			setup: func(s *mocks.Syncer) {
				s.EXPECT().Sync(mock.Anything, mock.MatchedBy(func(p userdto.UserSyncPayload) bool {
					return p.Sub == valid.Sub && p.Hash() == valid.Hash()
				}), occurred).Return(nil).Once()
			},
		},
		{
			name: "service error is returned for retry",
			env:  queue.Envelope{EventID: "1", EventType: usersync.EventType, Version: usersync.SchemaVersion, OccurredAt: occurred, Data: mustJSON(t, valid)},
			setup: func(s *mocks.Syncer) {
				s.EXPECT().Sync(mock.Anything, mock.Anything, occurred).Return(errDB).Once()
			},
			wantErr: errDB,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := mocks.NewSyncer(t)
			tt.setup(s)
			err := usersync.NewConsumer(s).Handle(context.Background(), tt.env)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

var errDB = errors.New("postgres: connection refused")
