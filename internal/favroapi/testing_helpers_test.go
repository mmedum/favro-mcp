package favroapi

import (
	"fmt"
	"net/http"
	"testing"
)

// failingRoundTripper fails the test the moment any HTTP request is
// dispatched. Used as a strict regression for "dry-run never hits
// the network" — a counter-based test can drift if the dry-run
// gate is moved; a RoundTripper that errors on entry catches every
// future regression. Shared across every resource's
// mutating-method test (tags, comments, etc.).
type failingRoundTripper struct {
	t *testing.T
}

func (f *failingRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("dry-run regression: transport.RoundTrip called for %s %s", r.Method, r.URL.Path)
	return nil, fmt.Errorf("RoundTrip must not be called in dry-run mode")
}

// testEntity is a stand-in resource for the pagination tests: the
// envelope's behaviour does not depend on what it carries, and using a
// real wire type here would tie a transport test to a schema.
type testEntity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
