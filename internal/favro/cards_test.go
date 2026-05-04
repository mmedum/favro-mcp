package favro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListCards_NoFilters(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Card]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-cards",
			Entities: []Card{
				{
					CardID:             "card-i-1",
					CardCommonID:       "card-c-1",
					Name:               "Print visitor passes",
					SequentialID:       42,
					SequentialIDPrefix: "VP",
					Position:           3,
					Tags:               []string{"tag-1"},
					Assignments: []CardAssignment{
						{UserID: "user-1", Completed: false},
					},
				},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListCards(context.Background(), 0, "", ListCardsFilter{})
	require.NoError(t, err)
	require.Equal(t, "req-cards", env.RequestID)
	require.Len(t, env.Entities, 1)
	require.Equal(t, "Print visitor passes", env.Entities[0].Name)
	require.Equal(t, 42, env.Entities[0].SequentialID)
	require.Equal(t, "VP", env.Entities[0].SequentialIDPrefix)
	require.Equal(t, []string{"tag-1"}, env.Entities[0].Tags)
	require.Equal(t, "user-1", env.Entities[0].Assignments[0].UserID)

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, "/cards", rec[0].Path)
	require.Empty(t, rec[0].Query.Get("page"), "page=0 must NOT add ?page=")
	require.Empty(t, rec[0].Query.Encode(), "empty filter must produce empty query")
}

func TestListCards_AllFiltersForwarded(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Card]{Page: 0, Pages: 1})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListCards(context.Background(), 0, "", ListCardsFilter{
		WidgetCommonID: "w-1",
		CollectionID:   "col-1",
		CardCommonID:   "card-c-7",
		SequentialID:   123,
		Unique:         true,
	})
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "w-1", rec[0].Query.Get("widgetCommonId"))
	require.Equal(t, "col-1", rec[0].Query.Get("collectionId"))
	require.Equal(t, "card-c-7", rec[0].Query.Get("cardCommonId"))
	require.Equal(t, "123", rec[0].Query.Get("cardSequentialId"))
	require.Equal(t, "true", rec[0].Query.Get("unique"))
}

func TestListCards_SequentialIDZero_OmitsParam(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Card]{Page: 0, Pages: 1})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListCards(context.Background(), 0, "", ListCardsFilter{
		WidgetCommonID: "w-1",
		SequentialID:   0,
	})
	require.NoError(t, err)

	rec := h.seen()
	require.Empty(t, rec[0].Query.Get("cardSequentialId"),
		"SequentialID=0 must be treated as 'no filter' (Favro has no card with sequentialId 0)")
}

func TestListCards_WithPageForwardsRequestIDAndFilter(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Card]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListCards(context.Background(), 2, "req-prior", ListCardsFilter{
		WidgetCommonID: "w-1",
	})
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "2", rec[0].Query.Get("page"))
	require.Equal(t, "w-1", rec[0].Query.Get("widgetCommonId"),
		"filter must ride along on every paginated page (Favro doesn't carry filter state)")
	require.Equal(t, "req-prior", rec[0].Headers.Get(headerRequestID))
}

func TestGetCard_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Card{
			CardID:       "card-i-zzz",
			CardCommonID: "card-c-zzz",
			Name:         "Looked Up",
			SequentialID: 99,
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	// GetCard takes the per-widget cardId, not the cardCommonId
	// (verified live: Favro 403s if you pass a cardCommonId here).
	card, err := c.GetCard(context.Background(), "card-i-zzz")
	require.NoError(t, err)
	require.Equal(t, "card-c-zzz", card.CardCommonID)
	require.Equal(t, "Looked Up", card.Name)

	rec := h.seen()
	require.Equal(t, "/cards/card-i-zzz", rec[0].Path,
		"GET /cards/{id} must use the per-widget cardId in the path")
}

func TestGetCard_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetCard(context.Background(), "")
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

func TestGetCard_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetCard(context.Background(), "missing")
	var nf *NotFoundError
	require.ErrorAs(t, err, &nf)
}
