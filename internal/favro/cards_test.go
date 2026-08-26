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
// for Card.CustomFieldsValues. Value is json.RawMessage so each
// Favro type survives decode without committing the projection to a
// single Go type. The fixture deliberately mixes both wire shapes:
// the documented one (Number/Rating in `total`, select item ids in
// `value`, `timeline` / `link` sibling objects) and the legacy one
// this client assumed before the contract was re-checked against the
// docs (`customFieldItemIds`, Rating split across value+total), so
// the tolerant decode covers whichever Favro actually emits.
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
				{"customFieldId":"cf-rating","value":3,"total":7},
				{"customFieldId":"cf-status","value":["st-1"]},
				{"customFieldId":"cf-number-total","total":8},
				{"customFieldId":"cf-timeline","timeline":{"startDate":"2026-01-01","dueDate":"2026-02-01","showTime":true}},
				{"customFieldId":"cf-link","link":{"url":"https://example.com","text":"docs"}},
				{"customFieldId":"cf-color","color":"blue-300"},
				{"customFieldId":"cf-time","total":50400000,"reports":{"u-1":{"reportId":"r-1","value":50400000}}}
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
	require.Len(t, cfvs, 13)

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

	// Legacy shape: Rating split across value + total.
	require.NotNil(t, byID["cf-rating"].Total)
	require.InDelta(t, 7.0, *byID["cf-rating"].Total, 0.0001)

	// Documented shapes.
	require.JSONEq(t, `["st-1"]`, string(byID["cf-status"].Value))
	require.NotNil(t, byID["cf-number-total"].Total)
	require.InDelta(t, 8.0, *byID["cf-number-total"].Total, 0.0001)
	require.JSONEq(t, `{"startDate":"2026-01-01","dueDate":"2026-02-01","showTime":true}`,
		string(byID["cf-timeline"].Timeline))
	require.JSONEq(t, `{"url":"https://example.com","text":"docs"}`, string(byID["cf-link"].Link))
	require.Equal(t, "blue-300", byID["cf-color"].Color)
	require.NotNil(t, byID["cf-time"].Total)
	require.InDelta(t, 50400000.0, *byID["cf-time"].Total, 0.0001)
	require.JSONEq(t, `{"u-1":{"reportId":"r-1","value":50400000}}`, string(byID["cf-time"].Reports))

	// Total stays nil (not 0) when Favro omits it, so an explicit
	// zero rating remains distinguishable from an unset field.
	require.Nil(t, byID["cf-text"].Total)
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

// TestCreateCard_HappyPath pins POST /cards: name + widget +
// optional knobs in the body, decoded Card response back.
func TestCreateCard_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPost, rec.Method)
		require.Equal(t, "/cards", rec.Path)
		require.JSONEq(t, `{
			"name":"new card",
			"widgetCommonId":"w-1",
			"columnId":"col-1",
			"tagIds":["t-1","t-2"],
			"assignmentIds":["u-1"]
		}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-new","cardCommonId":"cc-new","name":"new card","widgetCommonId":"w-1","columnId":"col-1"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.CreateCard(context.Background(), CreateCardRequest{
		Name:           "new card",
		WidgetCommonID: "w-1",
		ColumnID:       "col-1",
		TagIDs:         []string{"t-1", "t-2"},
		AssignmentIDs:  []string{"u-1"},
	})
	require.NoError(t, err)
	require.Equal(t, "ci-new", got.CardID)
	require.Equal(t, "cc-new", got.CardCommonID)
}

// TestCreateCard_EmptyName_NoNetworkCall pins that an empty name
// short-circuits before any HTTP call.
func TestCreateCard_EmptyName_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {
		// Should never be called.
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.CreateCard(context.Background(), CreateCardRequest{Name: ""})
	require.Error(t, err)
	require.Empty(t, h.seen())
}

// TestCreateCard_DryRun_ReturnsRecord pins the dry-run contract for
// the create path: returns *DryRunRecord wrapped in ErrDryRun and
// the network is never touched.
func TestCreateCard_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.CreateCard(WithDryRun(context.Background()), CreateCardRequest{Name: "preview"})
	require.ErrorIs(t, err, ErrDryRun)
	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodPost, rec.Method)
	require.Contains(t, rec.URL, "/cards")
}

// TestUpdateCard_HappyPath pins PUT /cards/{cardId}: body carries
// the updateable fields, response decodes into Card.
func TestUpdateCard_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPut, rec.Method)
		require.Equal(t, "/cards/ci-1", rec.Path)
		require.JSONEq(t, `{"name":"renamed","columnId":"col-2","addTagIds":["t-3"]}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","cardCommonId":"cc-1","name":"renamed","columnId":"col-2"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateCard(context.Background(), "ci-1", UpdateCardRequest{
		Name:      "renamed",
		ColumnID:  "col-2",
		AddTagIDs: []string{"t-3"},
	})
	require.NoError(t, err)
	require.Equal(t, "renamed", got.Name)
	require.Equal(t, "col-2", got.ColumnID)
}

