package favro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListTags_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Tag]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-tags",
			Entities: []Tag{
				{TagID: "t-1", Name: "blocker", Color: "red"},
				{TagID: "t-2", Name: "ux", Color: "blue"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListTags(context.Background(), 0, "", ListTagsFilter{})
	require.NoError(t, err)
	require.Equal(t, "req-tags", env.RequestID)
	require.Len(t, env.Entities, 2)
	require.Equal(t, "blocker", env.Entities[0].Name)
	require.Equal(t, "red", env.Entities[0].Color)

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, "/tags", rec[0].Path)
	require.Empty(t, rec[0].Query.Encode(), "no filter or page query expected on first-page list")
}

func TestListTags_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Tag]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListTags(context.Background(), 2, "req-prior", ListTagsFilter{})
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "2", rec[0].Query.Get("page"))
	require.Equal(t, "req-prior", rec[0].Headers.Get(headerRequestID))
}

func TestGetTag_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Tag{
			TagID: "t-zzz",
			Name:  "looked up",
			Color: "lime",
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	tag, err := c.GetTag(context.Background(), "t-zzz")
	require.NoError(t, err)
	require.Equal(t, "t-zzz", tag.TagID)
	require.Equal(t, "looked up", tag.Name)
	require.Equal(t, "lime", tag.Color)

	rec := h.seen()
	require.Equal(t, "/tags/t-zzz", rec[0].Path)
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
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
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
	require.ErrorAs(t, err, &nf)
}

// TestCreateTag_HappyPath pins the POST /tags wire shape: body
// carries name + color; the response is decoded back into a Tag.
func TestCreateTag_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPost, rec.Method)
		require.Equal(t, "/tags", rec.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tagId":"new-tag","organizationId":"fixture-org","name":"frontend","color":"blue"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.CreateTag(context.Background(), CreateTagRequest{Name: "frontend", Color: "blue"})
	require.NoError(t, err)
	require.Equal(t, "new-tag", got.TagID)
	require.Equal(t, "blue", got.Color)

	rec := h.seen()
	require.Len(t, rec, 1)
	require.JSONEq(t, `{"name":"frontend","color":"blue"}`, rec[0].Body)
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

	_, err := c.CreateTag(context.Background(), CreateTagRequest{Name: ""})
	require.Error(t, err)
	require.Empty(t, h.seen(), "empty name must short-circuit before any HTTP call")
}

// TestDeleteTag_HappyPath pins DELETE /tags/{tagId} → 204 success.
func TestDeleteTag_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodDelete, rec.Method)
		require.Equal(t, "/tags/abc123", rec.Path)
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	require.NoError(t, c.DeleteTag(context.Background(), "abc123"))
	require.Len(t, h.seen(), 1)
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

	require.ErrorIs(t, c.DeleteTag(context.Background(), ""), errMissingID)
	require.Empty(t, h.seen())
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
	require.ErrorAs(t, err, &nf)
}

// TestUpdateTag_HappyPath pins PUT /tags/{tagId} → updated Tag.
func TestUpdateTag_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPut, rec.Method)
		require.Equal(t, "/tags/abc123", rec.Path)
		require.JSONEq(t, `{"name":"renamed","color":"red"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tagId":"abc123","name":"renamed","color":"red"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateTag(context.Background(), "abc123", UpdateTagRequest{Name: "renamed", Color: "red"})
	require.NoError(t, err)
	require.Equal(t, "renamed", got.Name)
	require.Equal(t, "red", got.Color)
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

	_, err := c.UpdateTag(context.Background(), "", UpdateTagRequest{Name: "x"})
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

// TestUpdateTag_DryRun_ReturnsRecord pins the dry-run contract for
// the update path.
func TestUpdateTag_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.UpdateTag(WithDryRun(context.Background()), "abc", UpdateTagRequest{Name: "x"})
	require.ErrorIs(t, err, ErrDryRun)
	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodPut, rec.Method)
	require.Contains(t, rec.URL, "/tags/abc")
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
	require.ErrorIs(t, err, ErrDryRun)
	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodDelete, rec.Method)
	require.Contains(t, rec.URL, "/tags/abc")
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

	_, err := c.CreateTag(WithDryRun(context.Background()), CreateTagRequest{Name: "x"})
	require.ErrorIs(t, err, ErrDryRun)
	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodPost, rec.Method)
	require.Contains(t, rec.URL, "/tags")
}

// TestListTags_NameFilterForwarded pins ListTagsFilter.Name → ?name=
// (Favro's documented exact-match server-side filter).
func TestListTags_NameFilterForwarded(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Tag]{Page: 0, Pages: 1})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListTags(context.Background(), 0, "", ListTagsFilter{Name: "blocker"})
	require.NoError(t, err)
	require.Equal(t, "blocker", h.seen()[0].Query.Get("name"))
}
