package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListCollections_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Collection]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-c",
			Entities: []favro.Collection{
				{CollectionID: "c-1", Name: "Engineering", Color: "blue"},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listCollectionsToolName,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[listOutput[favro.Collection]](t, res)
	require.Len(t, out.Items, 1)
	require.Equal(t, "Engineering", out.Items[0].Name)
	require.NotNil(t, out.NextPage)
	require.Equal(t, 1, *out.NextPage)
	require.Equal(t, "req-c", out.RequestID)
}

func TestMCP_GetCollection_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/collections/c-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Collection{
			CollectionID: "c-zzz",
			Name:         "Looked Up",
			Archived:     true,
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getCollectionToolName,
		Arguments: map[string]any{
			"collection_id": "c-zzz",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[favro.Collection](t, res)
	require.Equal(t, "c-zzz", out.CollectionID)
	require.True(t, out.Archived)
}

func TestMCP_GetCollection_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertGetMissingIDFails(t, getCollectionToolName, "collection_id")
}
