package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

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
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[listOutput[favro.Comment]](t, res)
	require.Len(t, out.Items, 1)
	require.Equal(t, "hello", out.Items[0].Body)
	require.NotNil(t, out.NextPage)
	require.Equal(t, 1, *out.NextPage)
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
	require.NoError(t, err)
	require.Equal(t, "card-c-xyz", sawCard,
		"card_common_id input must reach Favro as ?cardCommonId=")
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
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[favro.Comment](t, res)
	require.Equal(t, "cm-zzz", out.CommentID)
	require.Equal(t, "looked up", out.Body)
}

func TestMCP_GetComment_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, getCommentToolName, "comment_id")
}
