package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	listTasksToolName  = "favro_list_tasks"
	getTaskToolName    = "favro_get_task"
	createTaskToolName = "favro_create_task"
	updateTaskToolName = "favro_update_task"
	deleteTaskToolName = "favro_delete_task"
)

// listTasksInput scopes to one card; Favro rejects an unscoped
// listing. task_list_id narrows further to a single checklist.
type listTasksInput struct {
	listInput
	CardCommonID string `json:"card_common_id" jsonschema:"the cross-widget cardCommonId whose tasks to list; required by Favro on every page (must be passed on follow-up pages too)"`
	TaskListID   string `json:"task_list_id,omitempty" jsonschema:"narrow to a single checklist on that card. Resolve via favro_list_tasklists."`
}

type getTaskInput struct {
	TaskID string `json:"task_id" jsonschema:"the Favro taskId"`
}

type createTaskInput struct {
	dryRunInput
	TaskListID string   `json:"task_list_id" jsonschema:"the checklist to add the item to. Create one with favro_create_tasklist, or find it via favro_list_tasklists."`
	Name       string   `json:"name" jsonschema:"the checklist item's text (required)"`
	Position   *float64 `json:"position,omitempty" jsonschema:"ordering within the checklist; omit to append at the end. Fractional values slot between siblings."`
	Completed  *bool    `json:"completed,omitempty" jsonschema:"create the item already ticked; defaults to false"`
}

type updateTaskInput struct {
	dryRunInput
	TaskID    string   `json:"task_id" jsonschema:"the Favro taskId to update"`
	Name      string   `json:"name,omitempty" jsonschema:"new item text; omit to keep current"`
	Position  *float64 `json:"position,omitempty" jsonschema:"new ordering within the checklist; omit to keep current"`
	Completed *bool    `json:"completed,omitempty" jsonschema:"true to tick the item, false to un-tick; omit to keep current"`
}

type deleteTaskInput struct {
	dryRunInput
	TaskID string `json:"task_id" jsonschema:"the Favro taskId to delete"`
}

func registerTasks(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listTasksToolName,
		Description: "List checklist items on a Favro card. Favro calls checklist items " +
			"\"tasks\" and the checklists that hold them \"tasklists\". `card_common_id` is " +
			"REQUIRED (the cross-widget card identity, not the per-widget cardId); add " +
			"`task_list_id` to narrow to one checklist. Returns one page; pass `page` plus " +
			"the prior `request_id` (and `card_common_id` again) for later pages. " +
			"favro_get_card_full already reports the done/total counts, so reach for this " +
			"when the individual item names matter. Read-only.",
		Annotations: readOnly("List Favro tasks"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listTasksInput) (*mcp.CallToolResult, listOutput[favro.Task], error) {
		env, err := client.ListTasks(ctx, in.Page, in.RequestID, favro.ListTasksFilter{
			CardCommonID: in.CardCommonID,
			TaskListID:   in.TaskListID,
		})
		if err != nil {
			return nil, listOutput[favro.Task]{}, err
		}
		return nil, newListOutput(env), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        getTaskToolName,
		Description: "Get a single Favro checklist item by its taskId. Read-only.",
		Annotations: readOnly("Get Favro task"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getTaskInput) (*mcp.CallToolResult, favro.Task, error) {
		t, err := client.GetTask(ctx, in.TaskID)
		if err != nil {
			return nil, favro.Task{}, err
		}
		return nil, t, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: createTaskToolName,
		Description: "Add a checklist item to an existing Favro checklist. Items belong to a " +
			"checklist, not directly to a card — create the checklist first with " +
			"favro_create_tasklist if the card doesn't have one. Omit `position` to append. " +
			"Pass `dry_run: true` to preview.",
		Annotations: mutating("Create Favro task", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createTaskInput) (*mcp.CallToolResult, writeOutput[favro.Task], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Task, error) {
				return client.CreateTask(writeCtx, favro.CreateTaskRequest{
					TaskListID: in.TaskListID,
					Name:       in.Name,
					Position:   in.Position,
					Completed:  in.Completed,
				})
			},
			func() string {
				return fmt.Sprintf("would add task %q to checklist %q", in.Name, in.TaskListID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.Task]{}, err
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: updateTaskToolName,
		Description: "Update a Favro checklist item. Every field is optional — pass at least " +
			"one. `completed: true` ticks the item, `completed: false` un-ticks it. Pass " +
			"`dry_run: true` to preview.",
		Annotations: mutating("Update Favro task", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateTaskInput) (*mcp.CallToolResult, writeOutput[favro.Task], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Task, error) {
				return client.UpdateTask(writeCtx, in.TaskID, favro.UpdateTaskRequest{
					Name:      in.Name,
					Position:  in.Position,
					Completed: in.Completed,
				})
			},
			func() string { return updateTaskStateDiff(&in) },
		)
		if err != nil {
			return nil, writeOutput[favro.Task]{}, err
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: deleteTaskToolName,
		Description: "Delete a Favro checklist item by its taskId. Destructive — MCP hosts " +
			"may warn before auto-confirming. Pass `dry_run: true` to preview.",
		Annotations: mutating("Delete Favro task", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteTaskInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (struct{}, error) { return struct{}{}, client.DeleteTask(writeCtx, in.TaskID) },
			func() string { return fmt.Sprintf("would delete task %q", in.TaskID) },
		)
		if err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		return nil, out, nil
	})
}

// updateTaskStateDiff renders the dry-run state-diff phrase for
// favro_update_task.
func updateTaskStateDiff(in *updateTaskInput) string {
	var changes []string
	if in.Name != "" {
		changes = append(changes, fmt.Sprintf("name → %q", in.Name))
	}
	if in.Position != nil {
		changes = append(changes, fmt.Sprintf("position → %v", *in.Position))
	}
	if in.Completed != nil {
		changes = append(changes, fmt.Sprintf("completed → %t", *in.Completed))
	}
	if len(changes) == 0 {
		return fmt.Sprintf("would PUT task %q with no changed fields (no-op)", in.TaskID)
	}
	return fmt.Sprintf("would update task %q: %s", in.TaskID, strings.Join(changes, ", "))
}
