package server

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestMCP_DeleteTag_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("delete_tag must DELETE; got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/tags/") {
			t.Errorf("expected /tags/{id}; got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      deleteTagToolName,
		Arguments: map[string]any{"tag_id": "abc123"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[struct{}]](t, res)
	require.False(t, out.DryRun)
	// Result is *struct{}: a non-nil pointer to an empty struct on a
	// successful live delete. The caller's contract is "if !DryRun,
	// the delete succeeded"; the empty payload conveys no extra info.
	require.NotNil(t, out.Result)
}

func TestMCP_DeleteTag_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: deleteTagToolName,
		Arguments: map[string]any{
			"tag_id":  "abc123",
			"dry_run": true,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[struct{}]](t, res)
	require.True(t, out.DryRun)
	require.Nil(t, out.Result)
	require.NotNil(t, out.WouldCall)
	require.Equal(t, http.MethodDelete, out.WouldCall.Method)
	require.Contains(t, out.WouldCall.URL, "/tags/abc123")
	require.Contains(t, out.PredictedStateDiff, "abc123")

	require.EqualValues(t, 0, calls.Load(), "dry_run must short-circuit before any Favro call")
}

func TestMCP_DeleteTag_MissingTagID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, deleteTagToolName, "tag_id")
}
