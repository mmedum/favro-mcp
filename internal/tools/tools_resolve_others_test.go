package tools

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
	"github.com/mmedum/favro-mcp/internal/service"
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[resolveOutput[service.ResolvedUser]](t, res)
	if len(out.Candidates) != 1 {
		t.Fatalf("len(out.Candidates) = %d, want 1", len(out.Candidates))
	}
	if got := out.Candidates[0].UserID; got != "u-1" {
		t.Errorf("out.Candidates[0].UserID = %v, want %v", got, "u-1")
	}
	if got := out.Candidates[0].Email; got != "alice@example.invalid" {
		t.Errorf("out.Candidates[0].Email = %v, want %v", got, "alice@example.invalid")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[resolveOutput[service.ResolvedCollection]](t, res)
	if len(out.Candidates) != 1 {
		t.Fatalf("len(out.Candidates) = %d, want 1", len(out.Candidates))
	}
	if got := out.Candidates[0].CollectionID; got != "c-1" {
		t.Errorf("out.Candidates[0].CollectionID = %v, want %v", got, "c-1")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[resolveOutput[service.ResolvedWidget]](t, res)
	if len(out.Candidates) != 1 {
		t.Fatalf("len(out.Candidates) = %d, want 1", len(out.Candidates))
	}
	if got := out.Candidates[0].WidgetCommonID; got != "w-1" {
		t.Errorf("out.Candidates[0].WidgetCommonID = %v, want %v", got, "w-1")
	}
	if got := out.Candidates[0].Type; got != "board" {
		t.Errorf("out.Candidates[0].Type = %v, want %v", got, "board")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[resolveOutput[service.ResolvedColumn]](t, res)
	if len(out.Candidates) != 1 {
		t.Fatalf("len(out.Candidates) = %d, want 1", len(out.Candidates))
	}
	if got := out.Candidates[0].ColumnID; got != "col-1" {
		t.Errorf("out.Candidates[0].ColumnID = %v, want %v", got, "col-1")
	}
	if got := out.Candidates[0].Position; got != 1 {
		t.Errorf("out.Candidates[0].Position = %v, want %v", got, 1)
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[resolveOutput[service.ResolvedCustomField]](t, res)
	if len(out.Candidates) != 1 {
		t.Fatalf("len(out.Candidates) = %d, want 1", len(out.Candidates))
	}
	if got := out.Candidates[0].CustomFieldID; got != "cf-1" {
		t.Errorf("out.Candidates[0].CustomFieldID = %v, want %v", got, "cf-1")
	}
	if got := out.Candidates[0].Type; got != "Single select" {
		t.Errorf("out.Candidates[0].Type = %v, want %v", got, "Single select")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[resolveOutput[service.ResolvedGroup]](t, res)
	if len(out.Candidates) != 1 {
		t.Fatalf("len(out.Candidates) = %d, want 1", len(out.Candidates))
	}
	if got := out.Candidates[0].GroupID; got != "g-1" {
		t.Errorf("out.Candidates[0].GroupID = %v, want %v", got, "g-1")
	}
}
