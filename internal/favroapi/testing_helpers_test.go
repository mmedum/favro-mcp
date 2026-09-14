package favroapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
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

// requireJSONEq fails unless want and got are the same JSON document,
// ignoring key order and whitespace.
//
// The request-body assertions need this and `==` will not do it: Go's
// map iteration means encoding/json writes object keys in sorted order,
// but the expected literals in these tests are written the way a person
// reads them, and a body built by appending fields is not textually
// equal to one built by a struct. Comparing the decoded values is the
// only comparison that means "the same request".
func requireJSONEq(t *testing.T, want, got string, why ...string) {
	t.Helper()

	var wantVal, gotVal any
	if err := json.Unmarshal([]byte(want), &wantVal); err != nil {
		t.Fatalf("the expected JSON does not parse: %v: %s", err, want)
	}
	if err := json.Unmarshal([]byte(got), &gotVal); err != nil {
		t.Fatalf("the body is not JSON: %v: %s", err, got)
	}
	if !reflect.DeepEqual(wantVal, gotVal) {
		// why is what the assertion was for, when the caller said —
		// several of these exist to pin a marshalling detail that the
		// bodies alone would not explain.
		t.Errorf("request body: %s\n got %s\nwant %s", strings.Join(why, " "), got, want)
	}
}
