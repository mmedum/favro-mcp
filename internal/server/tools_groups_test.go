package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListGroups_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Group]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-g",
			Entities: []favro.Group{
				{
					GroupID: "g-1",
					Name:    "Engineers",
					Members: []favro.GroupMember{
						{UserID: "u-1", Role: "administrator"},
					},
				},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listGroupsToolName,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[listOutput[favro.Group]](t, res)
	require.Len(t, out.Items, 1)
	require.Equal(t, "Engineers", out.Items[0].Name)
	require.Len(t, out.Items[0].Members, 1)
	require.Equal(t, "u-1", out.Items[0].Members[0].UserID)
	require.Equal(t, "administrator", out.Items[0].Members[0].Role)
	require.NotNil(t, out.NextPage)
	require.Equal(t, 1, *out.NextPage)
}

func TestMCP_GetGroup_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/groups/g-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Group{
			GroupID: "g-zzz",
			Name:    "looked up",
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getGroupToolName,
		Arguments: map[string]any{
			"group_id": "g-zzz",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[favro.Group](t, res)
	require.Equal(t, "g-zzz", out.GroupID)
	require.Equal(t, "looked up", out.Name)
	require.Empty(t, out.Members, "groups without members must NOT carry the field")
}

func TestMCP_GetGroup_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertGetMissingIDFails(t, getGroupToolName, "group_id")
}
