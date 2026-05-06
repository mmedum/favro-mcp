package server

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_CreateWidget_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("create_widget must POST; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"widgetCommonId":"w-new","name":"Sprint","collectionIds":["c-1"]}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createWidgetToolName,
		Arguments: map[string]any{
			"collection_id": "c-1",
			"name":          "Sprint",
			"type":          "backlog",
		},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Widget]](t, res)
	require.Equal(t, "w-new", out.Result.WidgetCommonID)
}

func TestMCP_CreateWidget_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createWidgetToolName,
		Arguments: map[string]any{
			"collection_id": "c-1",
			"name":          "preview",
			"dry_run":       true,
		},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Widget]](t, res)
	require.True(t, out.DryRun)
	require.Contains(t, out.PredictedStateDiff, "preview")
	require.Contains(t, out.PredictedStateDiff, "c-1")
	require.EqualValues(t, 0, calls.Load())
}

func TestMCP_CreateWidget_MissingFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		field string
	}{
		{"missing collection_id", "collection_id"},
		{"missing name", "name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertMissingRequiredFieldFails(t, createWidgetToolName, tc.field)
		})
	}
}

func TestMCP_UpdateWidget_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("update_widget must PUT; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"widgetCommonId":"w-1","name":"renamed"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateWidgetToolName,
		Arguments: map[string]any{"widget_common_id": "w-1", "name": "renamed"},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Widget]](t, res)
	require.Equal(t, "renamed", out.Result.Name)
}

func TestMCP_UpdateWidget_MissingWidgetCommonID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, updateWidgetToolName, "widget_common_id")
}

func TestMCP_DeleteWidget_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("delete_widget must DELETE; got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      deleteWidgetToolName,
		Arguments: map[string]any{"widget_common_id": "w-1"},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[struct{}]](t, res)
	require.False(t, out.DryRun)
}

func TestMCP_DeleteWidget_MissingWidgetCommonID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, deleteWidgetToolName, "widget_common_id")
}
