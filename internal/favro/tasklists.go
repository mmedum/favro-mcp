package favro

import (
	"context"
	"fmt"
	"net/url"
)

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

// ListTasklists returns one page of a card's checklists.
// cardCommonID is required — Favro rejects an unscoped listing, so
// the check happens client-side to give a clearer message than the
// API's 400.
func (c *Client) ListTasklists(ctx context.Context, page int, requestID, cardCommonID string) (PageEnvelope[Tasklist], error) {
	if cardCommonID == "" {
		return PageEnvelope[Tasklist]{}, fmt.Errorf("favro: card_common_id is required to list tasklists")
	}
	q := url.Values{"cardCommonId": []string{cardCommonID}}
	return listPageQ[Tasklist](ctx, c, "/tasklists", q, page, requestID)
}

// GetTasklist returns a single checklist by its taskListId.
func (c *Client) GetTasklist(ctx context.Context, taskListID string) (Tasklist, error) {
	return getByID[Tasklist](ctx, c, "/tasklists", taskListID)
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

// CreateTasklist adds a checklist to a card.
func (c *Client) CreateTasklist(ctx context.Context, req CreateTasklistRequest) (Tasklist, error) {
	if req.CardCommonID == "" {
		return Tasklist{}, fmt.Errorf("favro: card_common_id is required")
	}
	if req.Name == "" {
		return Tasklist{}, fmt.Errorf("favro: tasklist name is required")
	}
	var out Tasklist
	if err := c.PostJSON(ctx, "/tasklists", req, &out); err != nil {
		return Tasklist{}, err
	}
	return out, nil
}

// UpdateTasklistRequest is the body for PUT /tasklists/{taskListId}.
// Both fields are optional; absent ones are left untouched. Tasks are
// managed through the /tasks endpoints, not here.
type UpdateTasklistRequest struct {
	Name     string   `json:"name,omitempty"`
	Position *float64 `json:"position,omitempty"`
}

// UpdateTasklist updates a checklist by its taskListId.
func (c *Client) UpdateTasklist(ctx context.Context, taskListID string, req UpdateTasklistRequest) (Tasklist, error) {
	if taskListID == "" {
		return Tasklist{}, errMissingID
	}
	var out Tasklist
	if err := c.PutJSON(ctx, "/tasklists/"+url.PathEscape(taskListID), req, &out); err != nil {
		return Tasklist{}, err
	}
	return out, nil
}

// DeleteTasklist deletes a checklist and its items by taskListId.
// Honors WithDryRun / ForceDryRun via the wrapped DeleteJSON.
func (c *Client) DeleteTasklist(ctx context.Context, taskListID string) error {
	return deleteByID(ctx, c, "/tasklists", taskListID)
}
