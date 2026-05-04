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

	env, err := c.ListTags(context.Background(), 0, "")
	require.NoError(t, err)
	require.Equal(t, "req-tags", env.RequestID)
	require.Len(t, env.Entities, 2)
	require.Equal(t, "blocker", env.Entities[0].Name)
	require.Equal(t, "red", env.Entities[0].Color)

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, "/tags", rec[0].Path)
	require.Empty(t, rec[0].Query.Get("page"), "page=0 must NOT add ?page=")
	require.Empty(t, rec[0].Query.Encode(), "tags is org-global; no filter query expected")
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

	_, err := c.ListTags(context.Background(), 2, "req-prior")
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
