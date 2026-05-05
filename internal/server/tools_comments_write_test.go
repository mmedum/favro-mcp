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

func TestMCP_CreateComment_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("create_comment must POST; got %s", r.Method)
		}
		if r.URL.Path != "/comments" {
			t.Errorf("expected /comments; got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commentId":"cm-1","cardCommonId":"cc-1","userId":"u-1","comment":"hello"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      createCommentToolName,
		Arguments: map[string]any{"card_common_id": "cc-1", "comment": "hello"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Comment]](t, res)
	require.False(t, out.DryRun)
	require.Equal(t, "cm-1", out.Result.CommentID)
}

func TestMCP_CreateComment_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createCommentToolName,
		Arguments: map[string]any{
			"card_common_id": "cc-1",
			"comment":        "hello",
			"dry_run":        true,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Comment]](t, res)
	require.True(t, out.DryRun)
	require.Equal(t, http.MethodPost, out.WouldCall.Method)
	require.Contains(t, out.WouldCall.URL, "/comments")
	require.EqualValues(t, 0, calls.Load())
}

func TestMCP_CreateComment_MissingFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		field string
	}{
		{"missing card_common_id", "card_common_id"},
		{"missing comment", "comment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertMissingRequiredFieldFails(t, createCommentToolName, tc.field)
		})
	}
}

func TestMCP_UpdateComment_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCommentToolName,
		Arguments: map[string]any{
			"comment_id": "cm-1",
			"comment":    "edited",
			"dry_run":    true,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Comment]](t, res)
	require.True(t, out.DryRun)
	require.Equal(t, http.MethodPut, out.WouldCall.Method)
	require.Contains(t, out.WouldCall.URL, "/comments/cm-1")
	require.EqualValues(t, 0, calls.Load())
}

func TestMCP_UpdateComment_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("update_comment must PUT; got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/comments/") {
			t.Errorf("expected /comments/{id}; got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commentId":"cm-1","cardCommonId":"cc-1","userId":"u-1","comment":"edited"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateCommentToolName,
		Arguments: map[string]any{"comment_id": "cm-1", "comment": "edited"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Comment]](t, res)
	require.Equal(t, "edited", out.Result.Body)
}

func TestMCP_DeleteComment_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("delete_comment must DELETE; got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      deleteCommentToolName,
		Arguments: map[string]any{"comment_id": "cm-1"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[struct{}]](t, res)
	require.False(t, out.DryRun)
	require.NotNil(t, out.Result)
}

func TestMCP_DeleteComment_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: deleteCommentToolName,
		Arguments: map[string]any{
			"comment_id": "cm-1",
			"dry_run":    true,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[struct{}]](t, res)
	require.True(t, out.DryRun)
	require.Equal(t, http.MethodDelete, out.WouldCall.Method)
	require.EqualValues(t, 0, calls.Load())
}

func TestMCP_DeleteComment_MissingCommentID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, deleteCommentToolName, "comment_id")
}
