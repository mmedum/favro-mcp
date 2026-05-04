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

func TestMCP_GetCardFull_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/cards/"):
			_ = json.NewEncoder(w).Encode(favro.Card{
				CardID:         "c-1",
				CardCommonID:   "cc-1",
				Name:           "Print visitor passes",
				WidgetCommonID: "w-1",
				ColumnID:       "col-2",
				Tags:           []string{"tag-1"},
			})
		case r.URL.Path == "/widgets":
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Widget]{
				Pages: 1,
				Entities: []favro.Widget{
					{WidgetCommonID: "w-1", Name: "Sprint Board", CollectionIDs: []string{"col-A"}},
				},
			})
		case r.URL.Path == "/columns":
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Column]{
				Pages: 1,
				Entities: []favro.Column{
					{ColumnID: "col-2", WidgetCommonID: "w-1", Name: "Done"},
				},
			})
		case r.URL.Path == "/collections":
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Collection]{
				Pages:    1,
				Entities: []favro.Collection{{CollectionID: "col-A", Name: "Engineering"}},
			})
		case r.URL.Path == "/tags":
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
				Pages:    1,
				Entities: []favro.Tag{{TagID: "tag-1", Name: "frontend", Color: "blue"}},
			})
		default:
			t.Errorf("unexpected fixture path: %s", r.URL.Path)
		}
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      getCardFullToolName,
		Arguments: map[string]any{"card_id": "c-1"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool must succeed; got %s", serializedResponseString(t, res))

	out := decodeStructured[FullCard](t, res)
	require.Equal(t, "Print visitor passes", out.Name)
	require.Equal(t, "Sprint Board", out.WidgetName)
	require.Equal(t, "Done", out.ColumnName)
	require.Equal(t, []string{"Engineering"}, out.CollectionNames)
	require.Len(t, out.ResolvedTags, 1)
	require.Equal(t, "frontend", out.ResolvedTags[0].Name)
}

// TestMCP_GetCardFull_IdentityRequired pins that the SDK-layer
// rejects calls with no identity field via the typed error from
// the resolver — distinct from a missing-field schema error
// because the schema treats every identity field as optional
// (the "exactly one of N" rule lives in the handler).
func TestMCP_GetCardFull_IdentityRequired(t *testing.T) {
	t.Parallel()

	calls := 0
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      getCardFullToolName,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.True(t, res.IsError, "missing identity must surface as a tool error")
	require.Equal(t, 0, calls, "missing identity must short-circuit before any Favro call")

	full := strings.ToLower(serializedResponseString(t, res))
	require.Contains(t, full, "card_id")
	require.Contains(t, full, "card_common_id")
	require.Contains(t, full, "sequential_id")
}
