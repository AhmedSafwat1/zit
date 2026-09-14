package queue

import "errors"

var (
	// ErrUnsupportedVersion is permanent: the envelope's schema version is
	// unknown to this consumer. The message goes straight to the DLQ.
	ErrUnsupportedVersion = errors.New("queue: unsupported schema version")
	// ErrMalformed is permanent: the data cannot be decoded.
	ErrMalformed = errors.New("queue: malformed message")
	// ErrClosed is returned by a publisher after Close.
	ErrClosed = errors.New("queue: publisher closed")
)

// IsPermanent reports whether err must skip retries and go to the DLQ.
func IsPermanent(err error) bool {
	return errors.Is(err, ErrUnsupportedVersion) || errors.Is(err, ErrMalformed)
}
