package render

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
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
			if got := Classify(tc.err); got != tc.want {
				t.Errorf("Classify(tc.err) = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestClassifyWalksTheChain is the property everything else depends
// on: an error is wrapped with fmt.Errorf("%w: …") at the point it is
// raised, so a classifier that only looked at the outermost error would
// classify every one of them as the fallback.
func TestClassifyWalksTheChain(t *testing.T) {
	t.Parallel()

	if got := Classify(fmt.Errorf("%w (3 matches for %q)", selfClassed{ClassAmbiguous}, "a name")); got != ClassAmbiguous {
		t.Errorf("Classify(fmt.Errorf(\"%%w (3 matches for %%q)\", selfClassed{ClassAmbiguous}, \"a name\")) = %v, want %v", got, ClassAmbiguous)
	}
	if got := Classify(fmt.Errorf("looking up: %w", selfClassed{ClassNotFound})); got != ClassNotFound {
		t.Errorf("Classify(fmt.Errorf(\"looking up: %%w\", selfClassed{ClassNotFound})) = %v, want %v", got, ClassNotFound)
	}
}

// TestClassifyNetworkError covers the layer below the typed errors: a
// request that never reached Favro is about the network, not about
// what the caller asked for.
func TestClassifyNetworkError(t *testing.T) {
	t.Parallel()

	var netErr net.Error = &net.DNSError{Err: "no such host", IsNotFound: true}
	if got := Classify(fmt.Errorf("get: %w", netErr)); got != ClassUnavailable {
		t.Errorf("Classify(fmt.Errorf(\"get: %%w\", netErr)) = %v, want %v", got, ClassUnavailable)
	}
}

func TestError(t *testing.T) {
	t.Parallel()

	if err := Error(nil); err != nil {
		t.Fatalf("Error(nil): %v", err)
	}
	if got := Error(selfClassed{ClassNotFound}).Error(); got != "[not_found] sentinel" {
		t.Errorf("Error(selfClassed{ClassNotFound}).Error() = %v, want %v", got, "[not_found] sentinel")
	}

	// The one class with a machine-readable operand. It goes in the
	// text because a failed call has no structuredContent to put it in,
	// and it is asked of the error through an interface so that this
	// package stays a leaf.
	got := Error(retryAfter{ClassRateLimited, 90})
	if !strings.Contains(got.Error(), "[rate_limited]") {
		t.Errorf("got.Error() does not contain %q", "[rate_limited]")
	}
	if !strings.Contains(got.Error(), "retry_after_seconds=90") {
		t.Errorf("got.Error() does not contain %q", "retry_after_seconds=90")
	}

	// An error that reports no wait says nothing it does not know.
	got = Error(retryAfter{ClassRateLimited, 0})
	if !strings.Contains(got.Error(), "[rate_limited]") {
		t.Errorf("got.Error() does not contain %q", "[rate_limited]")
	}
	if strings.Contains(got.Error(), "retry_after_seconds") {
		t.Errorf("got.Error() unexpectedly contains %q", "retry_after_seconds")
	}

	// The operand is only meaningful on that one class.
	got = Error(retryAfter{ClassUnavailable, 30})
	if got := got.Error(); got != "[unavailable] sentinel" {
		t.Errorf("got.Error() = %v, want %v", got, "[unavailable] sentinel")
	}
}

// TestClassValuesAreWireSafe pins the spelling: a class is part of
// every error message a caller parses, so it stays lowercase snake.
func TestClassValuesAreWireSafe(t *testing.T) {
	t.Parallel()

	for _, c := range Classes {
		s := string(c)
		if len(s) == 0 {
			t.Fatal("s is empty")
		}
		if got := s; got != strings.ToLower(s) {
			t.Errorf("class %q must be lowercase: got %v, want %v", c, got, strings.ToLower(s))
		}
		if strings.Contains(s, " ") {
			t.Errorf("class %q must not contain a space: %q present", c, " ")
		}
		if strings.Contains(s, "]") {
			t.Errorf("class %q would break the [class] prefix: %q present", c, "]")
		}
	}
}
