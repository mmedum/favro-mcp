package tools

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListWidgets_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Widget]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-w",
			Entities: []favro.Widget{
				{WidgetCommonID: "w-1", Name: "Sprint Board", Type: "board"},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listWidgetsToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[listOutput[favro.Widget]](t, res)
	if len(out.Items) != 1 {
		t.Fatalf("len(out.Items) = %d, want 1", len(out.Items))
	}
	if got := out.Items[0].Name; got != "Sprint Board" {
		t.Errorf("out.Items[0].Name = %v, want %v", got, "Sprint Board")
	}
	if got := out.Items[0].Type; got != "board" {
		t.Errorf("out.Items[0].Type = %v, want %v", got, "board")
	}
	if out.NextPage == nil {
		t.Fatal("out.NextPage is nil")
	}
	if got := *out.NextPage; got != 2 {
		t.Errorf("*out.NextPage = %v, want %v", got, 2)
	}
}

func TestMCP_ListWidgets_FiltersByCollection(t *testing.T) {
	t.Parallel()

	var sawCollection string
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawCollection = r.URL.Query().Get("collectionId")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Widget]{Page: 0, Pages: 1})
	}))

	cs := connectInMemoryWith(t, c)
	_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: listWidgetsToolName,
		Arguments: map[string]any{
			"collection_id": "c-xyz",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := sawCollection; got != "c-xyz" {
		t.Errorf("collection_id input must reach Favro as ?collectionId=: got %v, want %v", got, "c-xyz")
	}
}

func TestMCP_GetWidget_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/widgets/w-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Widget{
			WidgetCommonID: "w-zzz",
			Name:           "Looked Up",
			Type:           "board",
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getWidgetToolName,
		Arguments: map[string]any{
			"widget_common_id": "w-zzz",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[favro.Widget](t, res)
	if got := out.WidgetCommonID; got != "w-zzz" {
		t.Errorf("out.WidgetCommonID = %v, want %v", got, "w-zzz")
	}
	if got := out.Type; got != "board" {
		t.Errorf("out.Type = %v, want %v", got, "board")
	}
}

func TestMCP_GetWidget_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, getWidgetToolName, "widget_common_id")
}
