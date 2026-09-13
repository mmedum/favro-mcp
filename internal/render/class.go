// Package render turns a tool's outcome into the two halves an MCP
// client shows: the readable `content` block and the machine-readable
// `structuredContent` one.
//
// It exists because the SDK, left alone, makes them the same bytes.
// When a handler returns a typed output and a nil *mcp.CallToolResult,
// mcp/server.go marshals the output into StructuredContent and — seeing
// Content unset — puts the identical serialized JSON in a TextContent
// block. Every tool in this repository was in that state. A client that
// shows only one half then has the same thing either way, and the
// standard's §2 asks for two halves that differ.
//
// The error vocabulary lives here for the same reason: a class is a
// presentation decision about an error, not a property of the wire.
package render

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// Class is one member of the closed error vocabulary. Every error a
// tool returns is rendered as "[class] actionable message", and the
// `classes` gate holds this list and the table in
// docs/architecture.md §6.2 to each other from both sides: a class
// declared here and undocumented fails, and a documented class with no
// constant here fails too.
//
// Closed means closed. Adding a member is a deliberate act that edits
// the document in the same commit — which is the point, because the
// value of a vocabulary is entirely in the caller being able to switch
// on it.
type Class string

// The six from the shared standard.
const (
	// ClassInvalid is the request as given cannot be served: a missing
	// argument, two mutually exclusive ones, a value out of range. The
	// caller's move is to change the arguments.
	ClassInvalid Class = "invalid"
	// ClassNotFound is a resource, or a name, that is not there. The
	// caller's move is to stop looking, or to resolve a name first.
	ClassNotFound Class = "not_found"
	// ClassAuth is the credentials themselves. The caller cannot fix
	// this; a human has to.
	ClassAuth Class = "auth"
	// ClassConflict is a state collision — the resource moved under
	// the caller. Re-read, then retry.
	ClassConflict Class = "conflict"
	// ClassUnavailable is Favro, or the network to it, failing. Retry
	// later; the arguments are not the problem.
	ClassUnavailable Class = "unavailable"
	// ClassUnsupported is a thing this server will not do, as opposed
	// to one that failed. Retrying never helps.
	ClassUnsupported Class = "unsupported"
)

// The three this platform forces. §6.2 says why each one cannot be
// folded into a member above.
const (
	// ClassForbidden is Favro's 403, which it uses both for "no
	// permission" and for "exists but not visible to this token".
	// Collapsing it into ClassNotFound would tell a model to stop
	// looking for something that is there.
	ClassForbidden Class = "forbidden"
	// ClassRateLimited is 429, carrying retry_after_seconds. Distinct
	// from ClassUnavailable because the correct response is to wait a
	// named duration rather than to retry.
	ClassRateLimited Class = "rate_limited"
	// ClassAmbiguous is a name matching several candidates. The caller
	// must choose, not retry.
	ClassAmbiguous Class = "ambiguous"
)

// Classes is every class, in the order §6.2 lists them, for a caller
// that needs to range over the vocabulary.
//
// The `classes` gate reads the constants above, not this slice, and
// then requires the two to hold the same set — so a constant declared
// and never listed here fails, and a member here with no constant
// fails too.
var Classes = []Class{
	ClassInvalid,
	ClassNotFound,
	ClassAuth,
	ClassConflict,
	ClassUnavailable,
	ClassUnsupported,
	ClassForbidden,
	ClassRateLimited,
	ClassAmbiguous,
}

// Retryable is implemented by an error that knows how long the caller
// should wait. Only ClassRateLimited carries one.
type Retryable interface {
	// RetryAfterSeconds returns the wait in whole seconds, rounded up
	// so that a sub-second wait never reads as "retry immediately".
	// Zero means the server did not say.
	RetryAfterSeconds() int
}

// Classed is implemented by an error that names its own class. The
// sentinels in internal/tools and internal/service implement it, which is what keeps
// classification derived from the error rather than from a match on
// its text.
type Classed interface {
	ErrorClass() Class
}

// Classify returns the class of err.
//
// Every error this repository raises names its own class: the server's
// sentinels through classed(), and the client's typed errors through
// ErrorClass() methods on themselves. Classify walks the chain for one,
// then falls back to the transport layer, which is the only place an
// error arrives that nobody here constructed.
//
// The fallback is ClassInvalid, and that is a measured choice rather
// than a neutral one. Every error this server's own layer raises
// without a sentinel is an argument the caller got wrong — "pass
// exactly one of", "is required", "must be between" — so ClassInvalid
// tells the caller the true thing in every case that exists today. A
// server bug arriving here would be mislabelled; that is the trade,
// and TestEverySentinelIsClassified, in internal/tools, is what keeps
// the set of unclassified errors from growing quietly.
func Classify(err error) Class {
	if err == nil {
		return ClassInvalid
	}
	var classed Classed
	if errors.As(err, &classed) {
		return classed.ErrorClass()
	}
	if class, ok := classifyTransport(err); ok {
		return class
	}
	return ClassInvalid
}

// classifyTransport is the layer below the typed errors: a request that
// never reached Favro is about the network rather than about anything
// the caller asked for. context.Canceled is the host hanging up
// mid-call.
func classifyTransport(err error) (Class, bool) {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return ClassUnavailable, true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ClassUnavailable, true
	}
	return "", false
}

// Error renders err as the standard's "[class] actionable message".
//
// The returned error is what the handler hands the SDK, which puts its
// text in a TextContent block and sets IsError — errors are tool
// results, not protocol errors, so nothing here returns a
// *jsonrpc.Error.
func Error(err error) error {
	if err == nil {
		return nil
	}
	class := Classify(err)
	msg := strings.TrimSpace(err.Error())

	// A rate limit is the one class with a machine-readable operand.
	// It goes in the message because that is the only half of a failed
	// call a client is guaranteed to show: SetError puts the error text
	// in Content and there is no structuredContent on an error result.
	//
	// Asked of the error through an interface, not read off a concrete
	// type, so that this package stays a leaf: it is imported by the
	// one that defines the error.
	if class == ClassRateLimited {
		var retryable Retryable
		if errors.As(err, &retryable) {
			if secs := retryable.RetryAfterSeconds(); secs > 0 {
				return fmt.Errorf("[%s] %s (retry_after_seconds=%d)", class, msg, secs)
			}
		}
	}

	return fmt.Errorf("[%s] %s", class, msg)
}

// sentinel is an error that names its own class, for the packages
// above this one to declare theirs with.
type sentinel struct {
	class Class
	msg   string
}

func (e *sentinel) Error() string { return e.msg }

// ErrorClass satisfies Classed.
func (e *sentinel) ErrorClass() Class { return e.class }

// Sentinel builds a package-level error that carries its class, so the
// boundary can render "[class] message" without matching on the text.
//
// The class comes first because it is the part a reader checks when
// they look at one of these. Wrapping the result with
// fmt.Errorf("%w: …") keeps the class, since Classify walks the chain.
func Sentinel(class Class, msg string) error {
	return &sentinel{class: class, msg: msg}
}
