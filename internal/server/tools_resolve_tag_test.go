package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ResolveTag_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
			Page:  0,
			Pages: 1,
			Entities: []favro.Tag{
				{TagID: "t-1", Name: "frontend", Color: "blue"},
				{TagID: "t-2", Name: "backend"},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: resolveTagToolName,
		Arguments: map[string]any{
			"name": "front",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[resolveTagOutput](t, res)
	require.Len(t, out.Candidates, 1)
	require.Equal(t, "t-1", out.Candidates[0].TagID)
	require.Equal(t, "frontend", out.Candidates[0].Name)
	require.Equal(t, "blue", out.Candidates[0].Color)
	require.InDelta(t, 0.7, out.Candidates[0].Score, 0.001)
	require.False(t, out.Cached, "first call must report uncached")
}

func TestMCP_ResolveTag_MissingName_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, resolveTagToolName, "name")
}

func TestMCP_ResolveTag_NoMatchReturnsEmptyList(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
			Page:     0,
			Pages:    1,
			Entities: []favro.Tag{{TagID: "t-1", Name: "frontend"}},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: resolveTagToolName,
		Arguments: map[string]any{
			"name": "nomatch",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "no match must NOT surface as a tool error — empty candidate list is the contract")

	out := decodeStructured[resolveTagOutput](t, res)
	require.Empty(t, out.Candidates)
}
