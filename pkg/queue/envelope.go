package queue

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// Envelope is the wire format of every message. Queue names are not
// versioned; Version describes the schema of Data.
type Envelope struct {
	EventID    string          `json:"event_id"`    // ULID
	EventType  string          `json:"event_type"`  // "user.sync"
	Version    int             `json:"version"`     // schema version of Data
	OccurredAt time.Time       `json:"occurred_at"` // set ONCE by the producer, never regenerated on retry
	Producer   string          `json:"producer"`    // "identity-gateway@<version>"
	Data       json.RawMessage `json:"data"`

	// RetryCount is read from the x-retry-count header on consume. Not serialised.
	RetryCount int `json:"-"`
}

// NewEnvelope builds an envelope with a fresh ULID and OccurredAt = now.
func NewEnvelope(eventType string, version int, producer string, data any) (Envelope, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal %s data: %w", eventType, err)
	}
	return Envelope{
		EventID:    ulid.Make().String(),
		EventType:  eventType,
		Version:    version,
		OccurredAt: time.Now().UTC(),
		Producer:   producer,
		Data:       raw,
	}, nil
}

// Decode unmarshals Data into v, mapping failure to ErrMalformed.
func (e Envelope) Decode(v any) error {
	if err := json.Unmarshal(e.Data, v); err != nil {
		return fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	return nil
}
