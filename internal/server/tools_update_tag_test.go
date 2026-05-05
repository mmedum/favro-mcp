package server

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_UpdateTag_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("update_tag must PUT; got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/tags/") {
			t.Errorf("expected /tags/{id}; got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tagId":"abc","name":"renamed","color":"red"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateTagToolName,
		Arguments: map[string]any{"tag_id": "abc", "name": "renamed", "color": "red"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Tag]](t, res)
	require.False(t, out.DryRun)
	require.Equal(t, "renamed", out.Result.Name)
}

func TestMCP_UpdateTag_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateTagToolName,
		Arguments: map[string]any{
			"tag_id":  "abc",
			"name":    "renamed",
			"color":   "red",
			"dry_run": true,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Tag]](t, res)
	require.True(t, out.DryRun)
	require.Equal(t, http.MethodPut, out.WouldCall.Method)
	require.Contains(t, out.WouldCall.URL, "/tags/abc")
	require.Contains(t, out.PredictedStateDiff, "renamed")
	require.Contains(t, out.PredictedStateDiff, "red")
	require.EqualValues(t, 0, calls.Load())
}

func TestMCP_UpdateTag_NoChanges_DryRun(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateTagToolName,
		Arguments: map[string]any{"tag_id": "abc", "dry_run": true},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Tag]](t, res)
	require.Contains(t, out.PredictedStateDiff, "no-op",
		"a dry-run with no name/color set must report a no-op so the LLM doesn't think a change happened")
}

func TestMCP_UpdateTag_MissingTagID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, updateTagToolName, "tag_id")
}
