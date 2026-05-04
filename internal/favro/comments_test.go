package favro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListComments_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Comment]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-cm",
			Entities: []Comment{
				{
					CommentID:    "cm-1",
					CardCommonID: "card-c-1",
					UserID:       "u-1",
					Body:         "initial thought",
					Created:      "2026-01-02T03:04:05.000Z",
				},
				{
					CommentID:    "cm-2",
					CardCommonID: "card-c-1",
					UserID:       "u-2",
					Body:         "follow-up",
					Created:      "2026-01-02T04:00:00.000Z",
					LastUpdated:  "2026-01-02T04:30:00.000Z",
				},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListComments(context.Background(), 0, "", "card-c-1")
	require.NoError(t, err)
	require.Equal(t, "req-cm", env.RequestID)
	require.Len(t, env.Entities, 2)
	require.Equal(t, "initial thought", env.Entities[0].Body)
	require.Equal(t, "follow-up", env.Entities[1].Body)
	require.Equal(t, "2026-01-02T04:30:00.000Z", env.Entities[1].LastUpdated)

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, "/comments", rec[0].Path)
	require.Equal(t, "card-c-1", rec[0].Query.Get("cardCommonId"))
	require.Empty(t, rec[0].Query.Get("page"), "page=0 must NOT add ?page=")
}

func TestListComments_EmptyCardCommonID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListComments(context.Background(), 0, "", "")
	require.ErrorIs(t, err, errMissingCardCommonID)
	require.Empty(t, h.seen(), "no HTTP call must be made for empty cardCommonID")
}

func TestListComments_WithPageForwardsRequestIDAndFilter(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Comment]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListComments(context.Background(), 2, "req-prior", "card-c-1")
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "2", rec[0].Query.Get("page"))
	require.Equal(t, "card-c-1", rec[0].Query.Get("cardCommonId"),
		"cardCommonID must be re-sent on every paginated page")
	require.Equal(t, "req-prior", rec[0].Headers.Get(headerRequestID))
}

func TestGetComment_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Comment{
			CommentID:    "cm-zzz",
			CardCommonID: "card-c-1",
			UserID:       "u-1",
			Body:         "looked up",
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	cm, err := c.GetComment(context.Background(), "cm-zzz")
	require.NoError(t, err)
	require.Equal(t, "cm-zzz", cm.CommentID)
	require.Equal(t, "looked up", cm.Body)

	rec := h.seen()
	require.Equal(t, "/comments/cm-zzz", rec[0].Path)
}

func TestGetComment_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetComment(context.Background(), "")
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

func TestGetComment_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetComment(context.Background(), "missing")
	var nf *NotFoundError
	require.ErrorAs(t, err, &nf)
}
