package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

// conn owns one AMQP connection and reconnects with backoff when it drops.
type conn struct {
	url string
	log zerolog.Logger

	mu     sync.RWMutex
	c      *amqp.Connection
	closed bool
	ready  chan struct{} // closed when c is usable; replaced on drop
}

func dial(url string, log zerolog.Logger) (*conn, error) {
	c := &conn{url: url, log: log, ready: make(chan struct{})}
	ac, err := amqp.DialConfig(url, amqp.Config{Dial: amqp.DefaultDial(5 * time.Second)})
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq: %w", err)
	}
	c.c = ac
	close(c.ready)
	go c.watch(ac)
	return c, nil
}

// watch redials when the connection closes; it exits when Close was called.
func (c *conn) watch(ac *amqp.Connection) {
	err := <-ac.NotifyClose(make(chan *amqp.Error, 1))
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.c = nil
	c.ready = make(chan struct{})
	c.mu.Unlock()
	c.log.Warn().Err(err).Msg("rabbitmq connection lost, reconnecting")

	backoff := 500 * time.Millisecond
	for {
		c.mu.RLock()
		closed := c.closed
		c.mu.RUnlock()
		if closed {
			return
		}
		nc, derr := amqp.DialConfig(c.url, amqp.Config{Dial: amqp.DefaultDial(5 * time.Second)})
		if derr == nil {
			c.mu.Lock()
			c.c = nc
			close(c.ready)
			c.mu.Unlock()
			c.log.Info().Msg("rabbitmq reconnected")
			go c.watch(nc)
			return
		}
		c.log.Error().Err(derr).Dur("backoff", backoff).Msg("rabbitmq redial failed")
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// channel waits (bounded by ctx) for a live connection and opens a channel.
func (c *conn) channel(ctx context.Context) (*amqp.Channel, error) {
	c.mu.RLock()
	ready, ac, closed := c.ready, c.c, c.closed
	c.mu.RUnlock()
	if closed {
		return nil, ErrClosed
	}
	if ac == nil {
		select {
		case <-ready:
			c.mu.RLock()
			ac = c.c
			c.mu.RUnlock()
		case <-ctx.Done():
			return nil, fmt.Errorf("rabbitmq not connected: %w", ctx.Err())
		}
	}
	if ac == nil {
		return nil, errors.New("rabbitmq not connected")
	}
	ch, err := ac.Channel()
	if err != nil {
		return nil, fmt.Errorf("open channel: %w", err)
	}
	return ch, nil
}

func (c *conn) close() error {
	c.mu.Lock()
	c.closed = true
	ac := c.c
	c.c = nil
	c.mu.Unlock()
	if ac != nil && !ac.IsClosed() {
		return ac.Close()
	}
	return nil
}