// TestUpdateCard_EmptyID_NoNetworkCall pins that an empty cardID
// short-circuits before any HTTP call.
func TestUpdateCard_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {
		// Should never be called.
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateCard(context.Background(), "", UpdateCardRequest{Name: "x"})
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

// TestUpdateCard_DryRun_ReturnsRecord pins the dry-run contract.
func TestUpdateCard_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.UpdateCard(WithDryRun(context.Background()), "ci-1", UpdateCardRequest{Name: "x"})
	require.ErrorIs(t, err, ErrDryRun)
	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodPut, rec.Method)
	require.Contains(t, rec.URL, "/cards/ci-1")
}

// TestArchiveCard_SendsArchiveTrue pins that ArchiveCard's body is
// `{"archive":true}` — and not omitted by an unfortunate
// pointer-elision pass.
func TestArchiveCard_SendsArchiveTrue(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.JSONEq(t, `{"archive":true}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","archived":true}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.ArchiveCard(context.Background(), "ci-1")
	require.NoError(t, err)
	require.True(t, got.IsArchived)
}

// TestUnarchiveCard_SendsArchiveFalse pins that UnarchiveCard sends
// the explicit false (Archive is *bool so omitempty doesn't strip
// &false; if the field were a plain bool the body would be empty).
func TestUnarchiveCard_SendsArchiveFalse(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.JSONEq(t, `{"archive":false}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","archived":false}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UnarchiveCard(context.Background(), "ci-1")
	require.NoError(t, err)
	require.False(t, got.IsArchived)
}

// TestUpdateCard_ListPositionIsJSONNumber pins the wire-contract
// gotcha caught live in Phase 5.3: Favro rejects string-valued
// listPosition with HTTP 400 ("Unexpected value of listPosition").
// The field must marshal as a JSON number; pointer typing also
// preserves an explicit 0 (top-of-column) which omitempty would
// strip if the field were a plain float64.
func TestUpdateCard_ListPositionIsJSONNumber(t *testing.T) {
	t.Parallel()

	pos := 0.0
	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.JSONEq(t, `{"columnId":"col-2","listPosition":0}`, rec.Body,
			"listPosition must marshal as a JSON number, NOT a string")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","columnId":"col-2","listPosition":0}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateCard(context.Background(), "ci-1", UpdateCardRequest{
		ColumnID:     "col-2",
		ListPosition: &pos,
	})
	require.NoError(t, err)
}

