package favroapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestListTags_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-tags",
			Entities: []favro.Tag{
				{TagID: "t-1", Name: "blocker", Color: "red"},
				{TagID: "t-2", Name: "ux", Color: "blue"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListTags(context.Background(), 0, "", favro.ListTagsFilter{})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := env.RequestID; got != "req-tags" {
		t.Errorf("env.RequestID = %v, want %v", got, "req-tags")
	}
	if len(env.Entities) != 2 {
		t.Fatalf("len(env.Entities) = %d, want 2", len(env.Entities))
	}
	if got := env.Entities[0].Name; got != "blocker" {
		t.Errorf("env.Entities[0].Name = %v, want %v", got, "blocker")
	}
	if got := env.Entities[0].Color; got != "red" {
		t.Errorf("env.Entities[0].Color = %v, want %v", got, "red")
	}

	rec := h.seen()
	if len(rec) != 1 {
		t.Fatalf("len(rec) = %d, want 1", len(rec))
	}
	if got := rec[0].Path; got != "/tags" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/tags")
	}
	if len(rec[0].Query.Encode()) != 0 {
		t.Errorf("no filter or page query expected on first-page list: got %v", rec[0].Query.Encode())
	}
}

func TestListTags_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListTags(context.Background(), 2, "req-prior", favro.ListTagsFilter{})
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

func TestGetTag_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Tag{
			TagID: "t-zzz",
			Name:  "looked up",
			Color: "lime",
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	tag, err := c.GetTag(context.Background(), "t-zzz")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := tag.TagID; got != "t-zzz" {
		t.Errorf("tag.TagID = %v, want %v", got, "t-zzz")
	}
	if got := tag.Name; got != "looked up" {
		t.Errorf("tag.Name = %v, want %v", got, "looked up")
	}
	if got := tag.Color; got != "lime" {
		t.Errorf("tag.Color = %v, want %v", got, "lime")
	}

	rec := h.seen()
	if got := rec[0].Path; got != "/tags/t-zzz" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/tags/t-zzz")
	}
}

func TestGetTag_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetTag(context.Background(), "")
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

func TestGetTag_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetTag(context.Background(), "missing")
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want nf", err)
	}
}

// TestCreateTag_HappyPath pins the POST /tags wire shape: body
// carries name + color; the response is decoded back into a favro.Tag.
func TestCreateTag_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPost {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
		}
		if got := rec.Path; got != "/tags" {
			t.Errorf("rec.Path = %v, want %v", got, "/tags")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tagId":"new-tag","organizationId":"fixture-org","name":"frontend","color":"blue"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.CreateTag(context.Background(), favro.CreateTagRequest{Name: "frontend", Color: "blue"})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.TagID; got != "new-tag" {
		t.Errorf("got.TagID = %v, want %v", got, "new-tag")
	}
	if got := got.Color; got != "blue" {
		t.Errorf("got.Color = %v, want %v", got, "blue")
	}

	rec := h.seen()
	if len(rec) != 1 {
		t.Fatalf("len(rec) = %d, want 1", len(rec))
	}
	requireJSONEq(t, `{"name":"frontend","color":"blue"}`, rec[0].Body)
}

// TestCreateTag_EmptyName_NoNetworkCall pins that an empty name
// short-circuits before any HTTP call — Favro returns a 400 on
// missing name, but surfacing it locally saves a round-trip.
func TestCreateTag_EmptyName_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {
		// Should never be called.
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.CreateTag(context.Background(), favro.CreateTagRequest{Name: ""})
	if err == nil {
		t.Fatal("err should have failed")
	}
	if len(h.seen()) != 0 {
		t.Errorf("empty name must short-circuit before any HTTP call: got %v", h.seen())
	}
}

// TestDeleteTag_HappyPath pins DELETE /tags/{tagId} → 204 success.
func TestDeleteTag_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodDelete {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodDelete)
		}
		if got := rec.Path; got != "/tags/abc123" {
			t.Errorf("rec.Path = %v, want %v", got, "/tags/abc123")
		}
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if err := c.DeleteTag(context.Background(), "abc123"); err != nil {
		t.Fatalf("c.DeleteTag(context.Background(), \"abc123\"): %v", err)
	}
	if len(h.seen()) != 1 {
		t.Fatalf("len(h.seen()) = %d, want 1", len(h.seen()))
	}
}

