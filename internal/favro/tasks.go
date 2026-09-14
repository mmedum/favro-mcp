package favro

import "net/url"

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

// CreateTaskRequest is the body for POST /tasks. TaskListID and Name
// are required; the task is appended to the end of the list when
// Position is omitted.
type CreateTaskRequest struct {
	TaskListID string   `json:"taskListId"`
	Name       string   `json:"name"`
	Position   *float64 `json:"position,omitempty"`
	Completed  *bool    `json:"completed,omitempty"`
}

// UpdateTaskRequest is the body for PUT /tasks/{taskId}. Every field
// is optional; absent ones are left untouched. Completed is *bool so
// &false (un-tick the item) is distinguishable from "don't touch".
type UpdateTaskRequest struct {
	Name      string   `json:"name,omitempty"`
	Position  *float64 `json:"position,omitempty"`
	Completed *bool    `json:"completed,omitempty"`
}
