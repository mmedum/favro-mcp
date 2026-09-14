package favroapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestListOrganizations_DefaultPage(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Organization]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-orgs",
			Entities: []favro.Organization{
				{OrganizationID: "org-1", Name: "Acme Corp"},
				{OrganizationID: "org-2", Name: "Initech"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListOrganizations(context.Background(), 0, "")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := env.RequestID; got != "req-orgs" {
		t.Errorf("env.RequestID = %v, want %v", got, "req-orgs")
	}
	if len(env.Entities) != 2 {
		t.Fatalf("len(env.Entities) = %d, want 2", len(env.Entities))
	}
	if got := env.Entities[0].Name; got != "Acme Corp" {
		t.Errorf("env.Entities[0].Name = %v, want %v", got, "Acme Corp")
	}

	rec := h.seen()
	if len(rec) != 1 {
		t.Fatalf("len(rec) = %d, want 1", len(rec))
	}
	if got := rec[0].Path; got != "/organizations" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/organizations")
	}
	if len(rec[0].Query.Get("page")) != 0 {
		t.Errorf("page=0 must NOT add ?page= to the request: got %v", rec[0].Query.Get("page"))
	}
}

func TestListOrganizations_WithPage(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Organization]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListOrganizations(context.Background(), 2, "req-prior")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	rec := h.seen()
	if got := rec[0].Query.Get("page"); got != "2" {
		t.Errorf("rec[0].Query.Get(\"page\") = %v, want %v", got, "2")
	}
	if got := rec[0].Headers.Get(headerRequestID); got != "req-prior" {
		t.Errorf("page > 0 must forward the prior requestId as X-Favro-Backend-Identifier: got %v, want %v", got, "req-prior")
	}
}

func TestListOrganizations_PropagatesAuthError(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnauthorized)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListOrganizations(context.Background(), 0, "")
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("got %v, want ae", err)
	}
}

func TestGetOrganization_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Organization{
			OrganizationID: "org-xyz",
			Name:           "Test Org",
			SharedToUsers: []favro.SharedUser{
				{UserID: "u-1", Role: "admin"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	org, err := c.GetOrganization(context.Background(), "org-xyz")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := org.OrganizationID; got != "org-xyz" {
		t.Errorf("org.OrganizationID = %v, want %v", got, "org-xyz")
	}
	if got := org.Name; got != "Test Org" {
		t.Errorf("org.Name = %v, want %v", got, "Test Org")
	}
	if len(org.SharedToUsers) != 1 {
		t.Fatalf("len(org.SharedToUsers) = %d, want 1", len(org.SharedToUsers))
	}
	if got := org.SharedToUsers[0].Role; got != "admin" {
		t.Errorf("org.SharedToUsers[0].Role = %v, want %v", got, "admin")
	}

	rec := h.seen()
	if got := rec[0].Path; got != "/organizations/org-xyz" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/organizations/org-xyz")
	}
}

func TestGetOrganization_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetOrganization(context.Background(), "")
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("empty id must short-circuit before any network call: got %v", h.seen())
	}
}

func TestGetOrganization_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetOrganization(context.Background(), "missing-org")
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want nf", err)
	}
}

func TestGetOrganization_PathEscapesID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Organization{OrganizationID: "weird/id"})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	// Real Favro org ids are 24-char hex, so this is a defensive check
	// that PathEscape would protect against future surprises.
	_, err := c.GetOrganization(context.Background(), "a/b")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	rec := h.seen()
	// Go's net/url leaves "%2F" encoded in URL.Path on purpose — if
	// it decoded, the slash would be indistinguishable from a path
	// separator. So the slash in the id is preserved as "%2F" all
	// the way to the server, which is exactly what we want.
	if got := rec[0].Path; got != "/organizations/a%2Fb" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/organizations/a%2Fb")
	}
}

// TestListOrganizations_NewFieldsDecoding pins decode for the
// Phase 4.5 additions: favro.Organization.Thumbnail and
// favro.SharedUser.JoinDate.
func TestListOrganizations_NewFieldsDecoding(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"requestId":"r","entities":[{
			"organizationId":"org-1","name":"Acme",
			"thumbnail":"https://favro.invalid/t/org-1.png",
			"sharedToUsers":[
				{"userId":"u-1","role":"administrator","joinDate":"2025-01-15T00:00:00Z"}
			]
		}]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListOrganizations(context.Background(), 0, "")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(env.Entities) != 1 {
		t.Fatalf("len(env.Entities) = %d, want 1", len(env.Entities))
	}
	if got := env.Entities[0].Thumbnail; got != "https://favro.invalid/t/org-1.png" {
		t.Errorf("env.Entities[0].Thumbnail = %v, want %v", got, "https://favro.invalid/t/org-1.png")
	}
	if len(env.Entities[0].SharedToUsers) != 1 {
		t.Fatalf("len(env.Entities[0].SharedToUsers) = %d, want 1", len(env.Entities[0].SharedToUsers))
	}
	if got := env.Entities[0].SharedToUsers[0].JoinDate; got != "2025-01-15T00:00:00Z" {
		t.Errorf("env.Entities[0].SharedToUsers[0].JoinDate = %v, want %v", got, "2025-01-15T00:00:00Z")
	}
}
