package server

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListTasks_HappyPath(t *testing.T) {
	t.Parallel()

	c := staticJSONFixture(t, nil, `{"page":0,"pages":1,"entities":[
		{"taskId":"t-1","taskListId":"tl-1","name":"do it","completed":false}
	]}`)

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listTasksToolName,
		Arguments: map[string]any{"card_common_id": "cc-1"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))

	out := decodeStructured[listOutput[favro.Task]](t, res)
	require.Len(t, out.Items, 1)
	require.Equal(t, "do it", out.Items[0].Name)
}

func TestMCP_CreateTasklist_SeedsTasksInOneRequest(t *testing.T) {
	t.Parallel()

	var body string
	c := staticJSONFixture(t, &body, `{"taskListId":"tl-new","name":"Acceptance"}`)

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createTasklistToolName,
		Arguments: map[string]any{
			"card_common_id": "cc-1",
			"name":           "Acceptance",
			"tasks": []map[string]any{
				{"name": "first"},
				{"name": "second", "completed": true},
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))

	require.Contains(t, body, `"tasks":[{"name":"first"},{"name":"second","completed":true}]`)

	out := decodeStructured[writeOutput[favro.Tasklist]](t, res)
	require.False(t, out.DryRun)
	require.Equal(t, "tl-new", out.Result.TaskListID)
}

// Un-ticking an item must reach the wire as completed:false rather
// than being elided into a no-op PUT.
func TestMCP_UpdateTask_UnTick(t *testing.T) {
	t.Parallel()

	var body string
	c := staticJSONFixture(t, &body, `{"taskId":"t-1","completed":false}`)

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateTaskToolName,
		Arguments: map[string]any{
			"task_id":   "t-1",
			"completed": false,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"completed":false`)
}

func TestMCP_ChecklistWrites_DryRun_NeverDispatch(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))
	cs := connectInMemoryWith(t, c)

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{createTaskToolName, map[string]any{"task_list_id": "tl-1", "name": "x", "dry_run": true}},
		{updateTaskToolName, map[string]any{"task_id": "t-1", "name": "x", "dry_run": true}},
		{deleteTaskToolName, map[string]any{"task_id": "t-1", "dry_run": true}},
		{createTasklistToolName, map[string]any{"card_common_id": "cc-1", "name": "x", "dry_run": true}},
		{updateTasklistToolName, map[string]any{"task_list_id": "tl-1", "name": "x", "dry_run": true}},
		{deleteTasklistToolName, map[string]any{"task_list_id": "tl-1", "dry_run": true}},
	} {
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
		require.NoError(t, err)
		require.False(t, res.IsError, "%s: %s", tc.tool, serializedResponseString(t, res))
	}

	require.Zero(t, calls.Load(), "dry-run must never reach Favro")
}
