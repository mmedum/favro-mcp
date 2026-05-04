package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListTags_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-tags",
			Entities: []favro.Tag{
				{TagID: "t-1", Name: "blocker", Color: "red"},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listTagsToolName,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[listOutput[favro.Tag]](t, res)
	require.Len(t, out.Items, 1)
	require.Equal(t, "blocker", out.Items[0].Name)
	require.Equal(t, "red", out.Items[0].Color)
	require.NotNil(t, out.NextPage)
	require.Equal(t, 1, *out.NextPage)
}

func TestMCP_GetTag_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tags/t-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Tag{
			TagID: "t-zzz",
			Name:  "looked up",
			Color: "lime",
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getTagToolName,
		Arguments: map[string]any{
			"tag_id": "t-zzz",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[favro.Tag](t, res)
	require.Equal(t, "t-zzz", out.TagID)
	require.Equal(t, "looked up", out.Name)
	require.Equal(t, "lime", out.Color)
}

func TestMCP_GetTag_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, getTagToolName, "tag_id")
}
