package sync_user

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/4jawaly/identity-gateway/pkg/queue"
)

// DedupeCache remembers the last payload hash published per subject.
type DedupeCache interface {
	GetSyncSeen(ctx context.Context, sub string) (string, error)
	SetSyncSeen(ctx context.Context, sub, hash string, ttl time.Duration) error
}

// Notifier implements services.SyncNotifier. It publishes a user.sync
// envelope unless Redis says the worker already has this exact state:
//
//	sync:seen:<sub> == hash  -> skip
//	missing / different      -> publish, then SET sync:seen:<sub> = hash
//	Redis error              -> publish anyway (the upsert is idempotent)
//	publish failed           -> do NOT write the key, so the next miss retries
type Notifier struct {
	pub      queue.Publisher
	cache    DedupeCache
	producer string
	ttl      time.Duration
	log      zerolog.Logger
}

// NotifierOption customises NewNotifier.
type NotifierOption func(*Notifier)

// WithProducer sets the envelope producer ("identity-gateway@<version>").
func WithProducer(p string) NotifierOption { return func(n *Notifier) { n.producer = p } }

// WithDedupeTTL sets the sync:seen TTL.
func WithDedupeTTL(d time.Duration) NotifierOption { return func(n *Notifier) { n.ttl = d } }

// WithNotifierLogger sets the logger.
func WithNotifierLogger(l zerolog.Logger) NotifierOption { return func(n *Notifier) { n.log = l } }

// NewNotifier builds the notifier.
func NewNotifier(pub queue.Publisher, c DedupeCache, opts ...NotifierOption) *Notifier {
	n := &Notifier{pub: pub, cache: c, producer: "identity-gateway", ttl: time.Hour, log: zerolog.Nop()}
	for _, o := range opts {
		o(n)
	}
	return n
}

// Notify publishes p unless it was already published with the same hash.
func (n *Notifier) Notify(ctx context.Context, p UserSyncPayload) error {
	hash := p.Hash()
	seen, err := n.cache.GetSyncSeen(ctx, p.Sub)
	if err != nil {
		n.log.Warn().Err(err).Str("sub", p.Sub).Msg("sync dedupe read failed, publishing anyway")
	} else if seen == hash {
		return nil
	}

	e, err := queue.NewEnvelope(EventType, SchemaVersion, n.producer, p)
	if err != nil {
		return err
	}
	if err := n.pub.Publish(ctx, EventType, e); err != nil {
		return fmt.Errorf("publish user.sync for %s: %w", p.Sub, err)
	}
	if err := n.cache.SetSyncSeen(ctx, p.Sub, hash, n.ttl); err != nil {
		n.log.Warn().Err(err).Str("sub", p.Sub).Msg("sync dedupe write failed")
	}
	return nil
}
