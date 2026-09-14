package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

// Consumer runs one handler per queue with a bounded worker pool. Failures
// are routed: RetryLater -> retry queue with per-message TTL; anything
// else -> DLQ with the original body untouched and the reason in headers.
type Consumer struct {
	url         string
	exchange    string
	prefetch    int
	concurrency int
	log         zerolog.Logger
	middlewares []Middleware

	mu       sync.Mutex
	handlers map[string]registration
}

type registration struct {
	binding Binding
	handler Handler
}

// ConsumerOption customises NewConsumer.
type ConsumerOption func(*Consumer)

// WithPrefetch sets the per-channel QoS prefetch.
func WithPrefetch(n int) ConsumerOption { return func(c *Consumer) { c.prefetch = n } }

// WithConcurrency sets the number of worker goroutines per queue.
func WithConcurrency(n int) ConsumerOption { return func(c *Consumer) { c.concurrency = n } }

// WithConsumerLogger sets the logger.
func WithConsumerLogger(l zerolog.Logger) ConsumerOption { return func(c *Consumer) { c.log = l } }

// WithMiddleware sets the middleware chain applied to every handler
// (outermost first). Recover is always added outermost by Run.
func WithMiddleware(mws ...Middleware) ConsumerOption {
	return func(c *Consumer) { c.middlewares = mws }
}

// NewConsumer builds a consumer. Nothing is dialled until Run.
func NewConsumer(url, exchange string, opts ...ConsumerOption) *Consumer {
	c := &Consumer{url: url, exchange: exchange, prefetch: 64, concurrency: 8, log: zerolog.Nop(), handlers: map[string]registration{}}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Handle registers h for the queue described by b.
func (c *Consumer) Handle(b Binding, h Handler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[b.Queue] = registration{binding: b, handler: h}
}

// Bindings returns every registered binding (for topology declaration).
func (c *Consumer) Bindings() []Binding {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Binding, 0, len(c.handlers))
	for _, r := range c.handlers {
		out = append(out, r.binding)
	}
	return out
}

// Run consumes until ctx is cancelled. It reconnects on connection loss and
// returns only when ctx is done (or the initial dial fails).
func (c *Consumer) Run(ctx context.Context) error {
	if len(c.handlers) == 0 {
		return errors.New("consumer: no handlers registered")
	}
	cn, err := dial(c.url, c.log)
	if err != nil {
		return err
	}
	defer func() { _ = cn.close() }()

	backoff := time.Second
	for {
		err := c.runOnce(ctx, cn)
		if ctx.Err() != nil {
			return nil
		}
		c.log.Error().Err(err).Dur("backoff", backoff).Msg("consumer session ended, restarting")
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// runOnce opens one channel per queue and blocks until the connection drops
// or ctx ends. All in-flight handlers finish before it returns.
func (c *Consumer) runOnce(ctx context.Context, cn *conn) error {
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	setup, err := cn.channel(sessionCtx)
	if err != nil {
		return err
	}
	if err := DeclareTopology(setup, c.exchange, c.Bindings()...); err != nil {
		_ = setup.Close()
		return err
	}
	_ = setup.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, len(c.handlers))
	c.mu.Lock()
	regs := make([]registration, 0, len(c.handlers))
	for _, r := range c.handlers {
		regs = append(regs, r)
	}
	c.mu.Unlock()

	for _, r := range regs {
		wg.Add(1)
		go func(r registration) {
			defer wg.Done()
			if err := c.consumeQueue(sessionCtx, cn, r); err != nil {
				errCh <- err
				cancel()
			}
		}(r)
	}
	wg.Wait()
	close(errCh)
	return <-errCh
}

func (c *Consumer) consumeQueue(ctx context.Context, cn *conn, r registration) error {
	ch, err := cn.channel(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()
	if err := ch.Qos(c.prefetch, 0, false); err != nil {
		return fmt.Errorf("qos: %w", err)
	}
	// A dedicated channel publishes retries / dead letters so a publish
	// failure never poisons the consuming channel.
	pubCh, err := cn.channel(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = pubCh.Close() }()
	if err := pubCh.Confirm(false); err != nil {
		return fmt.Errorf("confirm mode: %w", err)
	}

	deliveries, err := ch.Consume(r.binding.Queue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume %s: %w", r.binding.Queue, err)
	}
	closed := ch.NotifyClose(make(chan *amqp.Error, 1))
	handler := Chain(r.handler, append([]Middleware{Recover(c.log)}, c.middlewares...)...)

	var wg sync.WaitGroup
	sem := make(chan struct{}, c.concurrency)
	var pubMu sync.Mutex
	router := &failureRouter{ch: pubCh, mu: &pubMu, exchange: c.exchange, binding: r.binding, log: c.log}

	defer wg.Wait()
	for {
		select {
		case <-ctx.Done():
			return nil
		case cerr := <-closed:
			return fmt.Errorf("channel closed: %w", cerr)
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("deliveries closed")
			}
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				_ = d.Nack(false, true)
				return nil
			}
			wg.Add(1)
			go func(d amqp.Delivery) {
				defer wg.Done()
				defer func() { <-sem }()
				c.handleDelivery(ctx, d, handler, router)
			}(d)
		}
	}
}

