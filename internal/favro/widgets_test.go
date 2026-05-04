package favro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListWidgets_DefaultPage(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Widget]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-w",
			Entities: []Widget{
				{WidgetCommonID: "w-1", Name: "Engineering Backlog", Type: "backlog", Color: "blue"},
				{WidgetCommonID: "w-2", Name: "Sprint Board", Type: "board", CollectionIDs: []string{"c-1", "c-2"}},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListWidgets(context.Background(), 0, "", "")
	require.NoError(t, err)
	require.Equal(t, "req-w", env.RequestID)
	require.Len(t, env.Entities, 2)
	require.Equal(t, "Engineering Backlog", env.Entities[0].Name)
	require.Equal(t, "backlog", env.Entities[0].Type)
	require.ElementsMatch(t, []string{"c-1", "c-2"}, env.Entities[1].CollectionIDs)

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, "/widgets", rec[0].Path)
	require.Empty(t, rec[0].Query.Get("page"), "page=0 must NOT add ?page=")
	require.Empty(t, rec[0].Query.Get("collection"), "empty collectionID must NOT add ?collection=")
}

func TestListWidgets_FiltersByCollection(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Widget]{Page: 0, Pages: 1})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListWidgets(context.Background(), 0, "", "c-xyz")
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "c-xyz", rec[0].Query.Get("collection"),
		"non-empty collectionID must be sent as ?collection=")
}

func TestListWidgets_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Widget]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListWidgets(context.Background(), 2, "req-prior", "")
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "2", rec[0].Query.Get("page"))
	require.Equal(t, "req-prior", rec[0].Headers.Get(headerRequestID))
}

func TestGetWidget_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Widget{
			WidgetCommonID: "w-zzz",
			Name:           "Looked Up",
			Type:           "board",
			CollectionIDs:  []string{"c-only"},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	w, err := c.GetWidget(context.Background(), "w-zzz")
	require.NoError(t, err)
	require.Equal(t, "w-zzz", w.WidgetCommonID)
	require.Equal(t, "board", w.Type)

	rec := h.seen()
	require.Equal(t, "/widgets/w-zzz", rec[0].Path)
}

func TestGetWidget_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetWidget(context.Background(), "")
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

func TestGetWidget_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetWidget(context.Background(), "missing")
	var nf *NotFoundError
	require.ErrorAs(t, err, &nf)
}
