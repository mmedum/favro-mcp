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
