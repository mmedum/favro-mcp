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

// descriptionEditorFixture wires a server that:
//   - GET /cards/{id}?descriptionFormat=markdown → returns the
//     supplied body
//   - PUT /cards/{id} → echoes a Card back
//
// Used by every test that exercises the three description editors.
func descriptionEditorFixture(t *testing.T, body string, requireDescFormat string) *favro.Client {
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
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[editorResult]](t, res)
	require.False(t, out.DryRun)
	require.NotNil(t, out.Result)
	require.Equal(t, "# heading\n\nbody", out.Result.Old)
	require.Equal(t, "# heading\n\nbody\n\nappended", out.Result.New)
	require.Contains(t, out.Result.UnifiedDiff, "+appended")
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
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[editorResult]](t, res)
	require.True(t, out.DryRun)
	require.NotNil(t, out.Result, "editor tools must populate Result on dry-run too — the diff IS the value")
	require.Contains(t, out.Result.New, "preview")
	require.Contains(t, out.Result.UnifiedDiff, "+preview")
	require.NotNil(t, out.WouldCall)
	require.Equal(t, http.MethodPut, out.WouldCall.Method)
	require.EqualValues(t, 0, puts.Load())
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
	require.NoError(t, err)

	out := decodeStructured[writeOutput[editorResult]](t, res)
	require.Equal(t, "prepended\n\nbody", out.Result.New)
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
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[editorResult]](t, res)
	require.Equal(t, "FOUND and find this", out.Result.New,
		"default count: 1 must replace only the first match")
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
	require.NoError(t, err)
	require.True(t, res.IsError, "no-match must surface as a tool error")
	require.EqualValues(t, 0, puts.Load(), "no PUT must be issued when find matched nothing")
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
	require.NoError(t, err)

	out := decodeStructured[writeOutput[editorResult]](t, res)
	require.Equal(t, "beta alpha", out.Result.New)
}
