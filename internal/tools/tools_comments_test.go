package tools

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListComments_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Comment]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-cm",
			Entities: []favro.Comment{
				{CommentID: "cm-1", CardCommonID: "card-c-1", UserID: "u-1", Body: "hello"},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: listCommentsToolName,
		Arguments: map[string]any{
			"card_common_id": "card-c-1",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[listOutput[favro.Comment]](t, res)
	if len(out.Items) != 1 {
		t.Fatalf("len(out.Items) = %d, want 1", len(out.Items))
	}
	if got := out.Items[0].Body; got != "hello" {
		t.Errorf("out.Items[0].Body = %v, want %v", got, "hello")
	}
	if out.NextPage == nil {
		t.Fatal("out.NextPage is nil")
	}
	if got := *out.NextPage; got != 2 {
		t.Errorf("*out.NextPage = %v, want %v", got, 2)
	}
}

func TestMCP_ListComments_FilterForwarded(t *testing.T) {
	t.Parallel()

	var sawCard string
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawCard = r.URL.Query().Get("cardCommonId")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Comment]{Page: 0, Pages: 1})
	}))

	cs := connectInMemoryWith(t, c)
	_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: listCommentsToolName,
		Arguments: map[string]any{
			"card_common_id": "card-c-xyz",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := sawCard; got != "card-c-xyz" {
		t.Errorf("card_common_id input must reach Favro as ?cardCommonId=: got %v, want %v", got, "card-c-xyz")
	}
}

func TestMCP_ListComments_MissingCard_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, listCommentsToolName, "card_common_id")
}

func TestMCP_GetComment_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/comments/cm-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Comment{
			CommentID:    "cm-zzz",
			CardCommonID: "card-c-1",
			UserID:       "u-1",
			Body:         "looked up",
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getCommentToolName,
		Arguments: map[string]any{
			"comment_id": "cm-zzz",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[favro.Comment](t, res)
	if got := out.CommentID; got != "cm-zzz" {
		t.Errorf("out.CommentID = %v, want %v", got, "cm-zzz")
	}
	if got := out.Body; got != "looked up" {
		t.Errorf("out.Body = %v, want %v", got, "looked up")
	}
}

func TestMCP_GetComment_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, getCommentToolName, "comment_id")
}
