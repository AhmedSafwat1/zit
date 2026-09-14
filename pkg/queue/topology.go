package queue

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Topology names. Queues are NOT versioned — the schema version lives in
// the envelope. A breaking change creates user.sync.v2 at that time.
const (
	QueueUserSync   = "user.sync"
	RoutingUserSync = "user.sync"

	retrySuffix = ".retry"
	dlqSuffix   = ".dlq"
	dlxSuffix   = ".dlx"
)

// RetryQueue returns the delayed-retry queue name for q.
func RetryQueue(q string) string { return q + retrySuffix }

// DeadLetterQueue returns the DLQ name for q.
func DeadLetterQueue(q string) string { return q + dlqSuffix }

// DeadLetterExchange returns the DLX name for exchange.
func DeadLetterExchange(exchange string) string { return exchange + dlxSuffix }

// Binding is one work queue with its retry and dead-letter companions.
type Binding struct {
	Queue      string
	RoutingKey string
}

// DeclareTopology declares (idempotently) the exchange, DLX and, for every
// binding, the work queue, retry queue and DLQ. Both binaries call it so
// messages published before the worker starts are not lost.
func DeclareTopology(ch *amqp.Channel, exchange string, bindings ...Binding) error {
	dlx := DeadLetterExchange(exchange)
	if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange %s: %w", exchange, err)
	}
	if err := ch.ExchangeDeclare(dlx, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange %s: %w", dlx, err)
	}
	for _, b := range bindings {
		// Work queue: failures dead-letter to the DLX with the same key.
		if _, err := ch.QueueDeclare(b.Queue, true, false, false, false, amqp.Table{
			"x-dead-letter-exchange": dlx,
		}); err != nil {
			return fmt.Errorf("declare queue %s: %w", b.Queue, err)
		}
		if err := ch.QueueBind(b.Queue, b.RoutingKey, exchange, false, nil); err != nil {
			return fmt.Errorf("bind queue %s: %w", b.Queue, err)
		}
		// Retry queue: per-message TTL, expired messages route back to the work queue.
		retry := RetryQueue(b.Queue)
		if _, err := ch.QueueDeclare(retry, true, false, false, false, amqp.Table{
			"x-dead-letter-exchange":    exchange,
			"x-dead-letter-routing-key": b.RoutingKey,
		}); err != nil {
			return fmt.Errorf("declare queue %s: %w", retry, err)
		}
		// DLQ: bound to the DLX with the work routing key.
		dlq := DeadLetterQueue(b.Queue)
		if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare queue %s: %w", dlq, err)
		}
		if err := ch.QueueBind(dlq, b.RoutingKey, dlx, false, nil); err != nil {
			return fmt.Errorf("bind queue %s: %w", dlq, err)
		}
	}
	return nil
}

// Header names.
const (
	HeaderSchemaVersion = "x-schema-version"
	HeaderRetryCount    = "x-retry-count"
	HeaderFailureReason = "x-failure-reason"
	HeaderFailedAt      = "x-failed-at"
)

// toPublishing mirrors the envelope into AMQP properties.
func toPublishing(e Envelope, body []byte, retryCount int, extra amqp.Table) amqp.Publishing {
	headers := amqp.Table{
		HeaderSchemaVersion: int32(e.Version),
		HeaderRetryCount:    int32(retryCount),
	}
	for k, v := range extra {
		headers[k] = v
	}
	return amqp.Publishing{
		Headers:      headers,
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    e.EventID,
		Type:         e.EventType,
		AppId:        e.Producer,
		Timestamp:    e.OccurredAt,
		Body:         body,
	}
}

func headerInt(h amqp.Table, key string) int {
	switch v := h[key].(type) {
	case int32:
		return int(v)
	case int64:
		return int(v)
	case int:
		return v
	case float64:
		return int(v)
	}
	return 0
}
