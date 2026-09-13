package favroapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// ListWidgets returns one page of widgets in the active organization.
// Filters are optional; a zero filter requests every widget the
// token can see.
//
// Callers that paginate must pass the same filter on every page —
// dropping a filter mid-pagination silently switches the result set.
func (c *Client) ListWidgets(ctx context.Context, page int, requestID string, filter favro.ListWidgetsFilter) (favro.PageEnvelope[favro.Widget], error) {
	return listPageQ[favro.Widget](ctx, c, "/widgets", filter.Values(), page, requestID)
}

// GetWidget returns a single widget by its widgetCommonId. Returns
// *NotFoundError if no such widget exists in the active organization.
func (c *Client) GetWidget(ctx context.Context, widgetCommonID string) (favro.Widget, error) {
	return getByID[favro.Widget](ctx, c, "/widgets", widgetCommonID)
}

// CreateWidget creates a new widget in the given collection.
func (c *Client) CreateWidget(ctx context.Context, req favro.CreateWidgetRequest) (favro.Widget, error) {
	if req.CollectionID == "" {
		return favro.Widget{}, fmt.Errorf("favro: collection_id is required")
	}
	if req.Name == "" {
		return favro.Widget{}, fmt.Errorf("favro: widget name is required")
	}
	var out favro.Widget
	if err := c.PostJSON(ctx, "/widgets", req, &out); err != nil {
		return favro.Widget{}, err
	}
	return out, nil
}

// UpdateWidget updates a widget by its widgetCommonId. Archiving
// requires req.CollectionID — Favro scopes the archive to one of the
// collections the widget belongs to, and omitting it makes the call
// fail server-side with a less obvious message.
func (c *Client) UpdateWidget(ctx context.Context, widgetCommonID string, req favro.UpdateWidgetRequest) (favro.Widget, error) {
	if widgetCommonID == "" {
		return favro.Widget{}, errMissingID
	}
	if req.Archive != nil && req.CollectionID == "" {
		return favro.Widget{}, fmt.Errorf("favro: archiving a widget requires collection_id (a widget can belong to several collections)")
	}
	var out favro.Widget
	if err := c.PutJSON(ctx, "/widgets/"+url.PathEscape(widgetCommonID), req, &out); err != nil {
		return favro.Widget{}, err
	}
	return out, nil
}

// DeleteWidget deletes a widget by its widgetCommonId. collectionID
// scopes the delete to a single collection; empty deletes every
// instance of the widget across all collections it belongs to.
// Honors WithDryRun / ForceDryRun via the wrapped DeleteJSON.
func (c *Client) DeleteWidget(ctx context.Context, widgetCommonID, collectionID string) error {
	if widgetCommonID == "" {
		return errMissingID
	}
	q := url.Values{}
	if collectionID != "" {
		q.Set("collectionId", collectionID)
	}
	return c.doJSON(ctx, http.MethodDelete, "/widgets/"+url.PathEscape(widgetCommonID), q, nil, nil)
}
