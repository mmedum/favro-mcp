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

func TestListUsers_DefaultPage(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.User]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-users",
			Entities: []favro.User{
				{UserID: "u-1", Name: "Alice", Email: "alice@example.com", OrganizationRole: "fullMember"},
				{UserID: "u-2", Name: "Bob", OrganizationRole: "guest"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListUsers(context.Background(), 0, "")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := env.RequestID; got != "req-users" {
		t.Errorf("env.RequestID = %v, want %v", got, "req-users")
	}
	if len(env.Entities) != 2 {
		t.Fatalf("len(env.Entities) = %d, want 2", len(env.Entities))
	}
	if got := env.Entities[0].Name; got != "Alice" {
		t.Errorf("env.Entities[0].Name = %v, want %v", got, "Alice")
	}
	if got := env.Entities[0].Email; got != "alice@example.com" {
		t.Errorf("env.Entities[0].Email = %v, want %v", got, "alice@example.com")
	}
	if got := env.Entities[0].OrganizationRole; got != "fullMember" {
		t.Errorf("env.Entities[0].OrganizationRole = %v, want %v", got, "fullMember")
	}
	if len(env.Entities[1].Email) != 0 {
		t.Errorf("users without email decode cleanly to empty string: got %v", env.Entities[1].Email)
	}

	rec := h.seen()
	if len(rec) != 1 {
		t.Fatalf("len(rec) = %d, want 1", len(rec))
	}
	if got := rec[0].Path; got != "/users" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/users")
	}
	if len(rec[0].Query.Get("page")) != 0 {
		t.Errorf("page=0 must NOT add ?page= to the request: got %v", rec[0].Query.Get("page"))
	}
}

func TestListUsers_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.User]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListUsers(context.Background(), 2, "req-prior")
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
	if !errors.As(err, &ae) {
		t.Fatalf("got %v, want ae", err)
	}
}

func TestGetUser_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.User{
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := u.UserID; got != "u-xyz" {
		t.Errorf("u.UserID = %v, want %v", got, "u-xyz")
	}
	if got := u.Name; got != "Charlie" {
		t.Errorf("u.Name = %v, want %v", got, "Charlie")
	}
	if got := u.OrganizationRole; got != "administrator" {
		t.Errorf("u.OrganizationRole = %v, want %v", got, "administrator")
	}

	rec := h.seen()
	if got := rec[0].Path; got != "/users/u-xyz" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/users/u-xyz")
	}
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
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("empty id must short-circuit before any network call: got %v", h.seen())
	}
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
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want nf", err)
	}
}
