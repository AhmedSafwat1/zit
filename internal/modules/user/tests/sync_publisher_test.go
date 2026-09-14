package tests

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cmocks "github.com/4jawaly/identity-gateway/internal/mocks"
	userdto "github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	usersync "github.com/4jawaly/identity-gateway/internal/modules/user/jobs/sync_user"
	"github.com/4jawaly/identity-gateway/internal/modules/user/tests/mocks"
	"github.com/4jawaly/identity-gateway/pkg/queue"
)

func samplePayload() userdto.UserSyncPayload {
	return userdto.UserSyncPayload{
		Sub: "355646255235338042", Email: "admin@rawazin.example", Nickname: "شركة روازن الدولية",
		Roles: []string{"ADMIN"}, IsAdmin: true,
		MainAccount:  userdto.MainAccountPayload{Allowed: true, SubAccountsCount: 34, AccountID: "1", StatusName: "Active"},
		SubAccountOf: []string{"355646368934527195"},
		Origin:       json.RawMessage(`{"sub":"355646255235338042"}`),
	}
}

func TestSyncNotifier_Notify(t *testing.T) {
	t.Parallel()

	p := samplePayload()
	hash := p.Hash()
	const ttl = time.Hour

	envelopeFor := func(p userdto.UserSyncPayload) any {
		return mock.MatchedBy(func(e queue.Envelope) bool {
			var got userdto.UserSyncPayload
			return e.EventType == usersync.EventType && e.Version == usersync.SchemaVersion &&
				e.Producer == "identity-gateway@test" && e.EventID != "" && !e.OccurredAt.IsZero() &&
				json.Unmarshal(e.Data, &got) == nil && got.Sub == p.Sub
		})
	}

	tests := []struct {
		name    string
		setup   func(c *mocks.DedupeCache, pub *cmocks.Publisher)
		wantErr bool
	}{
		{
			name: "first publish stores the key",
			setup: func(c *mocks.DedupeCache, pub *cmocks.Publisher) {
				c.EXPECT().GetSyncSeen(mock.Anything, p.Sub).Return("", nil).Once()
				pub.EXPECT().Publish(mock.Anything, usersync.EventType, envelopeFor(p)).Return(nil).Once()
				c.EXPECT().SetSyncSeen(mock.Anything, p.Sub, hash, ttl).Return(nil).Once()
			},
		},
		{
			name: "identical hash skips the publish",
			setup: func(c *mocks.DedupeCache, pub *cmocks.Publisher) {
				c.EXPECT().GetSyncSeen(mock.Anything, p.Sub).Return(hash, nil).Once()
				// no Publish, no SetSyncSeen
			},
		},
		{
			name: "changed hash publishes and overwrites the key",
			setup: func(c *mocks.DedupeCache, pub *cmocks.Publisher) {
				c.EXPECT().GetSyncSeen(mock.Anything, p.Sub).Return("0000stale", nil).Once()
				pub.EXPECT().Publish(mock.Anything, usersync.EventType, envelopeFor(p)).Return(nil).Once()
				c.EXPECT().SetSyncSeen(mock.Anything, p.Sub, hash, ttl).Return(nil).Once()
			},
		},
		{
			name: "redis GET error publishes anyway",
			setup: func(c *mocks.DedupeCache, pub *cmocks.Publisher) {
				c.EXPECT().GetSyncSeen(mock.Anything, p.Sub).Return("", errors.New("redis: timeout")).Once()
				pub.EXPECT().Publish(mock.Anything, usersync.EventType, envelopeFor(p)).Return(nil).Once()
				c.EXPECT().SetSyncSeen(mock.Anything, p.Sub, hash, ttl).Return(nil).Once()
			},
		},
		{
			name: "publish failure does not write the key",
			setup: func(c *mocks.DedupeCache, pub *cmocks.Publisher) {
				c.EXPECT().GetSyncSeen(mock.Anything, p.Sub).Return("", nil).Once()
				pub.EXPECT().Publish(mock.Anything, usersync.EventType, envelopeFor(p)).Return(errors.New("amqp: channel closed")).Once()
				// no SetSyncSeen
			},
			wantErr: true,
		},
		{
			name: "redis SET failure after a successful publish is not an error",
			setup: func(c *mocks.DedupeCache, pub *cmocks.Publisher) {
				c.EXPECT().GetSyncSeen(mock.Anything, p.Sub).Return("", nil).Once()
				pub.EXPECT().Publish(mock.Anything, usersync.EventType, envelopeFor(p)).Return(nil).Once()
				c.EXPECT().SetSyncSeen(mock.Anything, p.Sub, hash, ttl).Return(errors.New("redis: timeout")).Once()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := mocks.NewDedupeCache(t)
			pub := cmocks.NewPublisher(t)
			tt.setup(c, pub)

			n := usersync.NewNotifier(pub, c, usersync.WithProducer("identity-gateway@test"), usersync.WithDedupeTTL(ttl))
			err := n.Notify(context.Background(), p)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
