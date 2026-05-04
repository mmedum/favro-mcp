package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListCustomFields_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.CustomField]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-cf",
			Entities: []favro.CustomField{
				{
					CustomFieldID: "cf-select",
					Type:          "Single select",
					Name:          "QA",
					Enabled:       true,
					CustomFieldItems: []favro.CustomFieldItem{
						{CustomFieldItemID: "i-1", Name: "ready", Color: "green"},
					},
				},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listCustomFieldsToolName,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[listOutput[favro.CustomField]](t, res)
	require.Len(t, out.Items, 1)
	require.Equal(t, "Single select", out.Items[0].Type)
	require.Equal(t, "QA", out.Items[0].Name)
	require.Len(t, out.Items[0].CustomFieldItems, 1)
	require.Equal(t, "ready", out.Items[0].CustomFieldItems[0].Name)
	require.NotNil(t, out.NextPage)
	require.Equal(t, 1, *out.NextPage)
}

func TestMCP_GetCustomField_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/customfields/cf-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.CustomField{
			CustomFieldID: "cf-zzz",
			Type:          "Text",
			Name:          "looked up",
			Enabled:       true,
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getCustomFieldToolName,
		Arguments: map[string]any{
			"custom_field_id": "cf-zzz",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[favro.CustomField](t, res)
	require.Equal(t, "cf-zzz", out.CustomFieldID)
	require.Equal(t, "Text", out.Type)
	require.Empty(t, out.CustomFieldItems, "primitive types must NOT carry items")
}

func TestMCP_GetCustomField_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, getCustomFieldToolName, "custom_field_id")
}
