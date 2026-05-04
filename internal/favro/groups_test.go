package favro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListGroups_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Group]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-g",
			Entities: []Group{
				{
					GroupID: "g-1",
					Name:    "Engineers",
					Members: []GroupMember{
						{UserID: "u-1", Role: "administrator"},
						{UserID: "u-2", Role: "member"},
					},
				},
				{
					GroupID: "g-2",
					Name:    "Read-only",
					// Members may be absent on some groups; omitempty drops it.
				},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListGroups(context.Background(), 0, "")
	require.NoError(t, err)
	require.Equal(t, "req-g", env.RequestID)
	require.Len(t, env.Entities, 2)
	require.Equal(t, "Engineers", env.Entities[0].Name)
	require.Len(t, env.Entities[0].Members, 2)
	require.Equal(t, "administrator", env.Entities[0].Members[0].Role)
	require.Empty(t, env.Entities[1].Members,
		"groups without members must NOT carry the field (omitempty drops it)")

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, "/groups", rec[0].Path)
	require.Empty(t, rec[0].Query.Encode(), "no filter or page query expected on first-page list")
}

func TestListGroups_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Group]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListGroups(context.Background(), 2, "req-prior")
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "2", rec[0].Query.Get("page"))
	require.Equal(t, "req-prior", rec[0].Headers.Get(headerRequestID))
}

func TestGetGroup_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Group{
			GroupID: "g-zzz",
			Name:    "looked up",
			Members: []GroupMember{
				{UserID: "u-only", Role: "member"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	g, err := c.GetGroup(context.Background(), "g-zzz")
	require.NoError(t, err)
	require.Equal(t, "g-zzz", g.GroupID)
	require.Equal(t, "looked up", g.Name)
	require.Len(t, g.Members, 1)
	require.Equal(t, "u-only", g.Members[0].UserID)

	rec := h.seen()
	require.Equal(t, "/groups/g-zzz", rec[0].Path)
}

func TestGetGroup_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetGroup(context.Background(), "")
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

func TestGetGroup_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetGroup(context.Background(), "missing")
	var nf *NotFoundError
	require.ErrorAs(t, err, &nf)
}
