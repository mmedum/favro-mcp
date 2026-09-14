package favro

// Tasklist is one checklist on a Favro card. "Tasklist" is the API
// name for what the Favro UI calls a checklist; its items are Tasks.
//
// Name carries the checklist's title. Favro's documented field table
// calls it `name` and its example responses call it `description`,
// so both wire keys are accepted on decode and Name() reconciles
// them — see the Title method.
type Tasklist struct {
	TaskListID     string `json:"taskListId"`
	CardCommonID   string `json:"cardCommonId,omitempty"`
	OrganizationID string `json:"organizationId,omitempty"`
	Name           string `json:"name,omitempty"`
	// Description is the key Favro's example payloads actually use
	// for the checklist title. Kept alongside Name because the
	// documented field table and the examples disagree and no live
	// payload has been captured to settle it.
	Description string `json:"description,omitempty"`
	// Position orders the tasklist among the card's checklists.
	// Fractional for the same reason as Task.Position.
	Position float64 `json:"position,omitempty"`
}

// Title returns the checklist's display title, preferring the
// documented `name` key and falling back to the `description` key
// the example payloads use.
func (t Tasklist) Title() string {
	if t.Name != "" {
		return t.Name
	}
	return t.Description
}

// CreateTasklistRequest is the body for POST /tasklists.
// CardCommonID and Name are required. Tasks seeds the checklist with
// items in the same round-trip.
type CreateTasklistRequest struct {
	CardCommonID string     `json:"cardCommonId"`
	Name         string     `json:"name"`
	Position     *float64   `json:"position,omitempty"`
	Tasks        []CardTask `json:"tasks,omitempty"`
}

// UpdateTasklistRequest is the body for PUT /tasklists/{taskListId}.
// Both fields are optional; absent ones are left untouched. Tasks are
// managed through the /tasks endpoints, not here.
type UpdateTasklistRequest struct {
	Name     string   `json:"name,omitempty"`
	Position *float64 `json:"position,omitempty"`
}
