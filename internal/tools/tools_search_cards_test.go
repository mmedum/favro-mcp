package tools

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_SearchCards_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// In-handler asserts use t.Errorf rather than require.* —
		// t.Errorf rather than t.Fatalf: FailNow is only legal on the
		// goroutine running the test, and this is an HTTP handler.
		if got := r.URL.Query().Get("descriptionFormat"); got != "markdown" {
			t.Errorf("the MCP tool must propagate descriptionFormat=markdown to Favro; got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Card]{
			Page: 0, Pages: 1,
			Entities: []favro.Card{
				{CardID: "card-1", CardCommonID: "cc-1", Name: "Printing pass setup"},
				{CardID: "card-2", CardCommonID: "cc-2", Name: "Unrelated card"},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: searchCardsToolName,
		Arguments: map[string]any{
			"query":            "printing",
			"widget_common_id": "w-1",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool must succeed; got %s", serializedResponseString(t, res))
	}

	out := decodeStructured[searchCardsOutput](t, res)
	if len(out.Results) != 1 {
		t.Fatalf("len(out.Results) = %d, want 1", len(out.Results))
	}
	if got := out.Results[0].CardID; got != "card-1" {
		t.Errorf("out.Results[0].CardID = %v, want %v", got, "card-1")
	}
	if out.Cached {
		t.Error("out.Cached = true, want false")
	}
}

func TestMCP_SearchCards_MissingQuery(t *testing.T) {
	t.Parallel()

	assertMissingRequiredFieldFails(t, searchCardsToolName, "query")
}

// TestMCP_SearchCards_MissingScope pins the contract that omitting
// both widget_common_id and collection_id surfaces as a tool error
// rather than silently failing against Favro's HTTP 400.
func TestMCP_SearchCards_MissingScope(t *testing.T) {
	t.Parallel()

	calls := 0
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      searchCardsToolName,
		Arguments: map[string]any{"query": "printing"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("missing scope must surface as a tool error")
	}
	if got := calls; got != 0 {
		t.Errorf("missing scope must short-circuit before any Favro call: got %v, want %v", got, 0)
	}

	full := strings.ToLower(serializedResponseString(t, res))
	if !strings.Contains(full, "widget_common_id") {
		t.Errorf("full does not contain %q", "widget_common_id")
	}
	if !strings.Contains(full, "collection_id") {
		t.Errorf("full does not contain %q", "collection_id")
	}
}

// TestMCP_SearchCards_ScopeConflict pins the contract that passing
// both widget_common_id and collection_id surfaces as a tool error
// rather than silently choosing one.
func TestMCP_SearchCards_ScopeConflict(t *testing.T) {
	t.Parallel()

	calls := 0
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: searchCardsToolName,
		Arguments: map[string]any{
			"query":            "printing",
			"widget_common_id": "w-1",
			"collection_id":    "c-1",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("scope conflict must surface as a tool error")
	}
	if got := calls; got != 0 {
		t.Errorf("scope conflict must short-circuit before any Favro call: got %v, want %v", got, 0)
	}

	full := strings.ToLower(serializedResponseString(t, res))
	if !strings.Contains(full, "widget_common_id") {
		t.Errorf("full does not contain %q", "widget_common_id")
	}
	if !strings.Contains(full, "collection_id") {
		t.Errorf("full does not contain %q", "collection_id")
	}
}

func TestMCP_SearchCards_WidgetScope(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("widgetCommonId"); got != "w-99" {
			t.Errorf("widget scope must propagate widgetCommonId=w-99; got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Card]{
			Page: 0, Pages: 1,
			Entities: []favro.Card{
				{CardID: "wc-1", CardCommonID: "cc-1", WidgetCommonID: "w-99", Name: "Onboarding flow"},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: searchCardsToolName,
		Arguments: map[string]any{
			"query":            "onboarding",
			"widget_common_id": "w-99",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[searchCardsOutput](t, res)
	if len(out.Results) != 1 {
		t.Fatalf("len(out.Results) = %d, want 1", len(out.Results))
	}
	if got := out.Results[0].CardID; got != "wc-1" {
		t.Errorf("out.Results[0].CardID = %v, want %v", got, "wc-1")
	}
}
