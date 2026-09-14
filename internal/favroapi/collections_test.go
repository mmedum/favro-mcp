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

func TestListCollections_DefaultPage(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Collection]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-cols",
			Entities: []favro.Collection{
				{CollectionID: "c-1", Name: "Engineering", Color: "blue", PublicSharing: "off"},
				{CollectionID: "c-2", Name: "Marketing", Archived: true},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListCollections(context.Background(), 0, "", favro.ListCollectionsFilter{})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := env.RequestID; got != "req-cols" {
		t.Errorf("env.RequestID = %v, want %v", got, "req-cols")
	}
	if len(env.Entities) != 2 {
		t.Fatalf("len(env.Entities) = %d, want 2", len(env.Entities))
	}
	if got := env.Entities[0].Name; got != "Engineering" {
		t.Errorf("env.Entities[0].Name = %v, want %v", got, "Engineering")
	}
	if got := env.Entities[0].Color; got != "blue" {
		t.Errorf("env.Entities[0].Color = %v, want %v", got, "blue")
	}
	if got := env.Entities[0].PublicSharing; got != "off" {
		t.Errorf("env.Entities[0].PublicSharing = %v, want %v", got, "off")
	}
	if !env.Entities[1].Archived {
		t.Error("env.Entities[1].Archived = false, want true")
	}
	if env.Entities[0].Archived {
		t.Error("missing archived field decodes to false")
	}

	rec := h.seen()
	if len(rec) != 1 {
		t.Fatalf("len(rec) = %d, want 1", len(rec))
	}
	if got := rec[0].Path; got != "/collections" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/collections")
	}
	if len(rec[0].Query.Get("page")) != 0 {
		t.Errorf("page=0 must NOT add ?page= to the request: got %v", rec[0].Query.Get("page"))
	}
}

func TestListCollections_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Collection]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListCollections(context.Background(), 2, "req-prior", favro.ListCollectionsFilter{})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	rec := h.seen()
	if got := rec[0].Query.Get("page"); got != "2" {
		t.Errorf("rec[0].Query.Get(\"page\") = %v, want %v", got, "2")
	}
	if got := rec[0].Headers.Get(headerRequestID); got != "req-prior" {
		t.Errorf("rec[0].Headers.Get(headerRequestID) = %v, want %v", got, "req-prior")
	}
}

func TestGetCollection_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Collection{
			CollectionID: "c-xyz",
			Name:         "Looked Up",
			SharedToUsers: []favro.SharedUser{
				{UserID: "u-1", Role: "fullMember"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	col, err := c.GetCollection(context.Background(), "c-xyz")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := col.CollectionID; got != "c-xyz" {
		t.Errorf("col.CollectionID = %v, want %v", got, "c-xyz")
	}
	if got := col.Name; got != "Looked Up" {
		t.Errorf("col.Name = %v, want %v", got, "Looked Up")
	}
	if len(col.SharedToUsers) != 1 {
		t.Fatalf("len(col.SharedToUsers) = %d, want 1", len(col.SharedToUsers))
	}

	rec := h.seen()
	if got := rec[0].Path; got != "/collections/c-xyz" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/collections/c-xyz")
	}
}

func TestGetCollection_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetCollection(context.Background(), "")
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// TestListCollections_ArchivedAndNewFields pins favro.ListCollectionsFilter.Archived
// → ?archived=true and that the new favro.Collection response fields
// (organizationId, background) decode without dropping data.
func TestListCollections_ArchivedAndNewFields(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"requestId":"r","entities":[{
			"collectionId":"c-1","name":"Engineering",
			"organizationId":"org-1",
			"background":"forest"
		}]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListCollections(context.Background(), 0, "", favro.ListCollectionsFilter{Archived: true})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := h.seen()[0].Query.Get("archived"); got != "true" {
		t.Errorf("h.seen()[0].Query.Get(\"archived\") = %v, want %v", got, "true")
	}
	if len(env.Entities) != 1 {
		t.Fatalf("len(env.Entities) = %d, want 1", len(env.Entities))
	}
	if got := env.Entities[0].OrganizationID; got != "org-1" {
		t.Errorf("env.Entities[0].OrganizationID = %v, want %v", got, "org-1")
	}
	if got := env.Entities[0].Background; got != "forest" {
		t.Errorf("env.Entities[0].Background = %v, want %v", got, "forest")
	}
}

func TestGetCollection_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetCollection(context.Background(), "missing")
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want nf", err)
	}
}

// TestCreateCollection_HappyPath pins POST /collections — name +
// optional sharing + color, response decoded back as favro.Collection.
func TestCreateCollection_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPost {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
		}
		if got := rec.Path; got != "/collections" {
			t.Errorf("rec.Path = %v, want %v", got, "/collections")
		}
		requireJSONEq(t, `{"name":"Eng","color":"blue","publicSharing":"organization"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collectionId":"c-new","name":"Eng","color":"blue","publicSharing":"organization"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.CreateCollection(context.Background(), favro.CreateCollectionRequest{
		Name:          "Eng",
		Color:         "blue",
		PublicSharing: "organization",
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.CollectionID; got != "c-new" {
		t.Errorf("got.CollectionID = %v, want %v", got, "c-new")
	}
}

// TestCreateCollection_EmptyName_NoNetworkCall pins the empty-name
// short-circuit.
func TestCreateCollection_EmptyName_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.CreateCollection(context.Background(), favro.CreateCollectionRequest{Name: ""})
	if err == nil {
		t.Fatal("err should have failed")
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// TestCreateCollection_DryRun_ReturnsRecord pins the dry-run contract.
func TestCreateCollection_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.CreateCollection(WithDryRun(context.Background()), favro.CreateCollectionRequest{Name: "x"})
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}
	var rec *DryRunRecord
	if !errors.As(err, &rec) {
		t.Fatalf("got %v, want rec", err)
	}
	if got := rec.Method; got != http.MethodPost {
		t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
	}
}

func TestUpdateCollection_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPut {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPut)
		}
		if got := rec.Path; got != "/collections/c-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/collections/c-1")
		}
		requireJSONEq(t, `{"name":"renamed"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collectionId":"c-1","name":"renamed"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateCollection(context.Background(), "c-1", favro.UpdateCollectionRequest{Name: "renamed"})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.Name; got != "renamed" {
		t.Errorf("got.Name = %v, want %v", got, "renamed")
	}
}

func TestUpdateCollection_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateCollection(context.Background(), "", favro.UpdateCollectionRequest{Name: "x"})
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

func TestDeleteCollection_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodDelete {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodDelete)
		}
		if got := rec.Path; got != "/collections/c-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/collections/c-1")
		}
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if err := c.DeleteCollection(context.Background(), "c-1"); err != nil {
		t.Fatalf("c.DeleteCollection(context.Background(), \"c-1\"): %v", err)
	}
}

func TestDeleteCollection_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if !errors.Is(c.DeleteCollection(context.Background(), ""), errMissingID) {
		t.Fatalf("got %v, want errMissingID", c.DeleteCollection(context.Background(), ""))
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

func TestDeleteCollection_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	err := c.DeleteCollection(WithDryRun(context.Background()), "c-1")
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}
	var rec *DryRunRecord
	if !errors.As(err, &rec) {
		t.Fatalf("got %v, want rec", err)
	}
	if got := rec.Method; got != http.MethodDelete {
		t.Errorf("rec.Method = %v, want %v", got, http.MethodDelete)
	}
}
