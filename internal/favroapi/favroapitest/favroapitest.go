// Package favroapitest is the Favro client fixture the test packages
// above internal/favroapi share.
//
// It exists because the credential triple is load-bearing: hard rule 1
// says nothing in this repository may carry a real tenant's values, and
// a fixture repeated per package is a fixture that drifts. It drifted
// once already — the copies in internal/service and internal/tools had
// grown two different sets of invented values and two different
// comments explaining the same rule — which is the argument for one
// copy in one place.
//
// It is a non-test package so that several test packages can import it;
// GoReleaser builds only ./cmd/..., so none of it ships.
package favroapitest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mmedum/favro-mcp/internal/auth"
	"github.com/mmedum/favro-mcp/internal/favroapi"
)

// Token is a credential triple shaped like a credential triple and
// belonging to nobody. Every value says in itself that it is invented,
// which is what keeps the leak gate from reporting the files that use
// it — and what makes it obvious to a reader that nothing here came
// from a real organization.
func Token() auth.Token {
	return auth.Token{
		Email:          "fixture@example.test",
		APIToken:       "synthetic-token-for-tests",
		OrganizationID: "synthetic-organization",
	}
}

// Client wires a Favro client to an httptest server backed by handler.
// The server closes when the test ends.
func Client(t *testing.T, handler http.Handler) *favroapi.Client {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := favroapi.NewClient(Token())
	c.BaseURL = srv.URL
	c.HTTPClient = srv.Client()
	return c
}
