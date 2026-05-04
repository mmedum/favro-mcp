package favro

import (
	"context"
	"errors"
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
