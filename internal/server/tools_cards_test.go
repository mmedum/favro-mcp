package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListCards_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Card]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-cards",
			Entities: []favro.Card{
				{CardCommonID: "card-c-1", Name: "Print visitor passes", SequentialID: 42},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listCardsToolName,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[listOutput[favro.Card]](t, res)
	require.Len(t, out.Items, 1)
	require.Equal(t, "Print visitor passes", out.Items[0].Name)
	require.Equal(t, 42, out.Items[0].SequentialID)
	require.NotNil(t, out.NextPage)
	require.Equal(t, 1, *out.NextPage)
}

func TestMCP_ListCards_FiltersForwarded(t *testing.T) {
	t.Parallel()

	var saw struct {
		widget, collection, common, seq, unique string
	}
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw.widget = r.URL.Query().Get("widgetCommonId")
		saw.collection = r.URL.Query().Get("collectionId")
		saw.common = r.URL.Query().Get("cardCommonId")
		saw.seq = r.URL.Query().Get("cardSequentialId")
		saw.unique = r.URL.Query().Get("unique")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Card]{Page: 0, Pages: 1})
	}))

	cs := connectInMemoryWith(t, c)
	_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: listCardsToolName,
		Arguments: map[string]any{
			"widget_common_id": "w-1",
			"collection_id":    "col-1",
			"card_common_id":   "card-c-7",
			"sequential_id":    123,
			"unique":           true,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "w-1", saw.widget)
	require.Equal(t, "col-1", saw.collection)
	require.Equal(t, "card-c-7", saw.common)
	require.Equal(t, "123", saw.seq)
	require.Equal(t, "true", saw.unique)
}

func TestMCP_GetCard_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cards/card-i-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Card{
			CardID:       "card-i-zzz",
			CardCommonID: "card-c-zzz",
			Name:         "Looked Up",
			SequentialID: 99,
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getCardToolName,
		Arguments: map[string]any{
			"card_id": "card-i-zzz",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[favro.Card](t, res)
	require.Equal(t, "card-i-zzz", out.CardID)
	require.Equal(t, "card-c-zzz", out.CardCommonID)
	require.Equal(t, "Looked Up", out.Name)
	require.Equal(t, 99, out.SequentialID)
}

func TestMCP_GetCard_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertGetMissingIDFails(t, getCardToolName, "card_id")
}
