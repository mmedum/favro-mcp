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

func TestMCP_AddCommentToCard_ByCardCommonID(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST; got %s", r.Method)
		}
		if r.URL.Path != "/comments" {
			t.Errorf("expected /comments; got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commentId":"cm-1","cardCommonId":"cc-1","userId":"u-1","comment":"hi"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addCommentToCardToolName,
		Arguments: map[string]any{
			"card_common_id": "cc-1",
			"comment":        "hi",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[addCommentToCardResult]](t, res)
	require.NotNil(t, out.Result.Comment)
	require.Equal(t, "cm-1", out.Result.Comment.CommentID)
	require.False(t, out.Result.Ambiguous)
}

func TestMCP_AddCommentToCard_ByCardID_ResolvesCommonID(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.Card{
				CardID:       "ci-1",
				CardCommonID: "cc-resolved",
			})
		case http.MethodPost:
			if r.URL.Path != "/comments" {
				t.Errorf("expected /comments; got %s", r.URL.Path)
			}
			body := decodeBody[favro.CreateCommentRequest](t, r)
			if body.CardCommonID != "cc-resolved" {
				t.Errorf("tool must resolve card_id → cardCommonId; got %q", body.CardCommonID)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"commentId":"cm-2","cardCommonId":"cc-resolved","userId":"u-1","comment":"x"}`))
		}
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addCommentToCardToolName,
		Arguments: map[string]any{
			"card_id": "ci-1",
			"comment": "x",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestMCP_AddCommentToCard_NoIdentity(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addCommentToCardToolName,
		Arguments: map[string]any{
			"comment": "no identity",
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
}

func TestMCP_AddCommentToCard_SearchQuery_RequiresScope(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addCommentToCardToolName,
		Arguments: map[string]any{
			"search_query": "anything",
			"comment":      "x",
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
}

func TestMCP_AddCommentToCard_SearchQuery_NoMatch(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /cards listing returns empty
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Card]{Pages: 1})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addCommentToCardToolName,
		Arguments: map[string]any{
			"search_query":     "nothing-to-find",
			"widget_common_id": "w-1",
			"comment":          "x",
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
}

// TestMCP_AddCommentToCard_SearchQuery_Ambiguous pins the
// non-error response shape: when top-2 hits are within the
// ambiguity margin, return Ambiguous=true with both candidates and
// do NOT post a comment.
func TestMCP_AddCommentToCard_SearchQuery_Ambiguous(t *testing.T) {
	t.Parallel()

	var posts atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Card]{
				Pages: 1,
				Entities: []favro.Card{
					{CardID: "ci-a", CardCommonID: "cc-a", Name: "alpha card"},
					{CardID: "ci-b", CardCommonID: "cc-b", Name: "alpha card"},
				},
			})
		case http.MethodPost:
			posts.Add(1)
		}
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addCommentToCardToolName,
		Arguments: map[string]any{
			"search_query":     "alpha card",
			"widget_common_id": "w-1",
			"comment":          "x",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[addCommentToCardResult]](t, res)
	require.True(t, out.Result.Ambiguous)
	require.Len(t, out.Result.Candidates, 2)
	require.Nil(t, out.Result.Comment)
	require.EqualValues(t, 0, posts.Load(), "ambiguous match must NOT post a comment")
}

func TestMCP_AddCommentToCard_MissingComment(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, addCommentToCardToolName, "comment")
}

// decodeBody is a small helper for test fixtures that need to inspect
// the JSON body of an incoming PUT/POST.
func decodeBody[T any](t *testing.T, r *http.Request) T {
	t.Helper()
	var out T
	require.NoError(t, json.NewDecoder(r.Body).Decode(&out))
	return out
}
