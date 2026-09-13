package render

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// selfClassed is the shape every error in this repository has: one that
// names its own class.
type selfClassed struct{ class Class }

func (e selfClassed) Error() string     { return "sentinel" }
func (e selfClassed) ErrorClass() Class { return e.class }

// retryAfter additionally reports a wait, the way the client's rate
// limit error does.
type retryAfter struct {
	class Class
	secs  int
}

func (e retryAfter) Error() string          { return "sentinel" }
func (e retryAfter) ErrorClass() Class      { return e.class }
func (e retryAfter) RetryAfterSeconds() int { return e.secs }

func TestClassify(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want Class
	}{
		{"nil", nil, ClassInvalid},
		{"self-classed wins", selfClassed{ClassAmbiguous}, ClassAmbiguous},
		{"self-classed, any member", selfClassed{ClassUnsupported}, ClassUnsupported},
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
// on: an error is wrapped with fmt.Errorf("%w: …") at the point it is
// raised, so a classifier that only looked at the outermost error would
// classify every one of them as the fallback.
func TestClassifyWalksTheChain(t *testing.T) {
	t.Parallel()

	require.Equal(t, ClassAmbiguous,
		Classify(fmt.Errorf("%w (3 matches for %q)", selfClassed{ClassAmbiguous}, "a name")))
	require.Equal(t, ClassNotFound,
		Classify(fmt.Errorf("looking up: %w", selfClassed{ClassNotFound})))
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
	require.Equal(t, "[not_found] sentinel", Error(selfClassed{ClassNotFound}).Error())

	// The one class with a machine-readable operand. It goes in the
	// text because a failed call has no structuredContent to put it in,
	// and it is asked of the error through an interface so that this
	// package stays a leaf.
	got := Error(retryAfter{ClassRateLimited, 90})
	require.Contains(t, got.Error(), "[rate_limited]")
	require.Contains(t, got.Error(), "retry_after_seconds=90")

	// An error that reports no wait says nothing it does not know.
	got = Error(retryAfter{ClassRateLimited, 0})
	require.Contains(t, got.Error(), "[rate_limited]")
	require.NotContains(t, got.Error(), "retry_after_seconds")

	// The operand is only meaningful on that one class.
	got = Error(retryAfter{ClassUnavailable, 30})
	require.Equal(t, "[unavailable] sentinel", got.Error())
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