// TestDeleteTag_EmptyID_NoNetworkCall pins that an empty tagId
// short-circuits before any HTTP call.
func TestDeleteTag_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {
		// Should never be called.
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if !errors.Is(c.DeleteTag(context.Background(), ""), errMissingID) {
		t.Fatalf("got %v, want errMissingID", c.DeleteTag(context.Background(), ""))
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// TestDeleteTag_NotFound surfaces a 404 from Favro as *NotFoundError.
func TestDeleteTag_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	err := c.DeleteTag(context.Background(), "missing")
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want nf", err)
	}
}

// TestUpdateTag_HappyPath pins PUT /tags/{tagId} → updated favro.Tag.
func TestUpdateTag_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPut {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPut)
		}
		if got := rec.Path; got != "/tags/abc123" {
			t.Errorf("rec.Path = %v, want %v", got, "/tags/abc123")
		}
		requireJSONEq(t, `{"name":"renamed","color":"red"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tagId":"abc123","name":"renamed","color":"red"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateTag(context.Background(), "abc123", favro.UpdateTagRequest{Name: "renamed", Color: "red"})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.Name; got != "renamed" {
		t.Errorf("got.Name = %v, want %v", got, "renamed")
	}
	if got := got.Color; got != "red" {
		t.Errorf("got.Color = %v, want %v", got, "red")
	}
}

// TestUpdateTag_EmptyID_NoNetworkCall pins that an empty tagID
// short-circuits.
func TestUpdateTag_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {
		// Should never be called.
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateTag(context.Background(), "", favro.UpdateTagRequest{Name: "x"})
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// TestUpdateTag_DryRun_ReturnsRecord pins the dry-run contract for
// the update path.
func TestUpdateTag_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.UpdateTag(WithDryRun(context.Background()), "abc", favro.UpdateTagRequest{Name: "x"})
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}
	var rec *DryRunRecord
	if !errors.As(err, &rec) {
		t.Fatalf("got %v, want rec", err)
	}
	if got := rec.Method; got != http.MethodPut {
		t.Errorf("rec.Method = %v, want %v", got, http.MethodPut)
	}
	if !strings.Contains(rec.URL, "/tags/abc") {
		t.Errorf("rec.URL does not contain %q", "/tags/abc")
	}
}

// TestDeleteTag_DryRun_ReturnsRecord pins the dry-run contract for
// the delete path: returns *DryRunRecord wrapped in ErrDryRun and
// the network is never touched.
func TestDeleteTag_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	err := c.DeleteTag(WithDryRun(context.Background()), "abc")
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
	if !strings.Contains(rec.URL, "/tags/abc") {
		t.Errorf("rec.URL does not contain %q", "/tags/abc")
	}
}

// TestCreateTag_DryRun_ReturnsRecord pins the dry-run contract for
// the new write helper at the resource layer: in dry-run mode the
// returned error wraps a *DryRunRecord and the network is never
// touched.
func TestCreateTag_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.CreateTag(WithDryRun(context.Background()), favro.CreateTagRequest{Name: "x"})
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
	if !strings.Contains(rec.URL, "/tags") {
		t.Errorf("rec.URL does not contain %q", "/tags")
	}
}

// TestUpdateTags_HappyPath_FanOut pins the client-side fan-out wire
// shape: one PUT /tags/{tagId} per input entry, results returned in
// input order. Favro has no real bulk endpoint (PUT /tags returns
// the SPA fallback HTML), so the bulk surface is a client-side
// fan-out over the per-tag PUT.
func TestUpdateTags_HappyPath_FanOut(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPut {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPut)
		}
		// Each entry hits its per-tag URL. Echo the body back as
		// the response so the per-index ordering is verifiable.
		w.Header().Set("Content-Type", "application/json")
		switch rec.Path {
		case "/tags/t-1":
			requireJSONEq(t, `{"name":"renamed-a","color":"red"}`, rec.Body)
			_, _ = w.Write([]byte(`{"tagId":"t-1","name":"renamed-a","color":"red"}`))
		case "/tags/t-2":
			requireJSONEq(t, `{"color":"blue"}`, rec.Body)
			_, _ = w.Write([]byte(`{"tagId":"t-2","name":"existing","color":"blue"}`))
		default:
			t.Errorf("unexpected per-tag path: %s", rec.Path)
		}
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateTags(context.Background(), []favro.BulkTagUpdate{
		{TagID: "t-1", Name: "renamed-a", Color: "red"},
		{TagID: "t-2", Color: "blue"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got := got[0].TagID; got != "t-1" {
		t.Errorf("results must be returned in input order: got %v, want %v", got, "t-1")
	}
	if got := got[0].Name; got != "renamed-a" {
		t.Errorf("got[0].Name = %v, want %v", got, "renamed-a")
	}
	if got := got[0].Color; got != "red" {
		t.Errorf("got[0].Color = %v, want %v", got, "red")
	}
	if got := got[1].TagID; got != "t-2" {
		t.Errorf("got[1].TagID = %v, want %v", got, "t-2")
	}
	if got := got[1].Color; got != "blue" {
		t.Errorf("got[1].Color = %v, want %v", got, "blue")
	}
	if len(h.seen()) != 2 {
		t.Fatalf("fan-out must dispatch one request per input entry: got %d", len(h.seen()))
	}
}

// TestUpdateTags_PerEntryError pins that a single per-entry failure
// surfaces a wrapped error naming both the offending tagId and its
// input index. The first error wins (errgroup default); other
// in-flight calls cancel.
func TestUpdateTags_PerEntryError(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		switch rec.Path {
		case "/tags/t-good":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tagId":"t-good","name":"renamed"}`))
		case "/tags/t-missing":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected per-tag path: %s", rec.Path)
		}
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateTags(context.Background(), []favro.BulkTagUpdate{
		{TagID: "t-good", Name: "renamed"},
		{TagID: "t-missing", Name: "wont-land"},
	})
	if err == nil {
		t.Fatal("err should have failed")
	}
	if !strings.Contains(err.Error(), "t-missing") {
		t.Errorf("err.Error() does not contain %q", "t-missing")
	}
	if !strings.Contains(err.Error(), "index 1") {
		t.Errorf("err.Error() does not contain %q", "index 1")
	}
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want nf", err)
	}
}

