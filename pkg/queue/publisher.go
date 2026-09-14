package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

// RabbitPublisher publishes envelopes in confirm mode over a self-healing
// connection. A failed publish is returned to the caller; the caller decides
// whether to log and move on (the HTTP path always does).
type RabbitPublisher struct {
	conn     *conn
	exchange string
	timeout  time.Duration
	confirm  bool
	log      zerolog.Logger

	mu sync.Mutex
	ch *amqp.Channel

	wg     sync.WaitGroup
	closed bool
}

// PublisherOption customises NewPublisher.
type PublisherOption func(*RabbitPublisher)

// WithPublishTimeout bounds one Publish call including the broker confirm.
func WithPublishTimeout(d time.Duration) PublisherOption {
	return func(p *RabbitPublisher) { p.timeout = d }
}

// WithConfirm toggles publisher confirms.
func WithConfirm(on bool) PublisherOption { return func(p *RabbitPublisher) { p.confirm = on } }

// WithPublisherLogger sets the logger.
func WithPublisherLogger(l zerolog.Logger) PublisherOption {
	return func(p *RabbitPublisher) { p.log = l }
}

// NewPublisher dials the broker and declares the topology for bindings. It
// fails fast when the broker is unreachable.
func NewPublisher(ctx context.Context, url, exchange string, bindings []Binding, opts ...PublisherOption) (*RabbitPublisher, error) {
	p := &RabbitPublisher{exchange: exchange, timeout: 500 * time.Millisecond, confirm: true, log: zerolog.Nop()}
	for _, o := range opts {
		o(p)
	}
	c, err := dial(url, p.log)
	if err != nil {
		return nil, err
	}
	p.conn = c
	ch, err := c.channel(ctx)
	if err != nil {
		_ = c.close()
		return nil, err
	}
	if err := DeclareTopology(ch, exchange, bindings...); err != nil {
		_ = ch.Close()
		_ = c.close()
		return nil, err
	}
	_ = ch.Close()
	return p, nil
}

// channel returns the cached channel or opens a fresh one in confirm mode.
func (p *RabbitPublisher) channel(ctx context.Context) (*amqp.Channel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, ErrClosed
	}
	if p.ch != nil && !p.ch.IsClosed() {
		return p.ch, nil
	}
	ch, err := p.conn.channel(ctx)
	if err != nil {
		return nil, err
	}
	if p.confirm {
		if err := ch.Confirm(false); err != nil {
			_ = ch.Close()
			return nil, fmt.Errorf("confirm mode: %w", err)
		}
	}
	p.ch = ch
	return ch, nil
}

// Publish sends e with routingKey and waits for the broker confirm within
// the publish timeout. OccurredAt and EventID are taken from e untouched.
func (p *RabbitPublisher) Publish(ctx context.Context, routingKey string, e Envelope) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrClosed
	}
	p.wg.Add(1)
	p.mu.Unlock()
	defer p.wg.Done()

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	ch, err := p.channel(ctx)
	if err != nil {
		return err
	}
	pub := toPublishing(e, body, 0, nil)
	if !p.confirm {
		if err := ch.PublishWithContext(ctx, p.exchange, routingKey, false, false, pub); err != nil {
			p.dropChannel(ch)
			return fmt.Errorf("publish %s: %w", routingKey, err)
		}
		return nil
	}
	dc, err := ch.PublishWithDeferredConfirmWithContext(ctx, p.exchange, routingKey, false, false, pub)
	if err != nil {
		p.dropChannel(ch)
		return fmt.Errorf("publish %s: %w", routingKey, err)
	}
	acked, err := dc.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("publish %s confirm: %w", routingKey, err)
	}
	if !acked {
		return fmt.Errorf("publish %s: broker nacked", routingKey)
	}
	return nil
}

func (p *RabbitPublisher) dropChannel(ch *amqp.Channel) {
	p.mu.Lock()
	if p.ch == ch {
		p.ch = nil
	}
	p.mu.Unlock()
	_ = ch.Close()
}

// Close waits for in-flight publishes (bounded by ctx) and closes the connection.
func (p *RabbitPublisher) Close(ctx context.Context) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	ch := p.ch
	p.ch = nil
	p.mu.Unlock()

	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	var errs []error
	select {
	case <-done:
	case <-ctx.Done():
		errs = append(errs, fmt.Errorf("publisher drain: %w", ctx.Err()))
	}
	if ch != nil {
		_ = ch.Close()
	}
	if err := p.conn.close(); err != nil && !errors.Is(err, amqp.ErrClosed) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

var _ Publisher = (*RabbitPublisher)(nil)
