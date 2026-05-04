package favro

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Favro response headers we observe. Documented at favro.com/developer.
const (
	headerRateLimitLimit     = "X-RateLimit-Limit"
	headerRateLimitRemaining = "X-RateLimit-Remaining"
	headerRateLimitReset     = "X-RateLimit-Reset"
	headerRetryAfter         = "Retry-After"
	headerRequestID          = "X-Favro-Backend-Identifier"
)

// RateLimitSnapshot captures the most-recent rate-limit signal Favro
// returned. Exposed via the favro_rate_limit_status MCP tool so an
// LLM can see when it's burning the per-organization quota.
type RateLimitSnapshot struct {
	// Limit is the per-org quota (requests per hour) Favro reported
	// in X-RateLimit-Limit. 0 means the header was absent.
	Limit int
	// Remaining is the count of requests left in the current window,
	// from X-RateLimit-Remaining. -1 means "header absent" so 0 stays
	// distinguishable from "missing".
	Remaining int
	// Reset is the epoch when the current window resets, parsed from
	// X-RateLimit-Reset. Zero if the header was absent.
	Reset time.Time
	// RetryAfter is populated only on HTTP 429 — the caller is meant
	// to wait this long before trying again.
	RetryAfter time.Duration
	// ObservedAt is when this snapshot was recorded.
	ObservedAt time.Time
	// Path is the URL path of the request that produced the snapshot
	// (no query string; query may carry pagination cursors that aren't
	// useful here).
	Path string
	// Status is the HTTP status code of that response.
	Status int
}

// rateLimitTracker is a thread-safe holder for the most-recent snapshot.
// One instance per Client.
type rateLimitTracker struct {
	mu       sync.RWMutex
	snapshot RateLimitSnapshot
	haveSeen bool
}

func (t *rateLimitTracker) record(s RateLimitSnapshot) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.snapshot = s
	t.haveSeen = true
}

// latest returns the most recent snapshot. The bool is false when
// no Favro request has been observed yet.
func (t *rateLimitTracker) latest() (RateLimitSnapshot, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.snapshot, t.haveSeen
}

// parseRateLimitHeaders extracts the rate-limit signal from a response.
// Missing headers leave the corresponding field at its zero value
// (Remaining is -1 to keep "absent" distinguishable from "0 left").
func parseRateLimitHeaders(resp *http.Response) RateLimitSnapshot {
	s := RateLimitSnapshot{
		Remaining:  -1,
		ObservedAt: time.Now(),
		Status:     resp.StatusCode,
	}
	if resp.Request != nil && resp.Request.URL != nil {
		s.Path = resp.Request.URL.Path
	}
	if v := resp.Header.Get(headerRateLimitLimit); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			s.Limit = n
		}
	}
	if v := resp.Header.Get(headerRateLimitRemaining); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			s.Remaining = n
		}
	}
	if v := resp.Header.Get(headerRateLimitReset); v != "" {
		if epoch, err := strconv.ParseInt(v, 10, 64); err == nil {
			s.Reset = time.Unix(epoch, 0)
		}
	}
	if v := resp.Header.Get(headerRetryAfter); v != "" {
		s.RetryAfter = parseRetryAfter(v)
	}
	return s
}

// parseRetryAfter handles the two RFC 7231 forms: an integer number of
// seconds, or an HTTP-date. Returns 0 on parse failure.
func parseRetryAfter(v string) time.Duration {
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}