// TestUpdateTags_EmptyUpdates_NoNetworkCall pins that an empty
// updates slice short-circuits before any HTTP call — Favro 400s
// on an empty array, but surfacing it locally saves a round-trip.
func TestUpdateTags_EmptyUpdates_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {
		// Should never be called.
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateTags(context.Background(), nil)
	if err == nil {
		t.Fatal("err should have failed")
	}
	if len(h.seen()) != 0 {
		t.Errorf("empty updates must short-circuit before any HTTP call: got %v", h.seen())
	}
}

// TestUpdateTags_MissingTagID_NoNetworkCall pins that a bulk entry
// missing tagId short-circuits before any HTTP call. The error
// message names the offending index so the LLM can fix the right
// entry rather than guessing.
func TestUpdateTags_MissingTagID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {
		// Should never be called.
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateTags(context.Background(), []favro.BulkTagUpdate{
		{TagID: "t-1", Name: "ok"},
		{Name: "no-id"},
	})
	if err == nil {
		t.Fatal("err should have failed")
	}
	if !strings.Contains(err.Error(), "index 1") {
		t.Errorf("err.Error() does not contain %q", "index 1")
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// TestUpdateTags_DryRun_ReturnsRecord pins the dry-run contract for
// the bulk update path: returns a single synthesized *DryRunRecord
// wrapped in ErrDryRun, no HTTP work. Body carries the input array
// (informational — there is no literal bulk-request payload since
// Favro has no bulk endpoint); URL describes the per-tag fan-out
// so the LLM sees the real wire pattern.
func TestUpdateTags_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.UpdateTags(WithDryRun(context.Background()), []favro.BulkTagUpdate{
		{TagID: "t-1", Name: "renamed"},
		{TagID: "t-2", Color: "blue"},
	})
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}
	var rec *DryRunRecord
	if !errors.As(err, &rec) {
		t.Fatalf("got %v, want rec", err)
	}
	if got := rec.Method; got != http.MethodPut {
		t.Errorf("rec.Method = %v, want %v", got, http.MethodPut)
	}
	if !strings.Contains(rec.URL, "/tags/{tagId}") {
		t.Errorf("rec.URL does not contain %q", "/tags/{tagId}")
	}
	if !strings.Contains(rec.URL, "× 2") {
		t.Errorf("rec.URL does not contain %q", "× 2")
	}
	if !strings.Contains(rec.URL, "fan-out") {
		t.Errorf("rec.URL does not contain %q", "fan-out")
	}
	requireJSONEq(t,
		`[{"tagId":"t-1","name":"renamed"},{"tagId":"t-2","color":"blue"}]`,
		string(rec.Body))
}

// TestListTags_NameFilterForwarded pins favro.ListTagsFilter.Name → ?name=
// (Favro's documented exact-match server-side filter).
func TestListTags_NameFilterForwarded(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{Page: 0, Pages: 1})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListTags(context.Background(), 0, "", favro.ListTagsFilter{Name: "blocker"})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := h.seen()[0].Query.Get("name"); got != "blocker" {
		t.Errorf("h.seen()[0].Query.Get(\"name\") = %v, want %v", got, "blocker")
	}
}
