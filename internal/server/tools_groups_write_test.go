package server

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_CreateGroup_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("create_group must POST; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"groupId":"g-new","name":"Eng"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      createGroupToolName,
		Arguments: map[string]any{"name": "Eng"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Group]](t, res)
	require.Equal(t, "g-new", out.Result.GroupID)
}

func TestMCP_CreateGroup_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      createGroupToolName,
		Arguments: map[string]any{"name": "preview", "dry_run": true},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Group]](t, res)
	require.True(t, out.DryRun)
	require.EqualValues(t, 0, calls.Load())
}

func TestMCP_CreateGroup_MissingName(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, createGroupToolName, "name")
}

func TestMCP_UpdateGroup_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("update_group must PUT; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"groupId":"g-1","name":"renamed"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateGroupToolName,
		Arguments: map[string]any{"group_id": "g-1", "name": "renamed"},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Group]](t, res)
	require.Equal(t, "renamed", out.Result.Name)
}

func TestMCP_UpdateGroup_NoChanges_DryRun(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateGroupToolName,
		Arguments: map[string]any{"group_id": "g-1", "dry_run": true},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Group]](t, res)
	require.Contains(t, out.PredictedStateDiff, "no-op")
}

func TestMCP_UpdateGroup_MissingGroupID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, updateGroupToolName, "group_id")
}

func TestMCP_DeleteGroup_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("delete_group must DELETE; got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      deleteGroupToolName,
		Arguments: map[string]any{"group_id": "g-1"},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[struct{}]](t, res)
	require.False(t, out.DryRun)
}

func TestMCP_DeleteGroup_MissingGroupID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, deleteGroupToolName, "group_id")
}
