package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListColumns_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Column]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-c",
			Entities: []favro.Column{
				{ColumnID: "col-1", WidgetCommonID: "w-1", Name: "Doing", Position: 1, CardCount: 4},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: listColumnsToolName,
		Arguments: map[string]any{
			"widget_common_id": "w-1",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[listOutput[favro.Column]](t, res)
	require.Len(t, out.Items, 1)
	require.Equal(t, "Doing", out.Items[0].Name)
	require.Equal(t, 1, out.Items[0].Position)
	require.NotNil(t, out.NextPage)
	require.Equal(t, 1, *out.NextPage)
}

func TestMCP_ListColumns_MissingWidget_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertGetMissingIDFails(t, listColumnsToolName, "widget_common_id")
}

func TestMCP_ListColumns_FiltersByWidget(t *testing.T) {
	t.Parallel()

	var sawWidget string
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawWidget = r.URL.Query().Get("widgetCommonId")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Column]{Page: 0, Pages: 1})
	}))

	cs := connectInMemoryWith(t, c)
	_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: listColumnsToolName,
		Arguments: map[string]any{
			"widget_common_id": "w-xyz",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "w-xyz", sawWidget,
		"widget_common_id input must reach Favro as ?widgetCommonId=")
}

func TestMCP_GetColumn_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/columns/col-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Column{
			ColumnID:       "col-zzz",
			WidgetCommonID: "w-1",
			Name:           "Done",
			Position:       4,
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getColumnToolName,
		Arguments: map[string]any{
			"column_id": "col-zzz",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[favro.Column](t, res)
	require.Equal(t, "col-zzz", out.ColumnID)
	require.Equal(t, "Done", out.Name)
	require.Equal(t, 4, out.Position)
}

func TestMCP_GetColumn_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertGetMissingIDFails(t, getColumnToolName, "column_id")
}
