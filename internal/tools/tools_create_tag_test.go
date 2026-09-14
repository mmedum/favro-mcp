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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Tag]](t, res)
	if out.DryRun {
		t.Error("out.DryRun = true, want false")
	}
	if out.Result == nil {
		t.Fatal("out.Result is nil")
	}
	if got := out.Result.TagID; got != "new-1" {
		t.Errorf("out.Result.TagID = %v, want %v", got, "new-1")
	}
	if out.WouldCall != nil {
		t.Errorf("live mode must not populate WouldCall: %v", out.WouldCall)
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Tag]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if out.Result != nil {
		t.Errorf("dry_run must NOT populate Result: %v", out.Result)
	}
	if out.WouldCall == nil {
		t.Fatal("out.WouldCall is nil")
	}
	if got := out.WouldCall.Method; got != http.MethodPost {
		t.Errorf("out.WouldCall.Method = %v, want %v", got, http.MethodPost)
	}
	if !strings.Contains(out.WouldCall.URL, "/tags") {
		t.Errorf("out.WouldCall.URL does not contain %q", "/tags")
	}
	body, ok := out.RequestBody.(map[string]any)
	if !ok {
		t.Errorf("RequestBody must decode as a JSON object; got %T", out.RequestBody)
	}
	if got := body["name"]; got != "preview-only" {
		t.Errorf("body[\"name\"] = %v, want %v", got, "preview-only")
	}
	if !strings.Contains(out.PredictedStateDiff, "preview-only") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "preview-only")
	}

	if got := calls.Load(); got != 0 {
		t.Errorf("dry_run must short-circuit before any Favro call: got %v, want %v", got, 0)
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := listCalls.Load(); got != 1 {
		t.Errorf("listCalls.Load() = %v, want %v", got, 1)
	}

	// Re-resolve — should hit the cache.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      resolveTagToolName,
		Arguments: map[string]any{"name": "frontend"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := listCalls.Load(); got != 1 {
		t.Errorf("second resolve must hit the cache: got %v, want %v", got, 1)
	}

	// Dry-run create_tag — must NOT invalidate the cache.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      createTagToolName,
		Arguments: map[string]any{"name": "preview", "dry_run": true},
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
		t.Errorf("dry_run create_tag must NOT invalidate the tag cache: got %v, want %v", got, 1)
	}

	// Live create_tag — must invalidate the cache.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      createTagToolName,
		Arguments: map[string]any{"name": "actual"},
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
		t.Errorf("live create_tag must invalidate the tag cache so the next resolve re-fetches: got %v, want %v", got, 2)
	}
}

// TestMCP_CreateTag_MissingName pins the SDK-level required-field
// rejection.
func TestMCP_CreateTag_MissingName(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, createTagToolName, "name")
}