func (c *Consumer) handleDelivery(ctx context.Context, d amqp.Delivery, h Handler, router *failureRouter) {
	var e Envelope
	if err := json.Unmarshal(d.Body, &e); err != nil {
		c.log.Error().Err(err).Str("message_id", d.MessageId).Msg("undecodable envelope, dead-lettering")
		router.deadLetter(ctx, d, fmt.Errorf("%w: %w", ErrMalformed, err))
		return
	}
	e.RetryCount = headerInt(d.Headers, HeaderRetryCount)

	err := h(ctx, e)
	if err == nil {
		if aerr := d.Ack(false); aerr != nil {
			c.log.Error().Err(aerr).Str("event_id", e.EventID).Msg("ack failed")
		}
		return
	}
	var rl *RetryLater
	if errors.As(err, &rl) {
		router.retry(ctx, d, e, rl)
		return
	}
	router.deadLetter(ctx, d, err)
}

// failureRouter publishes retries and dead letters, then acks the original.
type failureRouter struct {
	ch       *amqp.Channel
	mu       *sync.Mutex
	exchange string
	binding  Binding
	log      zerolog.Logger
}

func (f *failureRouter) publish(ctx context.Context, exchange, key string, pub amqp.Publishing) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	dc, err := f.ch.PublishWithDeferredConfirmWithContext(ctx, exchange, key, false, false, pub)
	if err != nil {
		return err
	}
	ok, err := dc.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("broker nacked")
	}
	return nil
}

// retry re-publishes the original body to the retry queue with a TTL, then acks.
func (f *failureRouter) retry(ctx context.Context, d amqp.Delivery, e Envelope, rl *RetryLater) {
	pub := toPublishing(e, d.Body, e.RetryCount+1, amqp.Table{HeaderFailureReason: rl.Err.Error()})
	pub.Expiration = strconv.FormatInt(rl.After.Milliseconds(), 10)
	if err := f.publish(ctx, "", RetryQueue(f.binding.Queue), pub); err != nil {
		f.log.Error().Err(err).Str("event_id", e.EventID).Msg("retry publish failed, nacking for requeue")
		_ = d.Nack(false, true)
		return
	}
	f.log.Warn().Err(rl.Err).Str("event_id", e.EventID).Int("retry", e.RetryCount+1).Dur("after", rl.After).Msg("message scheduled for retry")
	_ = d.Ack(false)
}

// deadLetter forwards the untouched body to the DLX with the reason in headers, then acks.
func (f *failureRouter) deadLetter(ctx context.Context, d amqp.Delivery, cause error) {
	headers := amqp.Table{}
	for k, v := range d.Headers {
		headers[k] = v
	}
	headers[HeaderFailureReason] = cause.Error()
	headers[HeaderFailedAt] = time.Now().UTC().Format(time.RFC3339Nano)
	pub := amqp.Publishing{
		Headers: headers, ContentType: d.ContentType, DeliveryMode: amqp.Persistent,
		MessageId: d.MessageId, Type: d.Type, AppId: d.AppId, Timestamp: d.Timestamp, Body: d.Body,
	}
	if err := f.publish(ctx, DeadLetterExchange(f.exchange), f.binding.RoutingKey, pub); err != nil {
		f.log.Error().Err(err).Str("message_id", d.MessageId).Msg("dead-letter publish failed, nacking without requeue")
		_ = d.Nack(false, false) // broker-side DLX still catches it
		return
	}
	f.log.Error().Err(cause).Str("message_id", d.MessageId).Msg("message dead-lettered")
	_ = d.Ack(false)
}
