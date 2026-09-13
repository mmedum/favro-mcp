package render

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// selfClassed is the shape the server package's sentinels have: an
// error that names its own class.
type selfClassed struct{ class Class }

func (e selfClassed) Error() string     { return "sentinel" }
func (e selfClassed) ErrorClass() Class { return e.class }

func TestClassify(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want Class
	}{
		{"nil", nil, ClassInvalid},
		{"401", &favro.AuthError{Status: 401}, ClassAuth},
		{"403", &favro.ForbiddenError{Status: 403, Path: "/cards/x"}, ClassForbidden},
		{"429", &favro.RateLimitError{Status: 429, RetryAfter: 30 * time.Second}, ClassRateLimited},
		{"404", &favro.NotFoundError{Resource: "card"}, ClassNotFound},
		{"400", &favro.ValidationError{Status: 400}, ClassInvalid},
		{"5xx after retries", &favro.TransientError{Status: 503, Attempts: 4}, ClassUnavailable},
		// *APIError carries only what the client did not turn into a
		// typed error first: 3xx and the leftover 4xx. 404, 429 and
		// 5xx never reach it, which is why classifyStatus has no
		// branch for them.
		{"409 via APIError", &favro.APIError{Status: http.StatusConflict}, ClassConflict},
		{"405 via APIError", &favro.APIError{Status: http.StatusMethodNotAllowed}, ClassUnsupported},
		{"410 via APIError", &favro.APIError{Status: http.StatusGone}, ClassInvalid},
		{"418 via APIError", &favro.APIError{Status: http.StatusTeapot}, ClassInvalid},
		{"self-classed wins", selfClassed{ClassAmbiguous}, ClassAmbiguous},
		{"context cancelled", context.Canceled, ClassUnavailable},
		{"deadline", context.DeadlineExceeded, ClassUnavailable},
		{"unclassified falls back", errors.New("something the server layer raised"), ClassInvalid},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, Classify(tc.err))
		})
	}
}

// TestClassifyWalksTheChain is the property everything else depends
// on: the sentinels are wrapped with fmt.Errorf("%w: …") at the point
// they are raised, so a classifier that only looked at the outermost
// error would classify every one of them as the fallback.
func TestClassifyWalksTheChain(t *testing.T) {
	t.Parallel()

	require.Equal(t, ClassAmbiguous,
		Classify(fmt.Errorf("%w (3 matches for %q)", selfClassed{ClassAmbiguous}, "a name")))
	require.Equal(t, ClassNotFound,
		Classify(fmt.Errorf("looking up: %w", &favro.NotFoundError{Resource: "tag"})))
}

// TestClassifyNetworkError covers the layer below the typed errors: a
// request that never reached Favro is about the network, not about
// what the caller asked for.
func TestClassifyNetworkError(t *testing.T) {
	t.Parallel()

	var netErr net.Error = &net.DNSError{Err: "no such host", IsNotFound: true}
	require.Equal(t, ClassUnavailable, Classify(fmt.Errorf("get: %w", netErr)))
}

func TestError(t *testing.T) {
	t.Parallel()

	require.NoError(t, Error(nil))

	got := Error(&favro.NotFoundError{Resource: "card", ID: "x"})
	require.Equal(t, `[not_found] Favro card "x" not found`, got.Error())

	// The one class with a machine-readable operand. It goes in the
	// text because a failed call has no structuredContent to put it in.
	got = Error(&favro.RateLimitError{Status: 429, RetryAfter: 90 * time.Second})
	require.Contains(t, got.Error(), "[rate_limited]")
	require.Contains(t, got.Error(), "retry_after_seconds=90")

	// A rate limit with no Retry-After still classifies, and says
	// nothing it does not know.
	got = Error(&favro.RateLimitError{Status: 429})
	require.Contains(t, got.Error(), "[rate_limited]")
	require.NotContains(t, got.Error(), "retry_after_seconds")

	// Sub-second waits round up rather than to "wait 0 seconds".
	got = Error(&favro.RateLimitError{Status: 429, RetryAfter: 200 * time.Millisecond})
	require.Contains(t, got.Error(), "retry_after_seconds=1")
}

// TestClassValuesAreWireSafe pins the spelling: a class is part of
// every error message a caller parses, so it stays lowercase snake.
func TestClassValuesAreWireSafe(t *testing.T) {
	t.Parallel()

	for _, c := range Classes {
		s := string(c)
		require.NotEmpty(t, s)
		require.Equal(t, strings.ToLower(s), s, "class %q must be lowercase", c)
		require.NotContains(t, s, " ", "class %q must not contain a space", c)
		require.NotContains(t, s, "]", "class %q would break the [class] prefix", c)
	}
}

// TestEveryFavroErrorTypeIsClassified is the other half of the
// vocabulary's coverage, and the one the sentinels do not give.
//
// A sentinel in internal/server names its own class, so a new one that
// forgot to would fail TestEverySentinelIsClassified. The typed errors
// in internal/favro do not: Classify reads them from the outside, and
// an eighth type added there would fall past classifyFavro, past
// classifyTransport, and land on the ClassInvalid fallback with
// nothing failing and the `classes` gate still reporting the
// vocabulary consistent.
//
// So the list of types is read from the source and the expectations
// are held against it. The deeper fix is to let those types name their
// own class the way the sentinels do, which needs Class to live in a
// package internal/favro can import — a layout question, and A3 is
// where layout is decided.
func TestEveryFavroErrorTypeIsClassified(t *testing.T) {
	t.Parallel()

	expected := map[string]Class{
		"AuthError":       ClassAuth,
		"ForbiddenError":  ClassForbidden,
		"RateLimitError":  ClassRateLimited,
		"NotFoundError":   ClassNotFound,
		"ValidationError": ClassInvalid,
		"TransientError":  ClassUnavailable,
		"APIError":        ClassInvalid, // by status; see classifyStatus
	}
	samples := map[string]error{
		"AuthError":       &favro.AuthError{Status: 401},
		"ForbiddenError":  &favro.ForbiddenError{Status: 403},
		"RateLimitError":  &favro.RateLimitError{Status: 429},
		"NotFoundError":   &favro.NotFoundError{Resource: "card"},
		"ValidationError": &favro.ValidationError{Status: 400},
		"TransientError":  &favro.TransientError{Status: 503, Attempts: 4},
		"APIError":        &favro.APIError{Status: 410},
	}

	declared := declaredErrorTypes(t)
	require.GreaterOrEqual(t, len(declared), 7,
		"read %d error types out of internal/favro/errors.go", len(declared))

	for _, name := range declared {
		want, ok := expected[name]
		require.True(t, ok,
			"internal/favro declares %s and Classify has no case for it — it would reach the ClassInvalid fallback silently", name)
		require.Equal(t, want, Classify(samples[name]))
	}
	for name := range expected {
		require.Contains(t, declared, name, "%s is expected here but no longer declared", name)
	}
}
