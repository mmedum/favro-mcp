package favro

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

// errMissingWidgetCommonID is returned by ListColumns when the caller
// passes an empty widgetCommonID. Favro's /columns endpoint rejects
// the unfiltered listing with HTTP 400, so the client short-circuits
// locally and surfaces the requirement directly to the caller.
var errMissingWidgetCommonID = errors.New("favro: widget_common_id is required for listing columns")

// Column is a Favro column — one status lane on a widget. A column
// belongs to exactly one widget (WidgetCommonID).
//
// Favro returns aggregate counts (CardCount / TimeSum / EstimationSum)
// alongside the column metadata. Sum fields are integers in Favro's
// own units (TimeSum is milliseconds; EstimationSum is whatever the
// widget's estimation unit is). Fields outside this struct are
// ignored on decode (forward-compatible).
type Column struct {
	ColumnID       string `json:"columnId"`
	OrganizationID string `json:"organizationId,omitempty"`
	WidgetCommonID string `json:"widgetCommonId"`
	Name           string `json:"name"`
	Position       int    `json:"position"`
	CardCount      int    `json:"cardCount"`
	TimeSum        int    `json:"timeSum"`
	EstimationSum  int    `json:"estimationSum"`
}

// ListColumns returns one page of columns on the widget identified
// by widgetCommonID. The widget id is mandatory — Favro's /columns
// endpoint rejects an unfiltered listing with HTTP 400 (verified
// live), so callers must always scope the request to a widget.
//
// Callers that paginate must pass widgetCommonID on every page;
// dropping the filter mid-pagination would silently flip Favro into
// the same 400 response.
func (c *Client) ListColumns(ctx context.Context, page int, requestID, widgetCommonID string) (PageEnvelope[Column], error) {
	if widgetCommonID == "" {
		return PageEnvelope[Column]{}, errMissingWidgetCommonID
	}
	q := url.Values{}
	q.Set("widgetCommonId", widgetCommonID)
	return listPageQ[Column](ctx, c, "/columns", q, page, requestID)
}

// GetColumn returns a single column by its columnId. Returns
// *NotFoundError if no such column exists in the active organization.
func (c *Client) GetColumn(ctx context.Context, columnID string) (Column, error) {
	return getByID[Column](ctx, c, "/columns", columnID)
}

// CreateColumnRequest is the body for POST /columns. Both
// widgetCommonId and name are required. Position is optional —
// Favro appends to the end when omitted. Pointer typing on Position
// distinguishes "absent" from explicit 0 (top of the column list).
type CreateColumnRequest struct {
	WidgetCommonID string `json:"widgetCommonId"`
	Name           string `json:"name"`
	Color          string `json:"color,omitempty"`
	Position       *int   `json:"position,omitempty"`
}

// CreateColumn creates a new column on the given widget.
func (c *Client) CreateColumn(ctx context.Context, req CreateColumnRequest) (Column, error) {
	if req.WidgetCommonID == "" {
		return Column{}, errMissingWidgetCommonID
	}
	if req.Name == "" {
		return Column{}, fmt.Errorf("favro: column name is required")
	}
	var out Column
	if err := c.PostJSON(ctx, "/columns", req, &out); err != nil {
		return Column{}, err
	}
	return out, nil
}

// UpdateColumnRequest is the body for PUT /columns/{columnId}. All
// fields are optional.
type UpdateColumnRequest struct {
	Name     string `json:"name,omitempty"`
	Color    string `json:"color,omitempty"`
	Position *int   `json:"position,omitempty"`
}

// UpdateColumn updates a column by its columnId.
func (c *Client) UpdateColumn(ctx context.Context, columnID string, req UpdateColumnRequest) (Column, error) {
	if columnID == "" {
		return Column{}, errMissingID
	}
	var out Column
	if err := c.PutJSON(ctx, "/columns/"+url.PathEscape(columnID), req, &out); err != nil {
		return Column{}, err
	}
	return out, nil
}

// DeleteColumn deletes a column by its columnId. Empty columnID
// short-circuits with errMissingID; *NotFoundError on 404. Honors
// WithDryRun / ForceDryRun via the wrapped DeleteJSON.
//
// Favro forbids deleting a column that contains cards (returns 400).
// Callers must move/archive the cards out first; this helper does
// NOT cascade.
func (c *Client) DeleteColumn(ctx context.Context, columnID string) error {
	return deleteByID(ctx, c, "/columns", columnID)
}
