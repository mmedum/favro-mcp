package server

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_UpdateTags_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("update_tags must PUT; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		// Fan-out: each entry hits its per-tag URL.
		switch r.URL.Path {
		case "/tags/t-1":
			_, _ = w.Write([]byte(`{"tagId":"t-1","name":"renamed-a","color":"red"}`))
		case "/tags/t-2":
			_, _ = w.Write([]byte(`{"tagId":"t-2","name":"existing","color":"blue"}`))
		default:
			t.Errorf("unexpected per-tag path: %s", r.URL.Path)
		}
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateTagsToolName,
		Arguments: map[string]any{
			"updates": []map[string]any{
				{"tag_id": "t-1", "name": "renamed-a", "color": "red"},
				{"tag_id": "t-2", "color": "blue"},
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[[]favro.Tag]](t, res)
	require.False(t, out.DryRun)
	require.NotNil(t, out.Result)
	require.Len(t, *out.Result, 2)
	require.Equal(t, "renamed-a", (*out.Result)[0].Name, "results must be returned in input order")
	require.Equal(t, "blue", (*out.Result)[1].Color)
}

func TestMCP_UpdateTags_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateTagsToolName,
		Arguments: map[string]any{
			"updates": []map[string]any{
				{"tag_id": "t-1", "name": "renamed-a"},
				{"tag_id": "t-2", "color": "blue"},
			},
			"dry_run": true,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[[]favro.Tag]](t, res)
	require.True(t, out.DryRun)
	require.Nil(t, out.Result, "dry_run must NOT populate Result")
	require.NotNil(t, out.WouldCall)
	require.Equal(t, http.MethodPut, out.WouldCall.Method)
	require.Contains(t, out.WouldCall.URL, "/tags/{tagId}")
	require.Contains(t, out.WouldCall.URL, "fan-out", "URL must surface the parallel-fan-out reality")
	body, ok := out.RequestBody.([]any)
	require.True(t, ok, "RequestBody must decode as a JSON array; got %T", out.RequestBody)
	require.Len(t, body, 2)
	require.Contains(t, out.PredictedStateDiff, "t-1")
	require.Contains(t, out.PredictedStateDiff, "renamed-a")
	require.Contains(t, out.PredictedStateDiff, "t-2")
	require.Contains(t, out.PredictedStateDiff, "blue")

	require.EqualValues(t, 0, calls.Load(), "dry_run must short-circuit before any Favro call")
}

// TestMCP_UpdateTags_NoChangesEntry_DryRun pins that an entry with
// neither name nor color is reported as a per-entry no-op in the
// state diff so the LLM can spot a malformed request before sending.
func TestMCP_UpdateTags_NoChangesEntry_DryRun(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateTagsToolName,
		Arguments: map[string]any{
			"updates": []map[string]any{{"tag_id": "t-1"}},
			"dry_run": true,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[[]favro.Tag]](t, res)
	require.True(t, out.DryRun)
	require.Contains(t, out.PredictedStateDiff, "no-op",
		"a dry-run with no name/color set on an entry must report a no-op so the LLM doesn't think a change happened")
}

// TestMCP_UpdateTags_InvalidatesCacheOnSuccess pins the contract
// that a successful live bulk-write invalidates the tag cache, but
// a dry-run does NOT. Mirrors the create_tag invalidation test.
func TestMCP_UpdateTags_InvalidatesCacheOnSuccess(t *testing.T) {
	t.Parallel()

	var listCalls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
				Pages:    1,
				Entities: []favro.Tag{{TagID: "t-1", Name: "frontend"}},
			})
		case http.MethodPut:
			// Fan-out: per-tag URL — t-1 is the only tag we update
			// in this test, so the path will be /tags/t-1.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tagId":"t-1","name":"frontend-renamed","color":"red"}`))
		}
	}))

	cs := connectInMemoryWith(t, c)

	// Warm the tag cache via favro_resolve_tag.
	_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      resolveTagToolName,
		Arguments: map[string]any{"name": "frontend"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, listCalls.Load())

	// Dry-run update_tags — must NOT invalidate the cache.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateTagsToolName,
		Arguments: map[string]any{
			"updates": []map[string]any{{"tag_id": "t-1", "name": "preview"}},
			"dry_run": true,
		},
	})
	require.NoError(t, err)
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      resolveTagToolName,
		Arguments: map[string]any{"name": "frontend"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, listCalls.Load(),
		"dry_run update_tags must NOT invalidate the tag cache")

	// Live update_tags — must invalidate the cache.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateTagsToolName,
		Arguments: map[string]any{
			"updates": []map[string]any{{"tag_id": "t-1", "name": "frontend-renamed", "color": "red"}},
		},
	})
	require.NoError(t, err)
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      resolveTagToolName,
		Arguments: map[string]any{"name": "frontend"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, listCalls.Load(),
		"live update_tags must invalidate the tag cache so the next resolve re-fetches")
}

func TestMCP_UpdateTags_MissingUpdates(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, updateTagsToolName, "updates")
}
