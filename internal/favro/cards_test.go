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

// TestListCards_FractionalPosition pins the contract that
// Card.Position / ListPosition decode from Favro's fractional
// values (e.g. 3.125 — Favro slots cards between siblings via
// arbitrary subdivision rather than re-numbering). int decoding
// would 400 the JSON unmarshal on any non-integer position.
func TestListCards_FractionalPosition(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"requestId":"r","entities":[
			{"cardId":"c1","cardCommonId":"cc1","name":"x","position":3.125,"listPosition":7.5}
		]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListCards(context.Background(), 0, "", ListCardsFilter{})
	require.NoError(t, err)
	require.Len(t, env.Entities, 1)
	require.InDelta(t, 3.125, env.Entities[0].Position, 0.0001)
	require.InDelta(t, 7.5, env.Entities[0].ListPosition, 0.0001)
}

// TestListCards_CustomFieldsValuesDecoding pins the JSON contract
// for Card.CustomFieldsValues across the field shapes Phase 4.4
// dereferences. Value is json.RawMessage so each Favro type (text,
// number, date, checkbox, link) survives decode without committing
// the projection to a single Go type; CustomFieldItemIDs carries
// the option references for select-flavored fields.
func TestListCards_CustomFieldsValuesDecoding(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"requestId":"r","entities":[{
			"cardId":"c1","cardCommonId":"cc1","name":"x",
			"customFieldsValues":[
				{"customFieldId":"cf-text","value":"hello"},
				{"customFieldId":"cf-num","value":42},
				{"customFieldId":"cf-bool","value":true},
				{"customFieldId":"cf-date","value":"2026-05-04T00:00:00Z"},
				{"customFieldId":"cf-select","customFieldItemIds":["item-1"]},
				{"customFieldId":"cf-multi","customFieldItemIds":["item-1","item-2"]},
				{"customFieldId":"cf-rating","value":3,"total":7}
			]
		}]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListCards(context.Background(), 0, "", ListCardsFilter{})
	require.NoError(t, err)
	require.Len(t, env.Entities, 1)
	cfvs := env.Entities[0].CustomFieldsValues
	require.Len(t, cfvs, 7)

	// Lookup helper — order is preserved but tests don't depend on it.
	byID := map[string]CardCustomFieldValue{}
	for _, v := range cfvs {
		byID[v.CustomFieldID] = v
	}

	require.JSONEq(t, `"hello"`, string(byID["cf-text"].Value))
	require.JSONEq(t, `42`, string(byID["cf-num"].Value))
	require.JSONEq(t, `true`, string(byID["cf-bool"].Value))
	require.JSONEq(t, `"2026-05-04T00:00:00Z"`, string(byID["cf-date"].Value))
	require.Equal(t, []string{"item-1"}, byID["cf-select"].CustomFieldItemIDs)
	require.Equal(t, []string{"item-1", "item-2"}, byID["cf-multi"].CustomFieldItemIDs)
	require.InDelta(t, 7.0, byID["cf-rating"].Total, 0.0001)
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
		WidgetCommonID:    "w-1",
		CollectionID:      "col-1",
		CardCommonID:      "card-c-7",
		SequentialID:      123,
		ColumnID:          "col-x",
		TodoList:          true,
		Archived:          true,
		Unique:            true,
		DescriptionFormat: "markdown",
	})
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "w-1", rec[0].Query.Get("widgetCommonId"))
	require.Equal(t, "col-1", rec[0].Query.Get("collectionId"))
	require.Equal(t, "card-c-7", rec[0].Query.Get("cardCommonId"))
	require.Equal(t, "123", rec[0].Query.Get("cardSequentialId"))
	require.Equal(t, "col-x", rec[0].Query.Get("columnId"))
	require.Equal(t, "true", rec[0].Query.Get("todoList"))
	require.Equal(t, "true", rec[0].Query.Get("archived"))
	require.Equal(t, "true", rec[0].Query.Get("unique"))
	require.Equal(t, "markdown", rec[0].Query.Get("descriptionFormat"))
}

// TestListCards_NewResponseFieldsDecoding pins decode for the
// extended Card response surface: isLane, tasksTotal, tasksDone,
// createdByUserId, createdAt, attachments, favroAttachments,
// timeOnBoard, timeOnColumns.
func TestListCards_NewResponseFieldsDecoding(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"requestId":"r","entities":[{
			"cardId":"c1","cardCommonId":"cc1","name":"x",
			"isLane":true,
			"tasksTotal":7,
			"tasksDone":3,
			"createdByUserId":"u-1",
			"createdAt":"2026-05-04T12:00:00Z",
			"attachments":[
				{"name":"spec.pdf","fileURL":"https://favro.invalid/a/spec.pdf"}
			],
			"favroAttachments":[
				{"type":"card","itemCommonId":"cc-other"}
			],
			"timeOnBoard":{"time":3600000,"isStopped":false},
			"timeOnColumns":{
				"col-doing":1800000,
				"col-done":1800000
			}
		}]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListCards(context.Background(), 0, "", ListCardsFilter{})
	require.NoError(t, err)
	require.Len(t, env.Entities, 1)

	got := env.Entities[0]
	require.True(t, got.IsLane)
	require.Equal(t, 7, got.TasksTotal)
	require.Equal(t, 3, got.TasksDone)
	require.Equal(t, "u-1", got.CreatedByUserID)
	require.Equal(t, "2026-05-04T12:00:00Z", got.CreatedAt)
	require.Len(t, got.Attachments, 1)
	require.Equal(t, "spec.pdf", got.Attachments[0].Name)
	require.Len(t, got.FavroAttachments, 1)
	require.Equal(t, "card", got.FavroAttachments[0].Type)
	require.Equal(t, "cc-other", got.FavroAttachments[0].ItemCommonID)
	require.Equal(t, int64(3600000), got.TimeOnBoard.Time)
	require.Len(t, got.TimeOnColumns, 2)
	require.Equal(t, int64(1800000), got.TimeOnColumns["col-doing"])
	require.Equal(t, int64(1800000), got.TimeOnColumns["col-done"])
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
