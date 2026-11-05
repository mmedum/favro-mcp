package tools

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
)

func TestMCP_CreateWidget_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("create_widget must POST; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"widgetCommonId":"w-new","name":"Sprint","collectionIds":["c-1"]}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createWidgetToolName,
		Arguments: map[string]any{
			"collection_id": "c-1",
			"name":          "Sprint",
			"type":          "backlog",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[favro.Widget]](t, res)
	if got := out.Result.WidgetCommonID; got != "w-new" {
		t.Errorf("out.Result.WidgetCommonID = %v, want %v", got, "w-new")
	}
}

func TestMCP_CreateWidget_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createWidgetToolName,
		Arguments: map[string]any{
			"collection_id": "c-1",
			"name":          "preview",
			"type":          "board",
			"dry_run":       true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[favro.Widget]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if !strings.Contains(out.PredictedStateDiff, "preview") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "preview")
	}
	if !strings.Contains(out.PredictedStateDiff, "c-1") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "c-1")
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls.Load() = %v, want %v", got, 0)
	}
}

// TestMCP_CreateWidget_TypeIsRequired: the schema keeps type optional,
// so the call itself refuses an empty one, before any request and
// before a dry run returns.
func TestMCP_CreateWidget_TypeIsRequired(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		typ       string
		dryRun    bool
		wantError bool
		wantCalls int32
	}{
		{"no type is refused", "", false, true, 0},
		{"no type is refused on a dry run", "", true, true, 0},
		{"a given type is sent", "board", false, false, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"widgetCommonId":"w-new","name":"Sprint","type":"board","collectionIds":["c-1"]}`))
			}))

			cs := connectInMemoryWith(t, c)
			args := map[string]any{"collection_id": "c-1", "name": "Sprint", "dry_run": tc.dryRun}
			if tc.typ != "" {
				args["type"] = tc.typ
			}
			res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: createWidgetToolName, Arguments: args})
			if err := err; err != nil {
				t.Fatalf("err: %v", err)
			}
			if got := res.IsError; got != tc.wantError {
				t.Fatalf("res.IsError = %v, want %v: %q", got, tc.wantError, resultText(res))
			}
			if tc.wantError {
				const want = "[invalid] favro: widget type is required: pass 'backlog' or 'board'"
				if got := resultText(res); got != want {
					t.Errorf("resultText(res) = %q, want %q", got, want)
				}
			}
			if got := calls.Load(); got != tc.wantCalls {
				t.Errorf("calls.Load() = %v, want %v", got, tc.wantCalls)
			}
		})
	}
}

func TestMCP_CreateWidget_MissingFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		field string
	}{
		{"missing collection_id", "collection_id"},
		{"missing name", "name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertMissingRequiredFieldFails(t, createWidgetToolName, tc.field)
		})
	}
}

func TestMCP_UpdateWidget_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("update_widget must PUT; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"widgetCommonId":"w-1","name":"renamed"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateWidgetToolName,
		Arguments: map[string]any{"widget_common_id": "w-1", "name": "renamed"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[favro.Widget]](t, res)
	if got := out.Result.Name; got != "renamed" {
		t.Errorf("out.Result.Name = %v, want %v", got, "renamed")
	}
}

func TestMCP_UpdateWidget_MissingWidgetCommonID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, updateWidgetToolName, "widget_common_id")
}

func TestMCP_DeleteWidget_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("delete_widget must DELETE; got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      deleteWidgetToolName,
		Arguments: map[string]any{"widget_common_id": "w-1", "everywhere": true, "confirm": true},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[struct{}]](t, res)
	if out.DryRun {
		t.Error("out.DryRun = true, want false")
	}
}

func TestMCP_DeleteWidget_MissingWidgetCommonID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, deleteWidgetToolName, "widget_common_id")
}
