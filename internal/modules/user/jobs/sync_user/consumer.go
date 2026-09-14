package sync_user

import (
	"context"
	"fmt"
	"time"

	"github.com/4jawaly/identity-gateway/pkg/queue"
)

// Syncer is what the consumer needs from the service layer.
type Syncer interface {
	Sync(ctx context.Context, p UserSyncPayload, occurredAt time.Time) error
}

// Consumer decodes user.sync envelopes and hands them to the Syncer.
type Consumer struct{ svc Syncer }

// NewConsumer builds the consumer.
func NewConsumer(svc Syncer) *Consumer { return &Consumer{svc: svc} }

// Handle is the queue.Handler. Version and decode failures are permanent.
func (c *Consumer) Handle(ctx context.Context, e queue.Envelope) error {
	if e.Version != SchemaVersion {
		return fmt.Errorf("%w: got %d, want %d", queue.ErrUnsupportedVersion, e.Version, SchemaVersion)
	}
	var p UserSyncPayload
	if err := e.Decode(&p); err != nil {
		return err
	}
	if p.Sub == "" {
		return fmt.Errorf("%w: empty sub", queue.ErrMalformed)
	}
	return c.svc.Sync(ctx, p, e.OccurredAt)
}

// Register binds Handle to the user.sync queue.
func (c *Consumer) Register(qc *queue.Consumer) { qc.Handle(Binding, c.Handle) }