// TestMoveCard_HappyPath pins that MoveCard sends only the move
// fields (not name / archive / etc), and PUTs to /cards/{id}.
func TestMoveCard_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, "/cards/ci-1", rec.Path)
		require.JSONEq(t, `{"widgetCommonId":"w-2","columnId":"col-3"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","widgetCommonId":"w-2","columnId":"col-3"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.MoveCard(context.Background(), "ci-1", MoveCardRequest{
		WidgetCommonID: "w-2",
		ColumnID:       "col-3",
	})
	require.NoError(t, err)
	require.Equal(t, "w-2", got.WidgetCommonID)
}

// TestMoveCard_EmptyMove_NoNetworkCall pins that a fully-empty
// MoveCardRequest short-circuits — silently PUT-ing a no-op would
// mask a caller bug.
func TestMoveCard_EmptyMove_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {
		// Should never be called.
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.MoveCard(context.Background(), "ci-1", MoveCardRequest{})
	require.Error(t, err)
	require.Empty(t, h.seen())
}

// TestDeleteCard_HappyPath_DefaultEverywhere pins the default-case
// (everywhere=false) wire shape: DELETE /cards/{id} with no
// `everywhere=` param, response decodes into DeleteCardResponse —
// verified live Phase 5.3, Favro returns a BARE JSON array, NOT
// the object form the docs hint at.
func TestDeleteCard_HappyPath_DefaultEverywhere(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodDelete, rec.Method)
		require.Equal(t, "/cards/ci-1", rec.Path)
		require.Empty(t, rec.Query.Get("everywhere"),
			"everywhere=false must omit the param entirely so older Favro behavior is preserved")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["ci-1"]`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.DeleteCard(context.Background(), "ci-1", false)
	require.NoError(t, err)
	require.Equal(t, DeleteCardResponse{"ci-1"}, got)
}

// TestDeleteCard_Everywhere_QueryForwarded pins everywhere=true →
// ?everywhere=true on the URL. Mis-encoding would have Favro silently
// fall back to per-widget delete and surprise the caller.
func TestDeleteCard_Everywhere_QueryForwarded(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, "true", rec.Query.Get("everywhere"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["ci-1","ci-2","ci-3"]`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.DeleteCard(context.Background(), "ci-1", true)
	require.NoError(t, err)
	require.Len(t, got, 3)
}

// TestDeleteCard_EmptyID_NoNetworkCall pins that an empty cardID
// short-circuits before any HTTP call.
func TestDeleteCard_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {
		// Should never be called.
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.DeleteCard(context.Background(), "", false)
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

// TestDeleteCard_NotFound surfaces a 404 from Favro as *NotFoundError.
func TestDeleteCard_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.DeleteCard(context.Background(), "missing", false)
	var nf *NotFoundError
	require.ErrorAs(t, err, &nf)
}

// TestDeleteCard_DryRun_ReturnsRecord pins the dry-run contract for
// delete: never hits the network.
func TestDeleteCard_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.DeleteCard(WithDryRun(context.Background()), "ci-1", true)
	require.ErrorIs(t, err, ErrDryRun)
	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodDelete, rec.Method)
	require.Contains(t, rec.URL, "/cards/ci-1")
	require.Contains(t, rec.URL, "everywhere=true")
}

// TestCard_CustomFields_ReconcilesBothWireKeys pins the accessor that
// bridges the two keys Favro's docs and payloads disagree on: the
// docs describe `customFields`, this client has always decoded
// `customFieldsValues`, and no captured payload settles which one a
// live tenant sends.
func TestCard_CustomFields_ReconcilesBothWireKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload string
		wantID  string
	}{
		{
			name:    "legacy key only",
			payload: `{"cardId":"c1","customFieldsValues":[{"customFieldId":"cf-legacy"}]}`,
			wantID:  "cf-legacy",
		},
		{
			name:    "documented key only",
			payload: `{"cardId":"c1","customFields":[{"customFieldId":"cf-doc"}]}`,
			wantID:  "cf-doc",
		},
		{
			name: "both present prefers the documented key",
			payload: `{"cardId":"c1",
				"customFieldsValues":[{"customFieldId":"cf-legacy"}],
				"customFields":[{"customFieldId":"cf-doc"}]}`,
			wantID: "cf-doc",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var card Card
			require.NoError(t, json.Unmarshal([]byte(tc.payload), &card))
			got := card.CustomFields()
			require.Len(t, got, 1)
			require.Equal(t, tc.wantID, got[0].CustomFieldID)
		})
	}
}

func TestCard_CustomFields_EmptyWhenNeitherKeyPresent(t *testing.T) {
	t.Parallel()

	var card Card
	require.NoError(t, json.Unmarshal([]byte(`{"cardId":"c1"}`), &card))
	require.Empty(t, card.CustomFields())
}
