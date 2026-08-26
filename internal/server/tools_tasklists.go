package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	listTasklistsToolName  = "favro_list_tasklists"
	getTasklistToolName    = "favro_get_tasklist"
	createTasklistToolName = "favro_create_tasklist"
	updateTasklistToolName = "favro_update_tasklist"
	deleteTasklistToolName = "favro_delete_tasklist"
)

type listTasklistsInput struct {
	listInput
	CardCommonID string `json:"card_common_id" jsonschema:"the cross-widget cardCommonId whose checklists to list; required by Favro on every page (must be passed on follow-up pages too)"`
}

type getTasklistInput struct {
	TaskListID string `json:"task_list_id" jsonschema:"the Favro taskListId"`
}

// createTasklistInput can seed the checklist with items in the same
// round-trip, saving a favro_create_task call per item.
type createTasklistInput struct {
	dryRunInput
	CardCommonID string           `json:"card_common_id" jsonschema:"the cross-widget cardCommonId to add the checklist to"`
	Name         string           `json:"name" jsonschema:"the checklist's title (required)"`
	Position     *float64         `json:"position,omitempty" jsonschema:"ordering among the card's checklists; omit to append at the end"`
	Tasks        []favro.CardTask `json:"tasks,omitempty" jsonschema:"optional initial items, each {name, completed}. Seeding them here avoids one favro_create_task call per item."`
}

type updateTasklistInput struct {
	dryRunInput
	TaskListID string   `json:"task_list_id" jsonschema:"the Favro taskListId to update"`
	Name       string   `json:"name,omitempty" jsonschema:"new checklist title; omit to keep current"`
	Position   *float64 `json:"position,omitempty" jsonschema:"new ordering among the card's checklists; omit to keep current"`
}

type deleteTasklistInput struct {
	dryRunInput
	TaskListID string `json:"task_list_id" jsonschema:"the Favro taskListId to delete"`
}

func registerTasklists(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listTasklistsToolName,
		Description: "List the checklists on a Favro card. Favro calls them \"tasklists\"; " +
			"their items are \"tasks\" (see favro_list_tasks). `card_common_id` is REQUIRED " +
			"(the cross-widget card identity, not the per-widget cardId). Returns one page; " +
			"pass `page` plus the prior `request_id` (and `card_common_id` again) for later " +
			"pages. Read-only.",
		Annotations: readOnly("List Favro tasklists"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listTasklistsInput) (*mcp.CallToolResult, listOutput[favro.Tasklist], error) {
		env, err := client.ListTasklists(ctx, in.Page, in.RequestID, in.CardCommonID)
		if err != nil {
			return nil, listOutput[favro.Tasklist]{}, err
		}
		return nil, newListOutput(env), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        getTasklistToolName,
		Description: "Get a single Favro checklist by its taskListId. Read-only.",
		Annotations: readOnly("Get Favro tasklist"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getTasklistInput) (*mcp.CallToolResult, favro.Tasklist, error) {
		tl, err := client.GetTasklist(ctx, in.TaskListID)
		if err != nil {
			return nil, favro.Tasklist{}, err
		}
		return nil, tl, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: createTasklistToolName,
		Description: "Add a checklist to a Favro card. Pass `tasks` to seed it with items in " +
			"the same request rather than calling favro_create_task per item. " +
			"`card_common_id` is the cross-widget card identity, not the per-widget cardId. " +
			"Pass `dry_run: true` to preview.",
		Annotations: mutating("Create Favro tasklist", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createTasklistInput) (*mcp.CallToolResult, writeOutput[favro.Tasklist], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Tasklist, error) {
				return client.CreateTasklist(writeCtx, favro.CreateTasklistRequest{
					CardCommonID: in.CardCommonID,
					Name:         in.Name,
					Position:     in.Position,
					Tasks:        in.Tasks,
				})
			},
			func() string {
				return fmt.Sprintf("would add checklist %q with %d item(s) to card %q", in.Name, len(in.Tasks), in.CardCommonID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.Tasklist]{}, err
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: updateTasklistToolName,
		Description: "Rename or reorder a Favro checklist. Its items are managed separately " +
			"via favro_create_task / favro_update_task / favro_delete_task. Pass " +
			"`dry_run: true` to preview.",
		Annotations: mutating("Update Favro tasklist", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateTasklistInput) (*mcp.CallToolResult, writeOutput[favro.Tasklist], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Tasklist, error) {
				return client.UpdateTasklist(writeCtx, in.TaskListID, favro.UpdateTasklistRequest{
					Name:     in.Name,
					Position: in.Position,
				})
			},
			func() string { return updateTasklistStateDiff(&in) },
		)
		if err != nil {
			return nil, writeOutput[favro.Tasklist]{}, err
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: deleteTasklistToolName,
		Description: "Delete a Favro checklist and every item in it. Destructive — MCP hosts " +
			"may warn before auto-confirming. Pass `dry_run: true` to preview.",
		Annotations: mutating("Delete Favro tasklist", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteTasklistInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (struct{}, error) { return struct{}{}, client.DeleteTasklist(writeCtx, in.TaskListID) },
			func() string {
				return fmt.Sprintf("would delete checklist %q and every item in it", in.TaskListID)
			},
		)
		if err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		return nil, out, nil
	})
}

// updateTasklistStateDiff renders the dry-run state-diff phrase for
// favro_update_tasklist.
func updateTasklistStateDiff(in *updateTasklistInput) string {
	var changes []string
	if in.Name != "" {
		changes = append(changes, fmt.Sprintf("name → %q", in.Name))
	}
	if in.Position != nil {
		changes = append(changes, fmt.Sprintf("position → %v", *in.Position))
	}
	if len(changes) == 0 {
		return fmt.Sprintf("would PUT checklist %q with no changed fields (no-op)", in.TaskListID)
	}
	return fmt.Sprintf("would update checklist %q: %s", in.TaskListID, strings.Join(changes, ", "))
}
