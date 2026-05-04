// Package cache provides an in-memory TTL cache used by the Favro
// resolver tools to avoid burning the Favro rate-limit budget on
// repeated lookups.
//
// The cache is intentionally simple: a goroutine-safe map with
// per-entry expiry and an explicit Invalidate hook for any tool that
// performs a write. There is no LRU eviction — entries either expire
// or are explicitly invalidated.
package cache

import (
	"strings"
	"sync"
	"time"
)

// TTL is a generic, goroutine-safe key-value cache with a per-entry
// time-to-live. Zero-value Cache is usable; the underlying map is
// allocated lazily on first Set.
//
// Keys are strings. Callers compose the per-org keyspace by
// prefixing keys (e.g. "org-1:tags:engineering"); the cache itself
// has no notion of organizations.
type TTL[V any] struct {
	mu      sync.Mutex
	entries map[string]ttlEntry[V]
	// Now overrides time.Now for tests. Production leaves it nil.
	Now func() time.Time
}

type ttlEntry[V any] struct {
	value     V
	expiresAt time.Time
}

// Get returns the cached value for key if it exists and has not
// expired. The bool is false otherwise (including when the entry was
// never set).
//
// Lazy expiry: an expired entry encountered during Get is also
// removed from the map so a long-lived cache doesn't accumulate dead
// entries between Sweeps.
func (c *TTL[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		var zero V
		return zero, false
	}
	// Zero expiresAt means "sticky entry — never expires by time".
	if !e.expiresAt.IsZero() && !c.now().Before(e.expiresAt) {
		delete(c.entries, key)
		var zero V
		return zero, false
	}
	return e.value, true
}

// Set stores value under key with the given TTL. ttl <= 0 means "use
// only until invalidated" — the entry never expires by time, only by
// explicit Invalidate / Clear.
func (c *TTL[V]) Set(key string, value V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]ttlEntry[V])
	}
	expiresAt := time.Time{}
	if ttl > 0 {
		expiresAt = c.now().Add(ttl)
	}
	c.entries[key] = ttlEntry[V]{value: value, expiresAt: expiresAt}
}

// Invalidate removes key from the cache. No-op if absent.
func (c *TTL[V]) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// InvalidatePrefix removes every key with the given prefix. Useful
// for "the user just created a new tag — drop everything under
// `org-1:tags:`".
func (c *TTL[V]) InvalidatePrefix(prefix string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if prefix == "" {
		// Empty prefix matches everything — equivalent to Clear.
		n := len(c.entries)
		c.entries = nil
		return n
	}
	n := 0
	for k := range c.entries {
		if strings.HasPrefix(k, prefix) {
			delete(c.entries, k)
			n++
		}
	}
	return n
}

// Clear empties the cache.
func (c *TTL[V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = nil
}

// Len returns the number of entries currently cached. Includes
// time-expired entries that haven't been Got yet (Get does lazy
// eviction; Len does not).
func (c *TTL[V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Sweep walks every entry and removes those whose expiry has
// elapsed. Returns the count removed. Called by long-lived servers
// on a timer if they want to bound memory; not required for
// correctness because Get is lazily evicting.
func (c *TTL[V]) Sweep() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	n := 0
	for k, e := range c.entries {
		if !e.expiresAt.IsZero() && !now.Before(e.expiresAt) {
			delete(c.entries, k)
			n++
		}
	}
	return n
}

func (c *TTL[V]) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
