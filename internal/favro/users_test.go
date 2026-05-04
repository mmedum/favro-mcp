package favro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListUsers_DefaultPage(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[User]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-users",
			Entities: []User{
				{UserID: "u-1", Name: "Alice", Email: "alice@example.com", OrganizationRole: "fullMember"},
				{UserID: "u-2", Name: "Bob", OrganizationRole: "guest"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListUsers(context.Background(), 0, "")
	require.NoError(t, err)
	require.Equal(t, "req-users", env.RequestID)
	require.Len(t, env.Entities, 2)
	require.Equal(t, "Alice", env.Entities[0].Name)
	require.Equal(t, "alice@example.com", env.Entities[0].Email)
	require.Equal(t, "fullMember", env.Entities[0].OrganizationRole)
	require.Empty(t, env.Entities[1].Email, "users without email decode cleanly to empty string")

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, "/users", rec[0].Path)
	require.Empty(t, rec[0].Query.Get("page"), "page=0 must NOT add ?page= to the request")
}

func TestListUsers_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[User]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListUsers(context.Background(), 2, "req-prior")
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "2", rec[0].Query.Get("page"))
	require.Equal(t, "req-prior", rec[0].Headers.Get(headerRequestID),
		"page > 0 must forward the prior requestId as X-Favro-Backend-Identifier")
}

func TestListUsers_PropagatesAuthError(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnauthorized)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListUsers(context.Background(), 0, "")
	var ae *AuthError
	require.ErrorAs(t, err, &ae)
}

func TestGetUser_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(User{
			UserID:           "u-xyz",
			Name:             "Charlie",
			Email:            "charlie@example.com",
			OrganizationRole: "administrator",
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	u, err := c.GetUser(context.Background(), "u-xyz")
	require.NoError(t, err)
	require.Equal(t, "u-xyz", u.UserID)
	require.Equal(t, "Charlie", u.Name)
	require.Equal(t, "administrator", u.OrganizationRole)

	rec := h.seen()
	require.Equal(t, "/users/u-xyz", rec[0].Path)
}

func TestGetUser_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetUser(context.Background(), "")
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen(), "empty id must short-circuit before any network call")
}

func TestGetUser_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetUser(context.Background(), "missing")
	var nf *NotFoundError
	require.ErrorAs(t, err, &nf)
}
