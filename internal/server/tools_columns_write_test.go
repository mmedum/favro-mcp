package server

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_CreateColumn_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("create_column must POST; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"columnId":"col-new","widgetCommonId":"w-1","name":"Done","position":3}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createColumnToolName,
		Arguments: map[string]any{
			"widget_common_id": "w-1",
			"name":             "Done",
		},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Column]](t, res)
	require.Equal(t, "col-new", out.Result.ColumnID)
}

func TestMCP_CreateColumn_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createColumnToolName,
		Arguments: map[string]any{
			"widget_common_id": "w-1",
			"name":             "preview",
			"dry_run":          true,
		},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Column]](t, res)
	require.True(t, out.DryRun)
	require.EqualValues(t, 0, calls.Load())
}

func TestMCP_CreateColumn_MissingFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		field string
	}{
		{"missing widget_common_id", "widget_common_id"},
		{"missing name", "name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertMissingRequiredFieldFails(t, createColumnToolName, tc.field)
		})
	}
}

func TestMCP_UpdateColumn_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("update_column must PUT; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"columnId":"col-1","name":"renamed"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateColumnToolName,
		Arguments: map[string]any{"column_id": "col-1", "name": "renamed"},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Column]](t, res)
	require.Equal(t, "renamed", out.Result.Name)
}

func TestMCP_UpdateColumn_MissingColumnID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, updateColumnToolName, "column_id")
}

func TestMCP_DeleteColumn_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("delete_column must DELETE; got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      deleteColumnToolName,
		Arguments: map[string]any{"column_id": "col-1"},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[struct{}]](t, res)
	require.False(t, out.DryRun)
}

func TestMCP_DeleteColumn_MissingColumnID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, deleteColumnToolName, "column_id")
}
