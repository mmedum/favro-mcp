package tools

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListCards_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Card]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-cards",
			Entities: []favro.Card{
				{CardCommonID: "card-c-1", Name: "Print visitor passes", SequentialID: 42},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listCardsToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[listOutput[favro.Card]](t, res)
	if len(out.Items) != 1 {
		t.Fatalf("len(out.Items) = %d, want 1", len(out.Items))
	}
	if got := out.Items[0].Name; got != "Print visitor passes" {
		t.Errorf("out.Items[0].Name = %v, want %v", got, "Print visitor passes")
	}
	if got := out.Items[0].SequentialID; got != 42 {
		t.Errorf("out.Items[0].SequentialID = %v, want %v", got, 42)
	}
	if out.NextPage == nil {
		t.Fatal("out.NextPage is nil")
	}
	if got := *out.NextPage; got != 2 {
		t.Errorf("*out.NextPage = %v, want %v", got, 2)
	}
}

func TestMCP_ListCards_FiltersForwarded(t *testing.T) {
	t.Parallel()

	var saw struct {
		widget, collection, common, seq, unique string
	}
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw.widget = r.URL.Query().Get("widgetCommonId")
		saw.collection = r.URL.Query().Get("collectionId")
		saw.common = r.URL.Query().Get("cardCommonId")
		saw.seq = r.URL.Query().Get("cardSequentialId")
		saw.unique = r.URL.Query().Get("unique")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Card]{Page: 0, Pages: 1})
	}))

	cs := connectInMemoryWith(t, c)
	_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: listCardsToolName,
		Arguments: map[string]any{
			"widget_common_id": "w-1",
			"collection_id":    "col-1",
			"card_common_id":   "card-c-7",
			"sequential_id":    123,
			"unique":           true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := saw.widget; got != "w-1" {
		t.Errorf("saw.widget = %v, want %v", got, "w-1")
	}
	if got := saw.collection; got != "col-1" {
		t.Errorf("saw.collection = %v, want %v", got, "col-1")
	}
	if got := saw.common; got != "card-c-7" {
		t.Errorf("saw.common = %v, want %v", got, "card-c-7")
	}
	if got := saw.seq; got != "123" {
		t.Errorf("saw.seq = %v, want %v", got, "123")
	}
	if got := saw.unique; got != "true" {
		t.Errorf("saw.unique = %v, want %v", got, "true")
	}
}

func TestMCP_GetCard_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cards/card-i-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Card{
			CardID:       "card-i-zzz",
			CardCommonID: "card-c-zzz",
			Name:         "Looked Up",
			SequentialID: 99,
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getCardToolName,
		Arguments: map[string]any{
			"card_id": "card-i-zzz",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[favro.Card](t, res)
	if got := out.CardID; got != "card-i-zzz" {
		t.Errorf("out.CardID = %v, want %v", got, "card-i-zzz")
	}
	if got := out.CardCommonID; got != "card-c-zzz" {
		t.Errorf("out.CardCommonID = %v, want %v", got, "card-c-zzz")
	}
	if got := out.Name; got != "Looked Up" {
		t.Errorf("out.Name = %v, want %v", got, "Looked Up")
	}
	if got := out.SequentialID; got != 99 {
		t.Errorf("out.SequentialID = %v, want %v", got, 99)
	}
}

func TestMCP_GetCard_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, getCardToolName, "card_id")
}

// TestCardCustomFieldValuesPassOutputValidation is the regression test
// for a bug no fixture could have caught.
//
// Card.customFields carries json.RawMessage fields, which are []byte in
// Go, so schema inference described them as arrays of integers 0-255.
// The SDK validates every result against that schema, so any card with
// a Vote, Members, Tags, Status or Multiple-select custom field — whose
// value is an array of ids — failed with a protocol error instead of
// returning. It survived every test here because the fixtures sent what
// the schema claimed; a live read against a real organization is what
// found it.
//
// So the payload below is deliberately the shape Favro actually sends:
// ids as strings, an object for a timeline, an object for a link.
func TestCardCustomFieldValuesPassOutputValidation(t *testing.T) {
	t.Parallel()

	const body = `{"limit":100,"page":0,"pages":1,"requestId":"r-1","entities":[{
		"cardId":"ci-1","cardCommonId":"cc-1","name":"A card","widgetCommonId":"w-1",
		"customFields":[
			{"customFieldId":"cf-members","value":["u-1","u-2"]},
			{"customFieldId":"cf-status","value":["item-1"]},
			{"customFieldId":"cf-link","link":{"url":"https://example.test","text":"docs"}},
			{"customFieldId":"cf-timeline","timeline":{"startDate":"2026-01-01","dueDate":"2026-02-01","showTime":false}},
			{"customFieldId":"cf-time","total":3600,"reports":{"u-1":{"value":3600}}},
			{"customFieldId":"cf-progress","value":{"percentage":40}}
		]}]}`

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	cs := connectInMemoryWith(t, c)

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listCardsToolName,
		Arguments: map[string]any{"widget_common_id": "w-1"},
	})
	// A schema violation surfaces as a protocol error, not a tool
	// error, so this is the assertion that would have failed.
	if err != nil {
		t.Fatalf("a card carrying real custom-field values must validate: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %v", res.Content)
	}

	out := decodeStructured[listOutput[favro.Card]](t, res)
	if len(out.Items) != 1 {
		t.Fatalf("len(out.Items) = %d, want 1", len(out.Items))
	}
	if got := len(out.Items[0].CustomFields()); got != 6 {
		t.Errorf("len(CustomFields()) = %d, want 6", got)
	}
}
