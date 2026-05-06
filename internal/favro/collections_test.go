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

	env, err := c.ListCollections(context.Background(), 0, "", ListCollectionsFilter{})
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

	_, err := c.ListCollections(context.Background(), 2, "req-prior", ListCollectionsFilter{})
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

// TestListCollections_ArchivedAndNewFields pins ListCollectionsFilter.Archived
// → ?archived=true and that the new Collection response fields
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

	env, err := c.ListCollections(context.Background(), 0, "", ListCollectionsFilter{Archived: true})
	require.NoError(t, err)
	require.Equal(t, "true", h.seen()[0].Query.Get("archived"))
	require.Len(t, env.Entities, 1)
	require.Equal(t, "org-1", env.Entities[0].OrganizationID)
	require.Equal(t, "forest", env.Entities[0].Background)
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

// TestCreateCollection_HappyPath pins POST /collections — name +
// optional sharing + color, response decoded back as Collection.
func TestCreateCollection_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPost, rec.Method)
		require.Equal(t, "/collections", rec.Path)
		require.JSONEq(t, `{"name":"Eng","color":"blue","publicSharing":"organization"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collectionId":"c-new","name":"Eng","color":"blue","publicSharing":"organization"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.CreateCollection(context.Background(), CreateCollectionRequest{
		Name:          "Eng",
		Color:         "blue",
		PublicSharing: "organization",
	})
	require.NoError(t, err)
	require.Equal(t, "c-new", got.CollectionID)
}

// TestCreateCollection_EmptyName_NoNetworkCall pins the empty-name
// short-circuit.
func TestCreateCollection_EmptyName_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.CreateCollection(context.Background(), CreateCollectionRequest{Name: ""})
	require.Error(t, err)
	require.Empty(t, h.seen())
}

// TestCreateCollection_DryRun_ReturnsRecord pins the dry-run contract.
func TestCreateCollection_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.CreateCollection(WithDryRun(context.Background()), CreateCollectionRequest{Name: "x"})
	require.ErrorIs(t, err, ErrDryRun)
	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodPost, rec.Method)
}

func TestUpdateCollection_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPut, rec.Method)
		require.Equal(t, "/collections/c-1", rec.Path)
		require.JSONEq(t, `{"name":"renamed"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collectionId":"c-1","name":"renamed"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateCollection(context.Background(), "c-1", UpdateCollectionRequest{Name: "renamed"})
	require.NoError(t, err)
	require.Equal(t, "renamed", got.Name)
}

func TestUpdateCollection_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateCollection(context.Background(), "", UpdateCollectionRequest{Name: "x"})
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

func TestDeleteCollection_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodDelete, rec.Method)
		require.Equal(t, "/collections/c-1", rec.Path)
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	require.NoError(t, c.DeleteCollection(context.Background(), "c-1"))
}

func TestDeleteCollection_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	require.ErrorIs(t, c.DeleteCollection(context.Background(), ""), errMissingID)
	require.Empty(t, h.seen())
}

func TestDeleteCollection_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	err := c.DeleteCollection(WithDryRun(context.Background()), "c-1")
	require.ErrorIs(t, err, ErrDryRun)
	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodDelete, rec.Method)
}
