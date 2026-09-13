package favroapi

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// ListTasklists returns one page of a card's checklists.
// cardCommonID is required — Favro rejects an unscoped listing, so
// the check happens client-side to give a clearer message than the
// API's 400.
func (c *Client) ListTasklists(ctx context.Context, page int, requestID, cardCommonID string) (favro.PageEnvelope[favro.Tasklist], error) {
	if cardCommonID == "" {
		return favro.PageEnvelope[favro.Tasklist]{}, fmt.Errorf("favro: card_common_id is required to list tasklists")
	}
	q := url.Values{"cardCommonId": []string{cardCommonID}}
	return listPageQ[favro.Tasklist](ctx, c, "/tasklists", q, page, requestID)
}

// GetTasklist returns a single checklist by its taskListId.
func (c *Client) GetTasklist(ctx context.Context, taskListID string) (favro.Tasklist, error) {
	return getByID[favro.Tasklist](ctx, c, "/tasklists", taskListID)
}

// CreateTasklist adds a checklist to a card.
func (c *Client) CreateTasklist(ctx context.Context, req favro.CreateTasklistRequest) (favro.Tasklist, error) {
	if req.CardCommonID == "" {
		return favro.Tasklist{}, fmt.Errorf("favro: card_common_id is required")
	}
	if req.Name == "" {
		return favro.Tasklist{}, fmt.Errorf("favro: tasklist name is required")
	}
	var out favro.Tasklist
	if err := c.PostJSON(ctx, "/tasklists", req, &out); err != nil {
		return favro.Tasklist{}, err
	}
	return out, nil
}

// UpdateTasklist updates a checklist by its taskListId.
func (c *Client) UpdateTasklist(ctx context.Context, taskListID string, req favro.UpdateTasklistRequest) (favro.Tasklist, error) {
	if taskListID == "" {
		return favro.Tasklist{}, errMissingID
	}
	var out favro.Tasklist
	if err := c.PutJSON(ctx, "/tasklists/"+url.PathEscape(taskListID), req, &out); err != nil {
		return favro.Tasklist{}, err
	}
	return out, nil
}

// DeleteTasklist deletes a checklist and its items by taskListId.
// Honors WithDryRun / ForceDryRun via the wrapped DeleteJSON.
func (c *Client) DeleteTasklist(ctx context.Context, taskListID string) error {
	return deleteByID(ctx, c, "/tasklists", taskListID)
}
