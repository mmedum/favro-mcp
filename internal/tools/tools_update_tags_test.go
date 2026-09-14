package tools

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[[]favro.Tag]](t, res)
	if out.DryRun {
		t.Error("out.DryRun = true, want false")
	}
	if out.Result == nil {
		t.Fatal("out.Result is nil")
	}
	if len(*out.Result) != 2 {
		t.Fatalf("len(*out.Result) = %d, want 2", len(*out.Result))
	}
	if got := (*out.Result)[0].Name; got != "renamed-a" {
		t.Errorf("results must be returned in input order: got %v, want %v", got, "renamed-a")
	}
	if got := (*out.Result)[1].Color; got != "blue" {
		t.Errorf("(*out.Result)[1].Color = %v, want %v", got, "blue")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[[]favro.Tag]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if out.Result != nil {
		t.Errorf("dry_run must NOT populate Result: %v", out.Result)
	}
	if out.WouldCall == nil {
		t.Fatal("out.WouldCall is nil")
	}
	if got := out.WouldCall.Method; got != http.MethodPut {
		t.Errorf("out.WouldCall.Method = %v, want %v", got, http.MethodPut)
	}
	if !strings.Contains(out.WouldCall.URL, "/tags/{tagId}") {
		t.Errorf("out.WouldCall.URL does not contain %q", "/tags/{tagId}")
	}
	if !strings.Contains(out.WouldCall.URL, "fan-out") {
		t.Errorf("URL must surface the parallel-fan-out reality: %q missing", "fan-out")
	}
	body, ok := out.RequestBody.([]any)
	if !ok {
		t.Errorf("RequestBody must decode as a JSON array; got %T", out.RequestBody)
	}
	if len(body) != 2 {
		t.Fatalf("len(body) = %d, want 2", len(body))
	}
	if !strings.Contains(out.PredictedStateDiff, "t-1") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "t-1")
	}
	if !strings.Contains(out.PredictedStateDiff, "renamed-a") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "renamed-a")
	}
	if !strings.Contains(out.PredictedStateDiff, "t-2") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "t-2")
	}
	if !strings.Contains(out.PredictedStateDiff, "blue") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "blue")
	}

	if got := calls.Load(); got != 0 {
		t.Errorf("dry_run must short-circuit before any Favro call: got %v, want %v", got, 0)
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[[]favro.Tag]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if !strings.Contains(out.PredictedStateDiff, "no-op") {
		t.Errorf("a dry-run with no name/color set on an entry must report a no-op so the LLM doesn't think a change happened: %q missing", "no-op")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := listCalls.Load(); got != 1 {
		t.Errorf("listCalls.Load() = %v, want %v", got, 1)
	}

	// Dry-run update_tags — must NOT invalidate the cache.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateTagsToolName,
		Arguments: map[string]any{
			"updates": []map[string]any{{"tag_id": "t-1", "name": "preview"}},
			"dry_run": true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      resolveTagToolName,
		Arguments: map[string]any{"name": "frontend"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := listCalls.Load(); got != 1 {
		t.Errorf("dry_run update_tags must NOT invalidate the tag cache: got %v, want %v", got, 1)
	}

	// Live update_tags — must invalidate the cache.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateTagsToolName,
		Arguments: map[string]any{
			"updates": []map[string]any{{"tag_id": "t-1", "name": "frontend-renamed", "color": "red"}},
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      resolveTagToolName,
		Arguments: map[string]any{"name": "frontend"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := listCalls.Load(); got != 2 {
		t.Errorf("live update_tags must invalidate the tag cache so the next resolve re-fetches: got %v, want %v", got, 2)
	}
}

func TestMCP_UpdateTags_MissingUpdates(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, updateTagsToolName, "updates")
}
