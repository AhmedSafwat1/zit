package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Key joins parts with ":" — the conventional Redis key separator.
func Key(parts ...string) string { return strings.Join(parts, ":") }

// SHA256Hex returns the lowercase hex sha256 of s. Use it to derive a key
// from a secret (a bearer token) so the secret itself never reaches the cache.
func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	var buf [sha256.Size * 2]byte
	hex.Encode(buf[:], sum[:])
	return string(buf[:])
}
