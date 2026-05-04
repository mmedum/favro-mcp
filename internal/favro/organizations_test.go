package favro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListOrganizations_DefaultPage(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Organization]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-orgs",
			Entities: []Organization{
				{OrganizationID: "org-1", Name: "Acme Corp"},
				{OrganizationID: "org-2", Name: "Initech"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListOrganizations(context.Background(), 0, "")
	require.NoError(t, err)
	require.Equal(t, "req-orgs", env.RequestID)
	require.Len(t, env.Entities, 2)
	require.Equal(t, "Acme Corp", env.Entities[0].Name)

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, "/organizations", rec[0].Path)
	require.Empty(t, rec[0].Query.Get("page"), "page=0 must NOT add ?page= to the request")
}

func TestListOrganizations_WithPage(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Organization]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListOrganizations(context.Background(), 2, "req-prior")
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "2", rec[0].Query.Get("page"))
	require.Equal(t, "req-prior", rec[0].Headers.Get(headerRequestID),
		"page > 0 must forward the prior requestId as X-Favro-Backend-Identifier")
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
	require.ErrorAs(t, err, &ae)
}

func TestGetOrganization_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Organization{
			OrganizationID: "org-xyz",
			Name:           "Test Org",
			SharedToUsers: []OrgMember{
				{UserID: "u-1", Role: "admin"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	org, err := c.GetOrganization(context.Background(), "org-xyz")
	require.NoError(t, err)
	require.Equal(t, "org-xyz", org.OrganizationID)
	require.Equal(t, "Test Org", org.Name)
	require.Len(t, org.SharedToUsers, 1)
	require.Equal(t, "admin", org.SharedToUsers[0].Role)

	rec := h.seen()
	require.Equal(t, "/organizations/org-xyz", rec[0].Path)
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
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen(), "empty id must short-circuit before any network call")
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
	require.ErrorAs(t, err, &nf)
}

func TestGetOrganization_PathEscapesID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Organization{OrganizationID: "weird/id"})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	// Real Favro org ids are 24-char hex, so this is a defensive check
	// that PathEscape would protect against future surprises.
	_, err := c.GetOrganization(context.Background(), "a/b")
	require.NoError(t, err)

	rec := h.seen()
	// Go's net/url leaves "%2F" encoded in URL.Path on purpose — if
	// it decoded, the slash would be indistinguishable from a path
	// separator. So the slash in the id is preserved as "%2F" all
	// the way to the server, which is exactly what we want.
	require.Equal(t, "/organizations/a%2Fb", rec[0].Path)
}
