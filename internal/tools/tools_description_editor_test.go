package tools

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
	"github.com/mmedum/favro-mcp/internal/favroapi"
	"github.com/mmedum/favro-mcp/internal/service"
)

// descriptionEditorFixture wires a server that:
//   - GET /cards/{id}?descriptionFormat=markdown → returns the
//     supplied body
//   - PUT /cards/{id} → echoes a Card back
//
// Used by every test that exercises the three description editors.
func descriptionEditorFixture(t *testing.T, body string, requireDescFormat string) *favroapi.Client {
	t.Helper()
	return favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if requireDescFormat != "" && r.URL.Query().Get("descriptionFormat") != requireDescFormat {
				t.Errorf("editor MUST request descriptionFormat=%q; got %q", requireDescFormat, r.URL.Query().Get("descriptionFormat"))
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.Card{
				CardID:              "ci-1",
				CardCommonID:        "cc-1",
				DetailedDescription: body,
			})
		case http.MethodPut:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cardId":"ci-1","cardCommonId":"cc-1"}`))
		}
	}))
}

func TestMCP_AppendCardDescription_HappyPath(t *testing.T) {
	t.Parallel()

	c := descriptionEditorFixture(t, "# heading\n\nbody", "markdown")

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      appendCardDescriptionToolName,
		Arguments: map[string]any{"card_id": "ci-1", "text": "appended"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[service.EditorResult]](t, res)
	if out.DryRun {
		t.Error("out.DryRun = true, want false")
	}
	if out.Result == nil {
		t.Fatal("out.Result is nil")
	}
	if got := out.Result.Old; got != "# heading\n\nbody" {
		t.Errorf("out.Result.Old = %v, want %v", got, "# heading\n\nbody")
	}
	if got := out.Result.New; got != "# heading\n\nbody\n\nappended" {
		t.Errorf("out.Result.New = %v, want %v", got, "# heading\n\nbody\n\nappended")
	}
	if !strings.Contains(out.Result.UnifiedDiff, "+appended") {
		t.Errorf("out.Result.UnifiedDiff does not contain %q", "+appended")
	}
}

func TestMCP_AppendCardDescription_DryRun_PreviewsDiff(t *testing.T) {
	t.Parallel()

	var puts atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.Card{
				CardID:              "ci-1",
				DetailedDescription: "before",
			})
		case http.MethodPut:
			puts.Add(1)
		}
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: appendCardDescriptionToolName,
		Arguments: map[string]any{
			"card_id": "ci-1",
			"text":    "preview",
			"dry_run": true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[service.EditorResult]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if out.Result == nil {
		t.Fatal("editor tools must populate Result on dry-run too — the diff IS the value")
	}
	if !strings.Contains(out.Result.New, "preview") {
		t.Errorf("out.Result.New does not contain %q", "preview")
	}
	if !strings.Contains(out.Result.UnifiedDiff, "+preview") {
		t.Errorf("out.Result.UnifiedDiff does not contain %q", "+preview")
	}
	if out.WouldCall == nil {
		t.Fatal("out.WouldCall is nil")
	}
	if got := out.WouldCall.Method; got != http.MethodPut {
		t.Errorf("out.WouldCall.Method = %v, want %v", got, http.MethodPut)
	}
	if got := puts.Load(); got != 0 {
		t.Errorf("puts.Load() = %v, want %v", got, 0)
	}
}

func TestMCP_AppendCardDescription_MissingCardID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, appendCardDescriptionToolName, "card_id")
}

func TestMCP_PrependCardDescription_HappyPath(t *testing.T) {
	t.Parallel()

	c := descriptionEditorFixture(t, "body", "markdown")

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      prependCardDescriptionToolName,
		Arguments: map[string]any{"card_id": "ci-1", "text": "prepended"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[service.EditorResult]](t, res)
	if got := out.Result.New; got != "prepended\n\nbody" {
		t.Errorf("out.Result.New = %v, want %v", got, "prepended\n\nbody")
	}
}

func TestMCP_ReplaceInCardDescription_HappyPath(t *testing.T) {
	t.Parallel()

	c := descriptionEditorFixture(t, "find this and find this", "markdown")

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: replaceInCardDescriptionToolName,
		Arguments: map[string]any{
			"card_id": "ci-1",
			"find":    "find this",
			"replace": "FOUND",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[service.EditorResult]](t, res)
	if got := out.Result.New; got != "FOUND and find this" {
		t.Errorf("default count: 1 must replace only the first match: got %v, want %v", got, "FOUND and find this")
	}
}

// TestMCP_ReplaceInCardDescription_NoMatch_RefusesPUT pins the
// safety net: a missing 'find' aborts with an error rather than
// PUT-ing an unchanged body — the LLM should learn the typo
// instead of silently no-op-ing.
func TestMCP_ReplaceInCardDescription_NoMatch_RefusesPUT(t *testing.T) {
	t.Parallel()

	var puts atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.Card{
				CardID:              "ci-1",
				DetailedDescription: "no token here",
			})
		case http.MethodPut:
			puts.Add(1)
		}
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: replaceInCardDescriptionToolName,
		Arguments: map[string]any{
			"card_id": "ci-1",
			"find":    "missing-token",
			"replace": "x",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("no-match must surface as a tool error")
	}
	if got := puts.Load(); got != 0 {
		t.Errorf("no PUT must be issued when find matched nothing: got %v, want %v", got, 0)
	}
}

func TestMCP_ReplaceInCardDescription_RegexBackref(t *testing.T) {
	t.Parallel()

	c := descriptionEditorFixture(t, "alpha beta", "markdown")

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: replaceInCardDescriptionToolName,
		Arguments: map[string]any{
			"card_id":   "ci-1",
			"find":      `(\w+) (\w+)`,
			"replace":   "$2 $1",
			"use_regex": true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[service.EditorResult]](t, res)
	if got := out.Result.New; got != "beta alpha" {
		t.Errorf("out.Result.New = %v, want %v", got, "beta alpha")
	}
}
