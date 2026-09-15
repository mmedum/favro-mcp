package favroapi

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mmedum/favro-mcp/internal/favro"
	"github.com/mmedum/favro-mcp/internal/render"
)

// errMissingWidgetCommonID is returned by ListColumns when the caller
// passes an empty widgetCommonID. Favro's /columns endpoint rejects
// the unfiltered listing with HTTP 400, so the client short-circuits
// locally and surfaces the requirement directly to the caller.
var errMissingWidgetCommonID = render.Sentinel(render.ClassInvalid, "favro: widget_common_id is required for listing columns")

// ListColumns returns one page of columns on the widget identified
// by widgetCommonID. The widget id is mandatory — Favro's /columns
// endpoint rejects an unfiltered listing with HTTP 400 (verified
// live), so callers must always scope the request to a widget.
//
// Callers that paginate must pass widgetCommonID on every page;
// dropping the filter mid-pagination would silently flip Favro into
// the same 400 response.
func (c *Client) ListColumns(ctx context.Context, page int, requestID, widgetCommonID string) (favro.PageEnvelope[favro.Column], error) {
	if widgetCommonID == "" {
		return favro.PageEnvelope[favro.Column]{}, errMissingWidgetCommonID
	}
	q := url.Values{}
	q.Set("widgetCommonId", widgetCommonID)
	return listPageQ[favro.Column](ctx, c, "/columns", q, page, requestID)
}

// GetColumn returns a single column by its columnId. Returns
// *NotFoundError if no such column exists in the active organization.
func (c *Client) GetColumn(ctx context.Context, columnID string) (favro.Column, error) {
	return getByID[favro.Column](ctx, c, "/columns", columnID)
}

// CreateColumn creates a new column on the given widget.
func (c *Client) CreateColumn(ctx context.Context, req favro.CreateColumnRequest) (favro.Column, error) {
	if req.WidgetCommonID == "" {
		return favro.Column{}, errMissingWidgetCommonID
	}
	if req.Name == "" {
		return favro.Column{}, fmt.Errorf("favro: column name is required")
	}
	var out favro.Column
	if err := c.PostJSON(ctx, "/columns", req, &out); err != nil {
		return favro.Column{}, err
	}
	return out, nil
}

// UpdateColumn updates a column by its columnId.
func (c *Client) UpdateColumn(ctx context.Context, columnID string, req favro.UpdateColumnRequest) (favro.Column, error) {
	if columnID == "" {
		return favro.Column{}, errMissingID
	}
	var out favro.Column
	if err := c.PutJSON(ctx, "/columns/"+url.PathEscape(columnID), req, &out); err != nil {
		return favro.Column{}, err
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
