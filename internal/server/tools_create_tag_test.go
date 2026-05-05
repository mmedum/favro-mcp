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

func TestMCP_CreateTag_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != http.MethodPost {
			t.Errorf("create_tag must POST; got %s", got)
		}
		if got := r.URL.Path; got != "/tags" {
			t.Errorf("expected /tags; got %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tagId":"new-1","organizationId":"org-1","name":"company","color":"blue"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createTagToolName,
		Arguments: map[string]any{
			"name":  "company",
			"color": "blue",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Tag]](t, res)
	require.False(t, out.DryRun)
	require.NotNil(t, out.Result)
	require.Equal(t, "new-1", out.Result.TagID)
	require.Nil(t, out.WouldCall, "live mode must not populate WouldCall")
}

// TestMCP_CreateTag_DryRun pins the dry-run behavior at the MCP
// layer: dry_run:true must (a) NOT contact Favro, (b) return a
// writeOutput with DryRun=true + WouldCall + RequestBody +
// PredictedStateDiff populated, (c) keep Result nil.
func TestMCP_CreateTag_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createTagToolName,
		Arguments: map[string]any{
			"name":    "preview-only",
			"dry_run": true,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Tag]](t, res)
	require.True(t, out.DryRun)
	require.Nil(t, out.Result, "dry_run must NOT populate Result")
	require.NotNil(t, out.WouldCall)
	require.Equal(t, http.MethodPost, out.WouldCall.Method)
	require.Contains(t, out.WouldCall.URL, "/tags")
	body, ok := out.RequestBody.(map[string]any)
	require.True(t, ok, "RequestBody must decode as a JSON object; got %T", out.RequestBody)
	require.Equal(t, "preview-only", body["name"])
	require.Contains(t, out.PredictedStateDiff, "preview-only")

	require.EqualValues(t, 0, calls.Load(), "dry_run must short-circuit before any Favro call")
}

// TestMCP_CreateTag_InvalidatesCacheOnSuccess pins the contract
// that a successful live write invalidates the resolver's tag
// cache, but a dry-run does NOT (no state changed, cache still
// correct).
func TestMCP_CreateTag_InvalidatesCacheOnSuccess(t *testing.T) {
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
		case http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tagId":"t-2","name":"new","color":"red"}`))
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

	// Re-resolve — should hit the cache.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      resolveTagToolName,
		Arguments: map[string]any{"name": "frontend"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, listCalls.Load(), "second resolve must hit the cache")

	// Dry-run create_tag — must NOT invalidate the cache.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      createTagToolName,
		Arguments: map[string]any{"name": "preview", "dry_run": true},
	})
	require.NoError(t, err)
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      resolveTagToolName,
		Arguments: map[string]any{"name": "frontend"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, listCalls.Load(),
		"dry_run create_tag must NOT invalidate the tag cache")

	// Live create_tag — must invalidate the cache.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      createTagToolName,
		Arguments: map[string]any{"name": "actual"},
	})
	require.NoError(t, err)
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      resolveTagToolName,
		Arguments: map[string]any{"name": "frontend"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, listCalls.Load(),
		"live create_tag must invalidate the tag cache so the next resolve re-fetches")
}

// TestMCP_CreateTag_MissingName pins the SDK-level required-field
// rejection.
func TestMCP_CreateTag_MissingName(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, createTagToolName, "name")
}
