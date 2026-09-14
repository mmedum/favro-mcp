package favroapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestListCards_NoFilters(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Card]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-cards",
			Entities: []favro.Card{
				{
					CardID:             "card-i-1",
					CardCommonID:       "card-c-1",
					Name:               "Print visitor passes",
					SequentialID:       42,
					SequentialIDPrefix: "VP",
					Position:           3,
					Tags:               []string{"tag-1"},
					Assignments: []favro.CardAssignment{
						{UserID: "user-1", Completed: false},
					},
				},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListCards(context.Background(), 0, "", favro.ListCardsFilter{})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := env.RequestID; got != "req-cards" {
		t.Errorf("env.RequestID = %v, want %v", got, "req-cards")
	}
	if len(env.Entities) != 1 {
		t.Fatalf("len(env.Entities) = %d, want 1", len(env.Entities))
	}
	if got := env.Entities[0].Name; got != "Print visitor passes" {
		t.Errorf("env.Entities[0].Name = %v, want %v", got, "Print visitor passes")
	}
	if got := env.Entities[0].SequentialID; got != 42 {
		t.Errorf("env.Entities[0].SequentialID = %v, want %v", got, 42)
	}
	if got := env.Entities[0].SequentialIDPrefix; got != "VP" {
		t.Errorf("env.Entities[0].SequentialIDPrefix = %v, want %v", got, "VP")
	}
	if got := env.Entities[0].Tags; !reflect.DeepEqual(got, ([]string{"tag-1"})) {
		t.Errorf("env.Entities[0].Tags = %v, want %v", got, []string{"tag-1"})
	}
	if got := env.Entities[0].Assignments[0].UserID; got != "user-1" {
		t.Errorf("env.Entities[0].Assignments[0].UserID = %v, want %v", got, "user-1")
	}

	rec := h.seen()
	if len(rec) != 1 {
		t.Fatalf("len(rec) = %d, want 1", len(rec))
	}
	if got := rec[0].Path; got != "/cards" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/cards")
	}
	if len(rec[0].Query.Get("page")) != 0 {
		t.Errorf("page=0 must NOT add ?page=: got %v", rec[0].Query.Get("page"))
	}
	if len(rec[0].Query.Encode()) != 0 {
		t.Errorf("empty filter must produce empty query: got %v", rec[0].Query.Encode())
	}
}

// TestListCards_FractionalPosition pins the contract that
// favro.Card.Position / ListPosition decode from Favro's fractional
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

	env, err := c.ListCards(context.Background(), 0, "", favro.ListCardsFilter{})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(env.Entities) != 1 {
		t.Fatalf("len(env.Entities) = %d, want 1", len(env.Entities))
	}
	if math.Abs(env.Entities[0].Position-3.125) > 0.0001 {
		t.Errorf("env.Entities[0].Position = %v, want %v within 0.0001", env.Entities[0].Position, 3.125)
	}
	if math.Abs(env.Entities[0].ListPosition-7.5) > 0.0001 {
		t.Errorf("env.Entities[0].ListPosition = %v, want %v within 0.0001", env.Entities[0].ListPosition, 7.5)
	}
}

