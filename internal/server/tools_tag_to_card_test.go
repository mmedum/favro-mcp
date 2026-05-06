package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// tagFixture wires a server that:
//   - GET /tags → returns the supplied tag list
//   - PUT /cards/{id} → echoes a Card back
//
// Used by every test that exercises favro_add_tag_to_card and
// favro_remove_tag_from_card.
func tagFixture(t *testing.T, tags []favro.Tag) *favro.Client {
	t.Helper()
	return favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
				Pages:    1,
				Entities: tags,
			})
		case http.MethodPut:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cardId":"ci-1","cardCommonId":"cc-1","name":"x"}`))
		}
	}))
}

func TestMCP_AddTagToCard_HappyPath(t *testing.T) {
	t.Parallel()

	c := tagFixture(t, []favro.Tag{{TagID: "t-1", Name: "Frontend"}})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addTagToCardToolName,
		Arguments: map[string]any{
			"card_id":  "ci-1",
			"tag_name": "Frontend",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestMCP_AddTagToCard_CaseInsensitive(t *testing.T) {
	t.Parallel()

	c := tagFixture(t, []favro.Tag{{TagID: "t-1", Name: "Frontend"}})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addTagToCardToolName,
		Arguments: map[string]any{
			"card_id":  "ci-1",
			"tag_name": "frontend",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

// TestMCP_AddTagToCard_HardFailUnknown pins the typo-prevention
// contract: an unknown name surfaces as an error pointing at
// favro_create_tag, never as an auto-create or silent success.
func TestMCP_AddTagToCard_HardFailUnknown(t *testing.T) {
	t.Parallel()

	var puts atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
				Pages:    1,
				Entities: []favro.Tag{{TagID: "t-1", Name: "Frontend"}},
			})
		case http.MethodPut:
			puts.Add(1)
		}
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addTagToCardToolName,
		Arguments: map[string]any{
			"card_id":  "ci-1",
			"tag_name": "Frontned",
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError, "unknown tag name must surface as a tool error")
	require.Contains(t, strings.ToLower(serializedResponseString(t, res)), "favro_create_tag",
		"error must point the LLM at favro_create_tag explicitly")
	require.EqualValues(t, 0, puts.Load(), "no PUT must be issued for an unknown tag")
}

func TestMCP_AddTagToCard_AmbiguousNames(t *testing.T) {
	t.Parallel()

	c := tagFixture(t, []favro.Tag{
		{TagID: "t-1", Name: "Frontend"},
		{TagID: "t-2", Name: "Frontend"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addTagToCardToolName,
		Arguments: map[string]any{
			"card_id":  "ci-1",
			"tag_name": "Frontend",
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, strings.ToLower(serializedResponseString(t, res)), "multiple")
}

func TestMCP_AddTagToCard_DryRun(t *testing.T) {
	t.Parallel()

	var puts atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
				Pages:    1,
				Entities: []favro.Tag{{TagID: "t-1", Name: "Frontend"}},
			})
		case http.MethodPut:
			puts.Add(1)
		}
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addTagToCardToolName,
		Arguments: map[string]any{
			"card_id":  "ci-1",
			"tag_name": "Frontend",
			"dry_run":  true,
		},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	require.True(t, out.DryRun)
	require.EqualValues(t, 0, puts.Load())
}

func TestMCP_RemoveTagFromCard_HappyPath(t *testing.T) {
	t.Parallel()

	c := tagFixture(t, []favro.Tag{{TagID: "t-1", Name: "Frontend"}})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: removeTagFromCardToolName,
		Arguments: map[string]any{
			"card_id":  "ci-1",
			"tag_name": "Frontend",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestMCP_AddTagToCard_MissingFields(t *testing.T) {
	t.Parallel()

	cases := []string{"card_id", "tag_name"}
	for _, field := range cases {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			assertMissingRequiredFieldFails(t, addTagToCardToolName, field)
		})
	}
}
