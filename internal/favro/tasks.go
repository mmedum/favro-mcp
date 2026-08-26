package favro

import (
	"context"
	"fmt"
	"net/url"
)

// Task is one checklist item on a Favro card. "Task" is the API name
// for what the Favro UI calls a checklist item; the containing
// checklist is a Tasklist.
//
// Tasks are addressed by CardCommonID rather than the per-widget
// CardID: a card that appears on several widgets shares one set of
// checklists. Fields outside this struct are ignored on decode
// (forward-compatible).
type Task struct {
	TaskID         string `json:"taskId"`
	TaskListID     string `json:"taskListId,omitempty"`
	CardCommonID   string `json:"cardCommonId,omitempty"`
	OrganizationID string `json:"organizationId,omitempty"`
	Name           string `json:"name"`
	Completed      bool   `json:"completed,omitempty"`
	// Position orders the task within its tasklist. Favro uses
	// fractional positions to slot an item between two siblings
	// without renumbering, so this is a float even though the
	// examples show integers.
	Position float64 `json:"position,omitempty"`
}

// ListTasksFilter bundles the query parameters for /tasks.
// CardCommonID is required by Favro; TaskListID narrows to one
// checklist on that card.
type ListTasksFilter struct {
	CardCommonID string
	TaskListID   string
}

// Values returns the filter as url.Values; empty fields are omitted.
func (f ListTasksFilter) Values() url.Values {
	q := url.Values{}
	if f.CardCommonID != "" {
		q.Set("cardCommonId", f.CardCommonID)
	}
	if f.TaskListID != "" {
		q.Set("taskListId", f.TaskListID)
	}
	return q
}

// ListTasks returns one page of tasks. filter.CardCommonID is
// required — Favro rejects an unscoped listing, so the check happens
// client-side to give a clearer message than the API's 400.
func (c *Client) ListTasks(ctx context.Context, page int, requestID string, filter ListTasksFilter) (PageEnvelope[Task], error) {
	if filter.CardCommonID == "" {
		return PageEnvelope[Task]{}, fmt.Errorf("favro: card_common_id is required to list tasks")
	}
	return listPageQ[Task](ctx, c, "/tasks", filter.Values(), page, requestID)
}

// GetTask returns a single task by its taskId.
func (c *Client) GetTask(ctx context.Context, taskID string) (Task, error) {
	return getByID[Task](ctx, c, "/tasks", taskID)
}

// CreateTaskRequest is the body for POST /tasks. TaskListID and Name
// are required; the task is appended to the end of the list when
// Position is omitted.
type CreateTaskRequest struct {
	TaskListID string   `json:"taskListId"`
	Name       string   `json:"name"`
	Position   *float64 `json:"position,omitempty"`
	Completed  *bool    `json:"completed,omitempty"`
}

// CreateTask adds a task to an existing tasklist.
func (c *Client) CreateTask(ctx context.Context, req CreateTaskRequest) (Task, error) {
	if req.TaskListID == "" {
		return Task{}, fmt.Errorf("favro: task_list_id is required")
	}
	if req.Name == "" {
		return Task{}, fmt.Errorf("favro: task name is required")
	}
	var out Task
	if err := c.PostJSON(ctx, "/tasks", req, &out); err != nil {
		return Task{}, err
	}
	return out, nil
}

// UpdateTaskRequest is the body for PUT /tasks/{taskId}. Every field
// is optional; absent ones are left untouched. Completed is *bool so
// &false (un-tick the item) is distinguishable from "don't touch".
type UpdateTaskRequest struct {
	Name      string   `json:"name,omitempty"`
	Position  *float64 `json:"position,omitempty"`
	Completed *bool    `json:"completed,omitempty"`
}

// UpdateTask updates a task by its taskId.
func (c *Client) UpdateTask(ctx context.Context, taskID string, req UpdateTaskRequest) (Task, error) {
	if taskID == "" {
		return Task{}, errMissingID
	}
	var out Task
	if err := c.PutJSON(ctx, "/tasks/"+url.PathEscape(taskID), req, &out); err != nil {
		return Task{}, err
	}
	return out, nil
}

// DeleteTask deletes a task by its taskId. Honors WithDryRun /
// ForceDryRun via the wrapped DeleteJSON.
func (c *Client) DeleteTask(ctx context.Context, taskID string) error {
	return deleteByID(ctx, c, "/tasks", taskID)
}