// TestListCards_CustomFieldsValuesDecoding pins the JSON contract
// for favro.Card.CustomFieldsValues. Value is json.RawMessage so each
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

	env, err := c.ListCards(context.Background(), 0, "", favro.ListCardsFilter{})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(env.Entities) != 1 {
		t.Fatalf("len(env.Entities) = %d, want 1", len(env.Entities))
	}
	cfvs := env.Entities[0].CustomFieldsValues
	if len(cfvs) != 13 {
		t.Fatalf("len(cfvs) = %d, want 13", len(cfvs))
	}

	// Lookup helper — order is preserved but tests don't depend on it.
	byID := map[string]favro.CardCustomFieldValue{}
	for _, v := range cfvs {
		byID[v.CustomFieldID] = v
	}

	requireJSONEq(t, `"hello"`, string(byID["cf-text"].Value))
	requireJSONEq(t, `42`, string(byID["cf-num"].Value))
	requireJSONEq(t, `true`, string(byID["cf-bool"].Value))
	requireJSONEq(t, `"2026-05-04T00:00:00Z"`, string(byID["cf-date"].Value))
	if got := byID["cf-select"].CustomFieldItemIDs; !reflect.DeepEqual(got, ([]string{"item-1"})) {
		t.Errorf("byID[\"cf-select\"].CustomFieldItemIDs = %v, want %v", got, []string{"item-1"})
	}
	if got := byID["cf-multi"].CustomFieldItemIDs; !reflect.DeepEqual(got, ([]string{"item-1", "item-2"})) {
		t.Errorf("byID[\"cf-multi\"].CustomFieldItemIDs = %v, want %v", got, []string{"item-1", "item-2"})
	}

	// Legacy shape: Rating split across value + total.
	if byID["cf-rating"].Total == nil {
		t.Fatal("byID[\"cf-rating\"].Total is nil")
	}
	if math.Abs(*byID["cf-rating"].Total-7.0) > 0.0001 {
		t.Errorf("*byID[\"cf-rating\"].Total = %v, want %v within 0.0001", *byID["cf-rating"].Total, 7.0)
	}

	// Documented shapes.
	requireJSONEq(t, `["st-1"]`, string(byID["cf-status"].Value))
	if byID["cf-number-total"].Total == nil {
		t.Fatal("byID[\"cf-number-total\"].Total is nil")
	}
	if math.Abs(*byID["cf-number-total"].Total-8.0) > 0.0001 {
		t.Errorf("*byID[\"cf-number-total\"].Total = %v, want %v within 0.0001", *byID["cf-number-total"].Total, 8.0)
	}
	requireJSONEq(t, `{"startDate":"2026-01-01","dueDate":"2026-02-01","showTime":true}`,
		string(byID["cf-timeline"].Timeline))
	requireJSONEq(t, `{"url":"https://example.com","text":"docs"}`, string(byID["cf-link"].Link))
	if got := byID["cf-color"].Color; got != "blue-300" {
		t.Errorf("byID[\"cf-color\"].Color = %v, want %v", got, "blue-300")
	}
	if byID["cf-time"].Total == nil {
		t.Fatal("byID[\"cf-time\"].Total is nil")
	}
	if math.Abs(*byID["cf-time"].Total-50400000.0) > 0.0001 {
		t.Errorf("*byID[\"cf-time\"].Total = %v, want %v within 0.0001", *byID["cf-time"].Total, 50400000.0)
	}
	requireJSONEq(t, `{"u-1":{"reportId":"r-1","value":50400000}}`, string(byID["cf-time"].Reports))

	// Total stays nil (not 0) when Favro omits it, so an explicit
	// zero rating remains distinguishable from an unset field.
	if byID["cf-text"].Total != nil {
		t.Errorf("byID[\"cf-text\"].Total = %v, want nil", byID["cf-text"].Total)
	}
}

