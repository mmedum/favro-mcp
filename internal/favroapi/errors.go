package favroapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/mmedum/favro-mcp/internal/render"
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
	return fmt.Sprintf("Favro authentication failed (HTTP %d) — check FAVRO_USER_EMAIL and FAVRO_API_TOKEN", e.Status)
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

// MoveIgnoredError is a 200 that did not move the card. Favro answers
// a column move that omits widgetCommonId with HTTP 200 and a stub
// body — cardId, cardCommonId, name and timeOnBoard, no columnId —
// leaving the card where it was. UpdateCard refuses that shape before
// it reaches the network, so what raises this is a well-formed move
// Favro accepted and ignored for some other reason.
//
// It exists because hard rule 2 is not satisfied by reading the status
// line: the response to the write is the cheapest read-back there is,
// and the field the caller asked to change is in it.
type MoveIgnoredError struct {
	// Want is the requested columnId, Got the one Favro answered with.
	// Got is empty when the response carried no column at all, which
	// is the stub shape.
	Want string
	Got  string
}

func (e *MoveIgnoredError) Error() string {
	got := "no column at all"
	if e.Got != "" {
		got = "column " + e.Got
	}
	return fmt.Sprintf("Favro accepted the move and did not perform it: the card came back in %s, not the requested column %s", got, e.Want)
}

// The error vocabulary, named by the errors themselves.
//
// A class used to be read off these types from outside, by a switch in
// internal/render. That worked and was wrong in one specific way: an
// eighth error type added here would have fallen through that switch to
// the ClassInvalid fallback, and nothing would have failed. Naming the
// class at the type makes forgetting it a compile-time concern instead
// — TestEveryErrorTypeNamesItsClass reads this file and requires each
// of them to have one.

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

// ErrorClass reports a write Favro accepted and did not perform.
//
// Not ClassInvalid: the arguments passed every check this client makes
// and Favro raised no complaint about them, so telling the caller to
// change them sends it looking in the wrong place. Not ClassConflict
// either — that means the resource moved underneath the caller, which
// is a specific story there is no evidence for. What is left is
// "Favro failed", which is what ClassUnavailable says.
//
// It is the closest of the nine and it is not exact: unavailable tells
// the caller to retry later, and this result is deterministic, so a
// retry returns it forever. A class meaning "accepted and dropped;
// report it" is §17 decision 5, held open until a second path needs one
// rather than settled by the first.
func (e *MoveIgnoredError) ErrorClass() render.Class { return render.ClassUnavailable }

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
