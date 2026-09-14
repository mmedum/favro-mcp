package tools

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListColumns_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Column]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-c",
			Entities: []favro.Column{
				{ColumnID: "col-1", WidgetCommonID: "w-1", Name: "Doing", Position: 1, CardCount: 4},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: listColumnsToolName,
		Arguments: map[string]any{
			"widget_common_id": "w-1",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[listOutput[favro.Column]](t, res)
	if len(out.Items) != 1 {
		t.Fatalf("len(out.Items) = %d, want 1", len(out.Items))
	}
	if got := out.Items[0].Name; got != "Doing" {
		t.Errorf("out.Items[0].Name = %v, want %v", got, "Doing")
	}
	if got := out.Items[0].Position; got != 1 {
		t.Errorf("out.Items[0].Position = %v, want %v", got, 1)
	}
	if out.NextPage == nil {
		t.Fatal("out.NextPage is nil")
	}
	if got := *out.NextPage; got != 2 {
		t.Errorf("*out.NextPage = %v, want %v", got, 2)
	}
}

func TestMCP_ListColumns_MissingWidget_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, listColumnsToolName, "widget_common_id")
}

// TestMCP_ListColumns_SortsByPosition pins the human-friendly contract:
// items must be returned in left-to-right board order regardless of the
// order Favro itself returns. Live observation showed Favro orders by
// columnId, which is meaningless to humans and to LLMs summarizing the
// board.
func TestMCP_ListColumns_SortsByPosition(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Column]{
			Page:  0,
			Pages: 1,
			Entities: []favro.Column{
				{ColumnID: "col-c", WidgetCommonID: "w-1", Name: "Done", Position: 3},
				{ColumnID: "col-a", WidgetCommonID: "w-1", Name: "Doing", Position: 1},
				{ColumnID: "col-d", WidgetCommonID: "w-1", Name: "Backlog", Position: 0},
				{ColumnID: "col-b", WidgetCommonID: "w-1", Name: "Review", Position: 2},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: listColumnsToolName,
		Arguments: map[string]any{
			"widget_common_id": "w-1",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[listOutput[favro.Column]](t, res)
	names := make([]string, len(out.Items))
	for i, it := range out.Items {
		names[i] = it.Name
	}
	if got := names; !reflect.DeepEqual(got, ([]string{"Backlog", "Doing", "Review", "Done"})) {
		t.Errorf("items must be sorted by position ascending, not by Favro's default columnId order: got %v, want %v", got, []string{"Backlog", "Doing", "Review", "Done"})
	}
}

func TestMCP_ListColumns_FiltersByWidget(t *testing.T) {
	t.Parallel()

	var sawWidget string
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawWidget = r.URL.Query().Get("widgetCommonId")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Column]{Page: 0, Pages: 1})
	}))

	cs := connectInMemoryWith(t, c)
	_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: listColumnsToolName,
		Arguments: map[string]any{
			"widget_common_id": "w-xyz",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := sawWidget; got != "w-xyz" {
		t.Errorf("widget_common_id input must reach Favro as ?widgetCommonId=: got %v, want %v", got, "w-xyz")
	}
}

func TestMCP_GetColumn_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/columns/col-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Column{
			ColumnID:       "col-zzz",
			WidgetCommonID: "w-1",
			Name:           "Done",
			Position:       4,
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getColumnToolName,
		Arguments: map[string]any{
			"column_id": "col-zzz",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[favro.Column](t, res)
	if got := out.ColumnID; got != "col-zzz" {
		t.Errorf("out.ColumnID = %v, want %v", got, "col-zzz")
	}
	if got := out.Name; got != "Done" {
		t.Errorf("out.Name = %v, want %v", got, "Done")
	}
	if got := out.Position; got != 4 {
		t.Errorf("out.Position = %v, want %v", got, 4)
	}
}

func TestMCP_GetColumn_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, getColumnToolName, "column_id")
}
