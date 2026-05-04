package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListWidgets_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Widget]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-w",
			Entities: []favro.Widget{
				{WidgetCommonID: "w-1", Name: "Sprint Board", Type: "board"},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listWidgetsToolName,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[listOutput[favro.Widget]](t, res)
	require.Len(t, out.Items, 1)
	require.Equal(t, "Sprint Board", out.Items[0].Name)
	require.Equal(t, "board", out.Items[0].Type)
	require.NotNil(t, out.NextPage)
	require.Equal(t, 1, *out.NextPage)
}

func TestMCP_ListWidgets_FiltersByCollection(t *testing.T) {
	t.Parallel()

	var sawCollection string
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawCollection = r.URL.Query().Get("collectionId")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Widget]{Page: 0, Pages: 1})
	}))

	cs := connectInMemoryWith(t, c)
	_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: listWidgetsToolName,
		Arguments: map[string]any{
			"collection_id": "c-xyz",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "c-xyz", sawCollection,
		"collection_id input must reach Favro as ?collectionId=")
}

func TestMCP_GetWidget_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/widgets/w-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Widget{
			WidgetCommonID: "w-zzz",
			Name:           "Looked Up",
			Type:           "board",
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getWidgetToolName,
		Arguments: map[string]any{
			"widget_common_id": "w-zzz",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[favro.Widget](t, res)
	require.Equal(t, "w-zzz", out.WidgetCommonID)
	require.Equal(t, "board", out.Type)
}

func TestMCP_GetWidget_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertGetMissingIDFails(t, getWidgetToolName, "widget_common_id")
}
