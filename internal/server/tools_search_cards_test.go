package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_SearchCards_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// In-handler asserts use t.Errorf rather than require.* —
		// require's testify-fail panics are goroutine-unsafe and
		// testifylint's go-require rule rejects them in handlers.
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool must succeed; got %s", serializedResponseString(t, res))

	out := decodeStructured[searchCardsOutput](t, res)
	require.Len(t, out.Results, 1)
	require.Equal(t, "card-1", out.Results[0].CardID)
	require.False(t, out.Cached)
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
	require.NoError(t, err)
	require.True(t, res.IsError, "missing scope must surface as a tool error")
	require.Equal(t, 0, calls, "missing scope must short-circuit before any Favro call")

	full := strings.ToLower(serializedResponseString(t, res))
	require.Contains(t, full, "widget_common_id")
	require.Contains(t, full, "collection_id")
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
	require.NoError(t, err)
	require.True(t, res.IsError, "scope conflict must surface as a tool error")
	require.Equal(t, 0, calls, "scope conflict must short-circuit before any Favro call")

	full := strings.ToLower(serializedResponseString(t, res))
	require.Contains(t, full, "widget_common_id")
	require.Contains(t, full, "collection_id")
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
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[searchCardsOutput](t, res)
	require.Len(t, out.Results, 1)
	require.Equal(t, "wc-1", out.Results[0].CardID)
}
