// Package usercache is the user module's view of the generic cache: key
// naming and the read/write rules for introspection reports, negative
// markers, sync dedupe and user documents. The application-wide key prefix
// is the Store's job, not this package's.
package usercache

import "github.com/4jawaly/identity-gateway/pkg/cache"

// IntrospectKey holds a cached identity report for a token hash.
func IntrospectKey(hash string) string { return cache.Key("introspect", hash) }

// InactiveKey is the negative-cache marker for a rejected token hash.
func InactiveKey(hash string) string { return cache.Key("inactive", hash) }

// SyncSeenKey holds the last payload hash published for a subject.
func SyncSeenKey(sub string) string { return cache.Key("sync", "seen", sub) }

// UserKey holds a cached user document.
func UserKey(sub string) string { return cache.Key("user", sub) }
