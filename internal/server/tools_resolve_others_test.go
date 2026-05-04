package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// One MCP-layer happy-path test per resolver — the rest of the
// behavior (cache mechanics, score scale, tie-break, force_refresh,
// limit cap) is locked in at the resolver method layer in
// resolver_test.go and shared via the listAllCached helper, so
// re-testing it through the MCP wrapper would be redundant.

func TestMCP_ResolveUser_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.User]{
			Page:     0,
			Pages:    1,
			Entities: []favro.User{{UserID: "u-1", Name: "Alice", Email: "alice@example.invalid"}},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      resolveUserToolName,
		Arguments: map[string]any{"name": "alic"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[resolveOutput[ResolvedUser]](t, res)
	require.Len(t, out.Candidates, 1)
	require.Equal(t, "u-1", out.Candidates[0].UserID)
	require.Equal(t, "alice@example.invalid", out.Candidates[0].Email)
}

// TestMCP_ResolveTools_MissingRequiredField pins the schema-layer
// rejection contract for every resolver's required field in one
// table. The 7 resolvers each fail differently from the LLM's
// perspective (different field names) but identically at the SDK
// schema layer; this test makes the contract failure visible at
// any one of them an unmissable break in the table.
func TestMCP_ResolveTools_MissingRequiredField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tool, field string
	}{
		{resolveTagToolName, "name"},
		{resolveUserToolName, "name"},
		{resolveCollectionToolName, "name"},
		{resolveWidgetToolName, "name"},
		{resolveColumnToolName, "widget_common_id"},
		{resolveCustomFieldToolName, "name"},
		{resolveGroupToolName, "name"},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			t.Parallel()
			assertMissingRequiredFieldFails(t, tc.tool, tc.field)
		})
	}
}

func TestMCP_ResolveCollection_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Collection]{
			Page:     0,
			Pages:    1,
			Entities: []favro.Collection{{CollectionID: "c-1", Name: "Documentation"}},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      resolveCollectionToolName,
		Arguments: map[string]any{"name": "doc"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[resolveOutput[ResolvedCollection]](t, res)
	require.Len(t, out.Candidates, 1)
	require.Equal(t, "c-1", out.Candidates[0].CollectionID)
}

func TestMCP_ResolveWidget_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Widget]{
			Page:     0,
			Pages:    1,
			Entities: []favro.Widget{{WidgetCommonID: "w-1", Name: "Sprint Board", Type: "board", CollectionIDs: []string{"c-1"}}},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      resolveWidgetToolName,
		Arguments: map[string]any{"name": "sprint"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[resolveOutput[ResolvedWidget]](t, res)
	require.Len(t, out.Candidates, 1)
	require.Equal(t, "w-1", out.Candidates[0].WidgetCommonID)
	require.Equal(t, "board", out.Candidates[0].Type)
}

func TestMCP_ResolveColumn_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the widget filter rode through to Favro.
		if got := r.URL.Query().Get("widgetCommonId"); got != "w-1" {
			t.Errorf("widget filter not forwarded: got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Column]{
			Page:     0,
			Pages:    1,
			Entities: []favro.Column{{ColumnID: "col-1", WidgetCommonID: "w-1", Name: "Doing", Position: 1}},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: resolveColumnToolName,
		Arguments: map[string]any{
			"widget_common_id": "w-1",
			"name":             "doing",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[resolveOutput[ResolvedColumn]](t, res)
	require.Len(t, out.Candidates, 1)
	require.Equal(t, "col-1", out.Candidates[0].ColumnID)
	require.Equal(t, 1, out.Candidates[0].Position)
}

func TestMCP_ResolveCustomField_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.CustomField]{
			Page:     0,
			Pages:    1,
			Entities: []favro.CustomField{{CustomFieldID: "cf-1", Name: "Priority", Type: "Single select"}},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      resolveCustomFieldToolName,
		Arguments: map[string]any{"name": "prior"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[resolveOutput[ResolvedCustomField]](t, res)
	require.Len(t, out.Candidates, 1)
	require.Equal(t, "cf-1", out.Candidates[0].CustomFieldID)
	require.Equal(t, "Single select", out.Candidates[0].Type)
}

func TestMCP_ResolveGroup_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Group]{
			Page:     0,
			Pages:    1,
			Entities: []favro.Group{{GroupID: "g-1", Name: "Engineering"}},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      resolveGroupToolName,
		Arguments: map[string]any{"name": "eng"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[resolveOutput[ResolvedGroup]](t, res)
	require.Len(t, out.Candidates, 1)
	require.Equal(t, "g-1", out.Candidates[0].GroupID)
}