func TestListCards_AllFiltersForwarded(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Card]{Page: 0, Pages: 1})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListCards(context.Background(), 0, "", favro.ListCardsFilter{
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	rec := h.seen()
	if got := rec[0].Query.Get("widgetCommonId"); got != "w-1" {
		t.Errorf("rec[0].Query.Get(\"widgetCommonId\") = %v, want %v", got, "w-1")
	}
	if got := rec[0].Query.Get("collectionId"); got != "col-1" {
		t.Errorf("rec[0].Query.Get(\"collectionId\") = %v, want %v", got, "col-1")
	}
	if got := rec[0].Query.Get("cardCommonId"); got != "card-c-7" {
		t.Errorf("rec[0].Query.Get(\"cardCommonId\") = %v, want %v", got, "card-c-7")
	}
	if got := rec[0].Query.Get("cardSequentialId"); got != "123" {
		t.Errorf("rec[0].Query.Get(\"cardSequentialId\") = %v, want %v", got, "123")
	}
	if got := rec[0].Query.Get("columnId"); got != "col-x" {
		t.Errorf("rec[0].Query.Get(\"columnId\") = %v, want %v", got, "col-x")
	}
	if got := rec[0].Query.Get("todoList"); got != "true" {
		t.Errorf("rec[0].Query.Get(\"todoList\") = %v, want %v", got, "true")
	}
	if got := rec[0].Query.Get("archived"); got != "true" {
		t.Errorf("rec[0].Query.Get(\"archived\") = %v, want %v", got, "true")
	}
	if got := rec[0].Query.Get("unique"); got != "true" {
		t.Errorf("rec[0].Query.Get(\"unique\") = %v, want %v", got, "true")
	}
	if got := rec[0].Query.Get("descriptionFormat"); got != "markdown" {
		t.Errorf("rec[0].Query.Get(\"descriptionFormat\") = %v, want %v", got, "markdown")
	}
}

// TestListCards_NewResponseFieldsDecoding pins decode for the
// extended favro.Card response surface: isLane, tasksTotal, tasksDone,
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

	env, err := c.ListCards(context.Background(), 0, "", favro.ListCardsFilter{})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(env.Entities) != 1 {
		t.Fatalf("len(env.Entities) = %d, want 1", len(env.Entities))
	}

	got := env.Entities[0]
	if !got.IsLane {
		t.Error("got.IsLane = false, want true")
	}
	if got := got.TasksTotal; got != 7 {
		t.Errorf("got.TasksTotal = %v, want %v", got, 7)
	}
	if got := got.TasksDone; got != 3 {
		t.Errorf("got.TasksDone = %v, want %v", got, 3)
	}
	if got := got.CreatedByUserID; got != "u-1" {
		t.Errorf("got.CreatedByUserID = %v, want %v", got, "u-1")
	}
	if got := got.CreatedAt; got != "2026-05-04T12:00:00Z" {
		t.Errorf("got.CreatedAt = %v, want %v", got, "2026-05-04T12:00:00Z")
	}
	if len(got.Attachments) != 1 {
		t.Fatalf("len(got.Attachments) = %d, want 1", len(got.Attachments))
	}
	if got := got.Attachments[0].Name; got != "spec.pdf" {
		t.Errorf("got.Attachments[0].Name = %v, want %v", got, "spec.pdf")
	}
	if len(got.FavroAttachments) != 1 {
		t.Fatalf("len(got.FavroAttachments) = %d, want 1", len(got.FavroAttachments))
	}
	if got := got.FavroAttachments[0].Type; got != "card" {
		t.Errorf("got.FavroAttachments[0].Type = %v, want %v", got, "card")
	}
	if got := got.FavroAttachments[0].ItemCommonID; got != "cc-other" {
		t.Errorf("got.FavroAttachments[0].ItemCommonID = %v, want %v", got, "cc-other")
	}
	if got := got.TimeOnBoard.Time; got != int64(3600000) {
		t.Errorf("got.TimeOnBoard.Time = %v, want %v", got, int64(3600000))
	}
	if len(got.TimeOnColumns) != 2 {
		t.Fatalf("len(got.TimeOnColumns) = %d, want 2", len(got.TimeOnColumns))
	}
	if got := got.TimeOnColumns["col-doing"]; got != int64(1800000) {
		t.Errorf("got.TimeOnColumns[\"col-doing\"] = %v, want %v", got, int64(1800000))
	}
	if got := got.TimeOnColumns["col-done"]; got != int64(1800000) {
		t.Errorf("got.TimeOnColumns[\"col-done\"] = %v, want %v", got, int64(1800000))
	}
}

