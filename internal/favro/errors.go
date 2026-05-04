package favro

import (
	"fmt"
	"time"
)

// AuthError indicates Favro rejected the credentials (HTTP 401 or 403).
// Never embeds the email or token in its message.
type AuthError struct {
	Status int
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("Favro authentication failed (HTTP %d) — check FAVRO_USER_EMAIL and FAVRO_API_TOKEN", e.Status)
}

// RateLimitError is returned when Favro responds with HTTP 429.
// RetryAfter carries the parsed `Retry-After` header (or the time
// until X-RateLimit-Reset, whichever is available); zero if neither
// header was present.
type RateLimitError struct {
	RetryAfter time.Duration
	Status     int
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("Favro rate limit exceeded (HTTP %d); retry after %s", e.Status, e.RetryAfter)
	}
	return fmt.Sprintf("Favro rate limit exceeded (HTTP %d)", e.Status)
}

// NotFoundError is returned for HTTP 404. Resource is the resource
// kind (e.g. "card", "widget"); ID is optional.
type NotFoundError struct {
	Resource string
	ID       string
	Path     string
}

func (e *NotFoundError) Error() string {
	switch {
	case e.ID != "" && e.Resource != "":
		return fmt.Sprintf("Favro %s %q not found", e.Resource, e.ID)
	case e.Resource != "":
		return fmt.Sprintf("Favro %s not found", e.Resource)
	case e.Path != "":
		return fmt.Sprintf("Favro resource not found at %s", e.Path)
	default:
		return "Favro resource not found"
	}
}

// ValidationError is returned for HTTP 400 / 422 — Favro rejected the
// request shape. Body carries the truncated server response so the
// caller can surface it; callers should not log Body verbatim if it
// might contain echoed credentials (Favro doesn't echo Authorization,
// so this is normally safe).
type ValidationError struct {
	Status int
	Body   string
}

func (e *ValidationError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("Favro validation error (HTTP %d)", e.Status)
	}
	return fmt.Sprintf("Favro validation error (HTTP %d): %s", e.Status, e.Body)
}

// TransientError is returned for any 5xx response after the retry
// budget is exhausted. Attempts is the number of attempts made,
// including the original.
type TransientError struct {
	Status   int
	Attempts int
}

func (e *TransientError) Error() string {
	return fmt.Sprintf("Favro transient failure (HTTP %d after %d attempts)", e.Status, e.Attempts)
}

// APIError is the catch-all for any non-success status the typed
// errors above don't cover.
type APIError struct {
	Status int
	Body   string
	Path   string
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("Favro API error (HTTP %d) at %s", e.Status, e.Path)
	}
	return fmt.Sprintf("Favro API error (HTTP %d) at %s: %s", e.Status, e.Path, e.Body)
}
