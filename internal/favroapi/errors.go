package favroapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/mmedum/favro-mcp/v4/internal/config"
	"github.com/mmedum/favro-mcp/v4/internal/render"
)

// AuthError indicates Favro rejected the credentials at the auth
// layer (HTTP 401). Never embeds the email or token in its message.
//
// HTTP 403 is NOT mapped here: 403 means the token authenticated but
// the specific resource is not accessible (it may not exist, or the
// token may lack permission). That case is *ForbiddenError so the
// LLM doesn't get a misleading "check your env vars" message when
// it asks for a card / user / collection that's simply not visible
// to its scope.
type AuthError struct {
	Status int
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("Favro authentication failed (HTTP %d) — check %s and %s",
		e.Status, config.EnvUserEmail, config.EnvAPIToken)
}

// ForbiddenError is returned for HTTP 403 — the credentials are
// valid (otherwise Favro would have answered 401), but the request
// can't proceed against this specific resource. In practice Favro
// uses 403 both for permission denials and for resources the token
// can't see (Favro chooses 403 over 404 to avoid leaking existence);
// from a caller's perspective these are functionally equivalent
// "you can't get this" outcomes that are distinct from "your auth
// is broken".
type ForbiddenError struct {
	Status int
	Path   string
}

func (e *ForbiddenError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("Favro denied access (HTTP %d): the resource may not exist or the token lacks permission for it", e.Status)
	}
	return fmt.Sprintf("Favro denied access at %s (HTTP %d): the resource may not exist or the token lacks permission for it", e.Path, e.Status)
}

// RateLimitError is returned when Favro responds with HTTP 429.
// RetryAfter is the parsed `Retry-After` header, zero when it was absent
// or unparseable.
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

// AmbiguousWriteError is a write that may have reached Favro and whose
// answer did not come back: a 5xx, or a connection lost after the
// request went out. Favro does not deduplicate writes, so it is never
// sent again, and the caller is told to read before it does anything.
type AmbiguousWriteError struct {
	Method string
	Path   string
	Err    error
}

func (e *AmbiguousWriteError) Error() string {
	return fmt.Sprintf("Favro %s %s may have been applied: %v. Do not repeat it; read the resource first to see whether it took effect",
		e.Method, e.Path, e.Err)
}

func (e *AmbiguousWriteError) Unwrap() error { return e.Err }

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

// WriteIgnoredError is a 200 that did not do what was asked: Favro
// accepted the request, raised no complaint, and the field the caller
// asked to change came back unchanged.
//
// It exists because a 200 from Favro is not confirmation, and the
// write's own response already carries the field. That makes it
// evidence rather than a read-back — a response echoing the request
// while storing nothing would pass — but it costs no call, and it
// catches the shape Favro answers with, which is the field missing.
//
// UpdateCard raises it for a column move whose response carries another
// column or none. A move that omits widgetCommonId, which Favro answers
// with a stub card and no columnId, is refused before it is sent.
//
// A custom-field write to a field the card's widget has not enabled
// also answers 200, with the full card and the field absent. That case
// is not this error: verifyCustomField in internal/tools reads the card
// back and reports it as a note, because clearing a field leaves it
// absent too.
type WriteIgnoredError struct {
	// Field names what did not change, in the caller's vocabulary
	// ("column"), not the wire's.
	Field string
	// Want is what was asked for, Got what came back. Got is empty
	// when the response carried no value at all.
	Want string
	Got  string
}

func (e *WriteIgnoredError) Error() string {
	got := "nothing"
	if e.Got != "" {
		got = e.Got
	}
	return fmt.Sprintf("Favro accepted the write and did not perform it: the card's %s came back as %s, not the requested %s. The same call will do the same thing — report it rather than retrying",
		e.Field, got, e.Want)
}

// The error vocabulary, named by the errors themselves.
//
// Each type names its own class, because a switch elsewhere would let a
// new type fall through to the ClassInvalid fallback with nothing
// failing. TestEveryErrorTypeNamesItsClass reads this file and requires
// each of them to have one.

// ErrorClass reports that credentials were rejected: a human has to fix
// this, and no retry will.
func (e *AuthError) ErrorClass() render.Class { return render.ClassAuth }

// ErrorClass reports a 403. Deliberately not not_found: Favro answers
// 403 both for "no permission" and for "exists but not visible", and
// collapsing the two would tell a caller to stop looking for something
// that is there.
func (e *ForbiddenError) ErrorClass() render.Class { return render.ClassForbidden }

// ErrorClass reports a rate limit, which is distinct from unavailable
// because the caller's correct move is to wait a named duration.
func (e *RateLimitError) ErrorClass() render.Class { return render.ClassRateLimited }

// RetryAfterSeconds implements render.Retryable. It rounds up, so a
// wait Favro reported in milliseconds never reads as "retry now".
func (e *RateLimitError) RetryAfterSeconds() int {
	if e.RetryAfter <= 0 {
		return 0
	}
	secs := int((e.RetryAfter + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	return secs
}

// ErrorClass reports a 404.
func (e *NotFoundError) ErrorClass() render.Class { return render.ClassNotFound }

// ErrorClass reports that Favro rejected the request shape, which is
// the caller's arguments.
func (e *ValidationError) ErrorClass() render.Class { return render.ClassInvalid }

// ErrorClass reports a 5xx that survived the retry budget.
func (e *TransientError) ErrorClass() render.Class { return render.ClassUnavailable }

// ErrorClass reports a write that may have happened. It wins over the
// class of what it wraps, because unavailable says to retry.
func (e *AmbiguousWriteError) ErrorClass() render.Class { return render.ClassAmbiguousOutcome }

// ErrorClass reports a write Favro accepted and did not perform.
//
// Not ClassInvalid: the arguments passed every check this client makes
// and Favro raised no complaint about them, so telling the caller to
// change them sends it looking in the wrong place. Not ClassConflict
// either — that means the resource moved underneath the caller, which
// is a specific story there is no evidence for. What is left is
// "Favro failed", which is what ClassUnavailable says.
//
// It is the closest class and not an exact one: unavailable tells the
// caller to retry later, and this result is deterministic, so a retry
// returns it forever. The message carries the correction, since every
// tool error is "[class] actionable message" from a closed vocabulary.
// Whether this failure needs a class of its own is an open question.
func (e *WriteIgnoredError) ErrorClass() render.Class { return render.ClassUnavailable }

// ErrorClass maps the statuses *APIError can actually carry.
//
// That set is much smaller than it looks. APIError is built in one
// place — classifyClientError — and only after Do has peeled off 2xx,
// 429 and every 5xx, and after 401, 403, 404, 400 and 422 have become
// their own typed errors. So what reaches here is 3xx and the leftover
// 4xx, and a branch for 404 or 502 would be a second copy of Do's table
// that no test could exercise and nothing would keep in step.
func (e *APIError) ErrorClass() render.Class {
	switch e.Status {
	case http.StatusConflict:
		return render.ClassConflict
	case http.StatusMethodNotAllowed:
		return render.ClassUnsupported
	default:
		return render.ClassInvalid
	}
}
