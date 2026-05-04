package favro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListCollections_DefaultPage(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Collection]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-cols",
			Entities: []Collection{
				{CollectionID: "c-1", Name: "Engineering", Color: "blue", PublicSharing: "off"},
				{CollectionID: "c-2", Name: "Marketing", Archived: true},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListCollections(context.Background(), 0, "")
	require.NoError(t, err)
	require.Equal(t, "req-cols", env.RequestID)
	require.Len(t, env.Entities, 2)
	require.Equal(t, "Engineering", env.Entities[0].Name)
	require.Equal(t, "blue", env.Entities[0].Color)
	require.Equal(t, "off", env.Entities[0].PublicSharing)
	require.True(t, env.Entities[1].Archived)
	require.False(t, env.Entities[0].Archived, "missing archived field decodes to false")

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, "/collections", rec[0].Path)
	require.Empty(t, rec[0].Query.Get("page"), "page=0 must NOT add ?page= to the request")
}

func TestListCollections_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Collection]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListCollections(context.Background(), 2, "req-prior")
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "2", rec[0].Query.Get("page"))
	require.Equal(t, "req-prior", rec[0].Headers.Get(headerRequestID))
}

func TestGetCollection_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Collection{
			CollectionID: "c-xyz",
			Name:         "Looked Up",
			SharedToUsers: []SharedUser{
				{UserID: "u-1", Role: "fullMember"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	col, err := c.GetCollection(context.Background(), "c-xyz")
	require.NoError(t, err)
	require.Equal(t, "c-xyz", col.CollectionID)
	require.Equal(t, "Looked Up", col.Name)
	require.Len(t, col.SharedToUsers, 1)

	rec := h.seen()
	require.Equal(t, "/collections/c-xyz", rec[0].Path)
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
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
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
	require.ErrorAs(t, err, &nf)
}