func TestListCards_SequentialIDZero_OmitsParam(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Card]{Page: 0, Pages: 1})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListCards(context.Background(), 0, "", favro.ListCardsFilter{
		WidgetCommonID: "w-1",
		SequentialID:   0,
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	rec := h.seen()
	if len(rec[0].Query.Get("cardSequentialId")) != 0 {
		t.Errorf("SequentialID=0 must be treated as 'no filter' (Favro has no card with sequentialId 0): got %v", rec[0].Query.Get("cardSequentialId"))
	}
}

func TestListCards_WithPageForwardsRequestIDAndFilter(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Card]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListCards(context.Background(), 2, "req-prior", favro.ListCardsFilter{
		WidgetCommonID: "w-1",
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	rec := h.seen()
	if got := rec[0].Query.Get("page"); got != "2" {
		t.Errorf("rec[0].Query.Get(\"page\") = %v, want %v", got, "2")
	}
	if got := rec[0].Query.Get("widgetCommonId"); got != "w-1" {
		t.Errorf("filter must ride along on every paginated page (Favro doesn't carry filter state): got %v, want %v", got, "w-1")
	}
	if got := rec[0].Headers.Get(headerRequestID); got != "req-prior" {
		t.Errorf("rec[0].Headers.Get(headerRequestID) = %v, want %v", got, "req-prior")
	}
}

func TestGetCard_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Card{
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := card.CardCommonID; got != "card-c-zzz" {
		t.Errorf("card.CardCommonID = %v, want %v", got, "card-c-zzz")
	}
	if got := card.Name; got != "Looked Up" {
		t.Errorf("card.Name = %v, want %v", got, "Looked Up")
	}

	rec := h.seen()
	if got := rec[0].Path; got != "/cards/card-i-zzz" {
		t.Errorf("GET /cards/{id} must use the per-widget cardId in the path: got %v, want %v", got, "/cards/card-i-zzz")
	}
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
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
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
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want nf", err)
	}
}

// TestCreateCard_HappyPath pins POST /cards: name + widget +
// optional knobs in the body, decoded favro.Card response back.
func TestCreateCard_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPost {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
		}
		if got := rec.Path; got != "/cards" {
			t.Errorf("rec.Path = %v, want %v", got, "/cards")
		}
		requireJSONEq(t, `{
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

	got, err := c.CreateCard(context.Background(), favro.CreateCardRequest{
		Name:           "new card",
		WidgetCommonID: "w-1",
		ColumnID:       "col-1",
		TagIDs:         []string{"t-1", "t-2"},
		AssignmentIDs:  []string{"u-1"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.CardID; got != "ci-new" {
		t.Errorf("got.CardID = %v, want %v", got, "ci-new")
	}
	if got := got.CardCommonID; got != "cc-new" {
		t.Errorf("got.CardCommonID = %v, want %v", got, "cc-new")
	}
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

	_, err := c.CreateCard(context.Background(), favro.CreateCardRequest{Name: ""})
	if err == nil {
		t.Fatal("err should have failed")
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// TestCreateCard_DryRun_ReturnsRecord pins the dry-run contract for
// the create path: returns *DryRunRecord wrapped in ErrDryRun and
// the network is never touched.
func TestCreateCard_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.CreateCard(WithDryRun(context.Background()), favro.CreateCardRequest{Name: "preview"})
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}
	var rec *DryRunRecord
	if !errors.As(err, &rec) {
		t.Fatalf("got %v, want rec", err)
	}
	if got := rec.Method; got != http.MethodPost {
		t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
	}
	if !strings.Contains(rec.URL, "/cards") {
		t.Errorf("rec.URL does not contain %q", "/cards")
	}
}

// TestUpdateCard_HappyPath pins PUT /cards/{cardId}: body carries
// the updateable fields, response decodes into favro.Card.
func TestUpdateCard_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPut {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPut)
		}
		if got := rec.Path; got != "/cards/ci-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/cards/ci-1")
		}
		requireJSONEq(t, `{"name":"renamed","columnId":"col-2","addTagIds":["t-3"]}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","cardCommonId":"cc-1","name":"renamed","columnId":"col-2"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateCard(context.Background(), "ci-1", favro.UpdateCardRequest{
		Name:      "renamed",
		ColumnID:  "col-2",
		AddTagIDs: []string{"t-3"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.Name; got != "renamed" {
		t.Errorf("got.Name = %v, want %v", got, "renamed")
	}
	if got := got.ColumnID; got != "col-2" {
		t.Errorf("got.ColumnID = %v, want %v", got, "col-2")
	}
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

	_, err := c.UpdateCard(context.Background(), "", favro.UpdateCardRequest{Name: "x"})
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// TestUpdateCard_DryRun_ReturnsRecord pins the dry-run contract.
func TestUpdateCard_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.UpdateCard(WithDryRun(context.Background()), "ci-1", favro.UpdateCardRequest{Name: "x"})
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}
	var rec *DryRunRecord
	if !errors.As(err, &rec) {
		t.Fatalf("got %v, want rec", err)
	}
	if got := rec.Method; got != http.MethodPut {
		t.Errorf("rec.Method = %v, want %v", got, http.MethodPut)
	}
	if !strings.Contains(rec.URL, "/cards/ci-1") {
		t.Errorf("rec.URL does not contain %q", "/cards/ci-1")
	}
}

// TestArchiveCard_SendsArchiveTrue pins that ArchiveCard's body is
// `{"archive":true}` — and not omitted by an unfortunate
// pointer-elision pass.
func TestArchiveCard_SendsArchiveTrue(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		requireJSONEq(t, `{"archive":true}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","archived":true}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.ArchiveCard(context.Background(), "ci-1")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !got.IsArchived {
		t.Error("got.IsArchived = false, want true")
	}
}

// TestUnarchiveCard_SendsArchiveFalse pins that UnarchiveCard sends
// the explicit false (Archive is *bool so omitempty doesn't strip
// &false; if the field were a plain bool the body would be empty).
func TestUnarchiveCard_SendsArchiveFalse(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		requireJSONEq(t, `{"archive":false}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","archived":false}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UnarchiveCard(context.Background(), "ci-1")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.IsArchived {
		t.Error("got.IsArchived = true, want false")
	}
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
		requireJSONEq(t, `{"columnId":"col-2","listPosition":0}`, rec.Body,
			"listPosition must marshal as a JSON number, NOT a string")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","columnId":"col-2","listPosition":0}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateCard(context.Background(), "ci-1", favro.UpdateCardRequest{
		ColumnID:     "col-2",
		ListPosition: &pos,
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
}

// TestMoveCard_HappyPath pins that MoveCard sends only the move
// fields (not name / archive / etc), and PUTs to /cards/{id}.
func TestMoveCard_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Path; got != "/cards/ci-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/cards/ci-1")
		}
		requireJSONEq(t, `{"widgetCommonId":"w-2","columnId":"col-3"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","widgetCommonId":"w-2","columnId":"col-3"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.MoveCard(context.Background(), "ci-1", favro.MoveCardRequest{
		WidgetCommonID: "w-2",
		ColumnID:       "col-3",
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.WidgetCommonID; got != "w-2" {
		t.Errorf("got.WidgetCommonID = %v, want %v", got, "w-2")
	}
}

// TestMoveCard_EmptyMove_NoNetworkCall pins that a fully-empty
// favro.MoveCardRequest short-circuits — silently PUT-ing a no-op would
// mask a caller bug.
func TestMoveCard_EmptyMove_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {
		// Should never be called.
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.MoveCard(context.Background(), "ci-1", favro.MoveCardRequest{})
	if err == nil {
		t.Fatal("err should have failed")
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// TestDeleteCard_HappyPath_DefaultEverywhere pins the default-case
// (everywhere=false) wire shape: DELETE /cards/{id} with no
// `everywhere=` param, response decodes into favro.DeleteCardResponse —
// verified live Phase 5.3, Favro returns a BARE JSON array, NOT
// the object form the docs hint at.
func TestDeleteCard_HappyPath_DefaultEverywhere(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodDelete {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodDelete)
		}
		if got := rec.Path; got != "/cards/ci-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/cards/ci-1")
		}
		if len(rec.Query.Get("everywhere")) != 0 {
			t.Errorf("everywhere=false must omit the param entirely so older Favro behavior is preserved: got %v", rec.Query.Get("everywhere"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["ci-1"]`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.DeleteCard(context.Background(), "ci-1", false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got; !reflect.DeepEqual(got, (favro.DeleteCardResponse{"ci-1"})) {
		t.Errorf("got = %v, want %v", got, favro.DeleteCardResponse{"ci-1"})
	}
}

// TestDeleteCard_Everywhere_QueryForwarded pins everywhere=true →
// ?everywhere=true on the URL. Mis-encoding would have Favro silently
// fall back to per-widget delete and surprise the caller.
func TestDeleteCard_Everywhere_QueryForwarded(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Query.Get("everywhere"); got != "true" {
			t.Errorf("rec.Query.Get(\"everywhere\") = %v, want %v", got, "true")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["ci-1","ci-2","ci-3"]`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.DeleteCard(context.Background(), "ci-1", true)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
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
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
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
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want nf", err)
	}
}

// TestDeleteCard_DryRun_ReturnsRecord pins the dry-run contract for
// delete: never hits the network.
func TestDeleteCard_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.DeleteCard(WithDryRun(context.Background()), "ci-1", true)
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}
	var rec *DryRunRecord
	if !errors.As(err, &rec) {
		t.Fatalf("got %v, want rec", err)
	}
	if got := rec.Method; got != http.MethodDelete {
		t.Errorf("rec.Method = %v, want %v", got, http.MethodDelete)
	}
	if !strings.Contains(rec.URL, "/cards/ci-1") {
		t.Errorf("rec.URL does not contain %q", "/cards/ci-1")
	}
	if !strings.Contains(rec.URL, "everywhere=true") {
		t.Errorf("rec.URL does not contain %q", "everywhere=true")
	}
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

			var card favro.Card
			if err := json.Unmarshal([]byte(tc.payload), &card); err != nil {
				t.Fatalf("json.Unmarshal([]byte(tc.payload), &card): %v", err)
			}
			got := card.CustomFields()
			if len(got) != 1 {
				t.Fatalf("len(got) = %d, want 1", len(got))
			}
			if got := got[0].CustomFieldID; got != tc.wantID {
				t.Errorf("got[0].CustomFieldID = %v, want %v", got, tc.wantID)
			}
		})
	}
}

func TestCard_CustomFields_EmptyWhenNeitherKeyPresent(t *testing.T) {
	t.Parallel()

	var card favro.Card
	if err := json.Unmarshal([]byte(`{"cardId":"c1"}`), &card); err != nil {
		t.Fatalf("json.Unmarshal([]byte(`{\"cardId\":\"c1\"}`), &card): %v", err)
	}
	if len(card.CustomFields()) != 0 {
		t.Errorf("card.CustomFields() = %v, want empty", card.CustomFields())
	}
}
