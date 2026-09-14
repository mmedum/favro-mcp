package tools

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[struct{}]](t, res)
	if out.DryRun {
		t.Error("out.DryRun = true, want false")
	}
	// Result is *struct{}: a non-nil pointer to an empty struct on a
	// successful live delete. The caller's contract is "if !DryRun,
	// the delete succeeded"; the empty payload conveys no extra info.
	if out.Result == nil {
		t.Fatal("out.Result is nil")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[struct{}]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if out.Result != nil {
		t.Errorf("out.Result = %v, want nil", out.Result)
	}
	if out.WouldCall == nil {
		t.Fatal("out.WouldCall is nil")
	}
	if got := out.WouldCall.Method; got != http.MethodDelete {
		t.Errorf("out.WouldCall.Method = %v, want %v", got, http.MethodDelete)
	}
	if !strings.Contains(out.WouldCall.URL, "/tags/abc123") {
		t.Errorf("out.WouldCall.URL does not contain %q", "/tags/abc123")
	}
	if !strings.Contains(out.PredictedStateDiff, "abc123") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "abc123")
	}

	if got := calls.Load(); got != 0 {
		t.Errorf("dry_run must short-circuit before any Favro call: got %v, want %v", got, 0)
	}
}

func TestMCP_DeleteTag_MissingTagID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, deleteTagToolName, "tag_id")
}
