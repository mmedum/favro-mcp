package tools

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const mcpDependenciesFixture = `{
	"cardId":"ci-1",
	"cardCommonId":"cc-1",
	"dependencies":[{"cardId":"ci-2","cardCommonId":"cc-2","isBefore":true,"reverseCardId":"ci-1"}]
}`

func TestMCP_ListDependencies_HappyPath(t *testing.T) {
	t.Parallel()

	c := staticJSONFixture(t, nil, mcpDependenciesFixture)

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listDependenciesToolName,
		Arguments: map[string]any{"card_id": "ci-1"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}

	out := decodeStructured[favro.CardDependencies](t, res)
	if len(out.Dependencies) != 1 {
		t.Fatalf("len(out.Dependencies) = %d, want 1", len(out.Dependencies))
	}
	if got := out.Dependencies[0].CardID; got != "ci-2" {
		t.Errorf("out.Dependencies[0].CardID = %v, want %v", got, "ci-2")
	}
	if !out.Dependencies[0].IsBefore {
		t.Error("out.Dependencies[0].IsBefore = false, want true")
	}
}

// Add and replace hit the same URL and differ only in HTTP method,
// so pin the mapping: swapping them would silently wipe a card's
// existing dependencies.
func TestMCP_Dependencies_AddVsReplaceMethod(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		tool       string
		wantMethod string
	}{
		{addDependenciesToolName, http.MethodPost},
		{replaceDependenciesToolName, http.MethodPut},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			t.Parallel()

			var method string
			c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method = r.Method
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(mcpDependenciesFixture))
			}))

			cs := connectInMemoryWith(t, c)
			res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
				Name: tc.tool,
				Arguments: map[string]any{
					"card_id": "ci-1",
					"dependencies": []map[string]any{
						{"cardId": "ci-2", "isBefore": true},
					},
				},
			})
			if err := err; err != nil {
				t.Fatalf("err: %v", err)
			}
			if res.IsError {
				t.Errorf("tool error: %s", serializedResponseString(t, res))
			}
			if got := method; got != tc.wantMethod {
				t.Errorf("method = %v, want %v", got, tc.wantMethod)
			}
		})
	}
}

// Replacing and clearing dependencies are annotated destructive so
// MCP hosts can warn; adding and re-pointing one are not.
func TestMCP_Dependencies_DestructiveAnnotations(t *testing.T) {
	t.Parallel()

	cs := connectInMemory(t)
	res, err := cs.ListTools(t.Context(), nil)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	want := map[string]bool{
		listDependenciesToolName:      false,
		addDependenciesToolName:       false,
		updateDependencyToolName:      false,
		replaceDependenciesToolName:   true,
		deleteDependencyToolName:      true,
		deleteAllDependenciesToolName: true,
	}
	seen := 0
	for _, tool := range res.Tools {
		wantDestructive, ok := want[tool.Name]
		if !ok {
			continue
		}
		seen++
		if tool.Name == listDependenciesToolName {
			if !tool.Annotations.ReadOnlyHint {
				t.Errorf("%s must be read-only", tool.Name)
			}
			continue
		}
		if tool.Annotations.DestructiveHint == nil {
			t.Fatalf("%s must set DestructiveHint explicitly", tool.Name)
		}
		if got := *tool.Annotations.DestructiveHint; got != wantDestructive {
			t.Errorf("%s: got %v, want %v", tool.Name, got, wantDestructive)
		}
	}
	if len(want) != seen {
		t.Fatalf("every dependency tool must be advertised: got %d", len(want))
	}
}

func TestMCP_DependencyWrites_DryRun_NeverDispatch(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))
	cs := connectInMemoryWith(t, c)

	deps := []map[string]any{{"cardId": "ci-2", "isBefore": true}}
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{addDependenciesToolName, map[string]any{"card_id": "ci-1", "dependencies": deps, "dry_run": true}},
		{replaceDependenciesToolName, map[string]any{"card_id": "ci-1", "dependencies": deps, "dry_run": true}},
		{updateDependencyToolName, map[string]any{"card_id": "ci-1", "dependency_card_id": "ci-2", "is_before": false, "dry_run": true}},
		{deleteDependencyToolName, map[string]any{"card_id": "ci-1", "dependency_card_id": "ci-2", "dry_run": true}},
		{deleteAllDependenciesToolName, map[string]any{"card_id": "ci-1", "dry_run": true}},
	} {
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
		if err := err; err != nil {
			t.Fatalf("err: %v", err)
		}
		if res.IsError {
			t.Errorf("%s: %s", tc.tool, serializedResponseString(t, res))
		}
	}

	if calls.Load() != 0 {
		t.Errorf("calls.Load() = %v, want 0", calls.Load())
	}
}

func TestMCP_ListCardActivities_HappyPath(t *testing.T) {
	t.Parallel()

	c := staticJSONFixture(t, nil, `{"page":0,"pages":1,"entities":[
		{"type":"assigned","source":"follow","cardId":"ci-1","byUserId":"u-1","time":"2026-01-15T06:27:12.466Z"}
	]}`)

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: listCardActivitiesToolName,
		Arguments: map[string]any{
			"card_id": "ci-1",
			"since":   "2026-01-01T00:00:00Z",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}

	out := decodeStructured[listOutput[favro.Activity]](t, res)
	if len(out.Items) != 1 {
		t.Fatalf("len(out.Items) = %d, want 1", len(out.Items))
	}
	if got := out.Items[0].Type; got != "assigned" {
		t.Errorf("out.Items[0].Type = %v, want %v", got, "assigned")
	}
	if got := out.Items[0].ByUserID; got != "u-1" {
		t.Errorf("out.Items[0].ByUserID = %v, want %v", got, "u-1")
	}
}
