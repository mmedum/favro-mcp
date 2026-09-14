package tools

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[addCommentToCardResult]](t, res)
	if out.Result.Comment == nil {
		t.Fatal("out.Result.Comment is nil")
	}
	if got := out.Result.Comment.CommentID; got != "cm-1" {
		t.Errorf("out.Result.Comment.CommentID = %v, want %v", got, "cm-1")
	}
	if out.Result.Ambiguous {
		t.Error("out.Result.Ambiguous = true, want false")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("res.IsError = false, want true")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("res.IsError = false, want true")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("res.IsError = false, want true")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[addCommentToCardResult]](t, res)
	if !out.Result.Ambiguous {
		t.Error("out.Result.Ambiguous = false, want true")
	}
	if len(out.Result.Candidates) != 2 {
		t.Fatalf("len(out.Result.Candidates) = %d, want 2", len(out.Result.Candidates))
	}
	if out.Result.Comment != nil {
		t.Errorf("out.Result.Comment = %v, want nil", out.Result.Comment)
	}
	if got := posts.Load(); got != 0 {
		t.Errorf("ambiguous match must NOT post a comment: got %v, want %v", got, 0)
	}
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
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		t.Fatalf("json.NewDecoder(r.Body).Decode(&out): %v", err)
	}
	return out
}
