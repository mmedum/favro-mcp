package server

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))

	out := decodeStructured[favro.CardDependencies](t, res)
	require.Len(t, out.Dependencies, 1)
	require.Equal(t, "ci-2", out.Dependencies[0].CardID)
	require.True(t, out.Dependencies[0].IsBefore)
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
			require.NoError(t, err)
			require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
			require.Equal(t, tc.wantMethod, method)
		})
	}
}

// Replacing and clearing dependencies are annotated destructive so
// MCP hosts can warn; adding and re-pointing one are not.
func TestMCP_Dependencies_DestructiveAnnotations(t *testing.T) {
	t.Parallel()

	cs := connectInMemory(t)
	res, err := cs.ListTools(t.Context(), nil)
	require.NoError(t, err)

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
			require.True(t, tool.Annotations.ReadOnlyHint, "%s must be read-only", tool.Name)
			continue
		}
		require.NotNil(t, tool.Annotations.DestructiveHint, "%s must set DestructiveHint explicitly", tool.Name)
		require.Equal(t, wantDestructive, *tool.Annotations.DestructiveHint, "%s", tool.Name)
	}
	require.Len(t, want, seen, "every dependency tool must be advertised")
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
		require.NoError(t, err)
		require.False(t, res.IsError, "%s: %s", tc.tool, serializedResponseString(t, res))
	}

	require.Zero(t, calls.Load(), "dry-run must never reach Favro")
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))

	out := decodeStructured[listOutput[favro.Activity]](t, res)
	require.Len(t, out.Items, 1)
	require.Equal(t, "assigned", out.Items[0].Type)
	require.Equal(t, "u-1", out.Items[0].ByUserID)
}
