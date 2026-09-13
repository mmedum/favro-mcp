package favroapi

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// ListTasks returns one page of tasks. filter.CardCommonID is
// required — Favro rejects an unscoped listing, so the check happens
// client-side to give a clearer message than the API's 400.
func (c *Client) ListTasks(ctx context.Context, page int, requestID string, filter favro.ListTasksFilter) (favro.PageEnvelope[favro.Task], error) {
	if filter.CardCommonID == "" {
		return favro.PageEnvelope[favro.Task]{}, fmt.Errorf("favro: card_common_id is required to list tasks")
	}
	return listPageQ[favro.Task](ctx, c, "/tasks", filter.Values(), page, requestID)
}

// GetTask returns a single task by its taskId.
func (c *Client) GetTask(ctx context.Context, taskID string) (favro.Task, error) {
	return getByID[favro.Task](ctx, c, "/tasks", taskID)
}

// CreateTask adds a task to an existing tasklist.
func (c *Client) CreateTask(ctx context.Context, req favro.CreateTaskRequest) (favro.Task, error) {
	if req.TaskListID == "" {
		return favro.Task{}, fmt.Errorf("favro: task_list_id is required")
	}
	if req.Name == "" {
		return favro.Task{}, fmt.Errorf("favro: task name is required")
	}
	var out favro.Task
	if err := c.PostJSON(ctx, "/tasks", req, &out); err != nil {
		return favro.Task{}, err
	}
	return out, nil
}

// UpdateTask updates a task by its taskId.
func (c *Client) UpdateTask(ctx context.Context, taskID string, req favro.UpdateTaskRequest) (favro.Task, error) {
	if taskID == "" {
		return favro.Task{}, errMissingID
	}
	var out favro.Task
	if err := c.PutJSON(ctx, "/tasks/"+url.PathEscape(taskID), req, &out); err != nil {
		return favro.Task{}, err
	}
	return out, nil
}

// DeleteTask deletes a task by its taskId. Honors WithDryRun /
// ForceDryRun via the wrapped DeleteJSON.
func (c *Client) DeleteTask(ctx context.Context, taskID string) error {
	return deleteByID(ctx, c, "/tasks", taskID)
}
