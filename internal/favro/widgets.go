package favro

import (
	"context"
	"net/url"
)

// Widget is a Favro widget — what the UI calls a "board". A widget
// can appear in multiple collections; CollectionIDs lists every
// collection it's been added to. Fields outside this struct are
// ignored on decode (forward-compatible).
//
// Type / OwnerRole / EditRole are left as plain strings because
// Favro extends the value sets without notice (existing Type values
// include "backlog", "board", "calendar", "table", "matrix"). Typed
// aliases would silently mask new ones.
type Widget struct {
	WidgetCommonID string   `json:"widgetCommonId"`
	Name           string   `json:"name"`
	CollectionIDs  []string `json:"collectionIds,omitempty"`
	Type           string   `json:"type,omitempty"`
	Color          string   `json:"color,omitempty"`
	// BreakdownCardCommonID points at the breakdown card; cards
	// (the resource) land in Phase 3.5+, so this is just an opaque
	// id at the widget layer.
	BreakdownCardCommonID string `json:"breakdownCardCommonId,omitempty"`
	OwnerRole             string `json:"ownerRole,omitempty"`
	EditRole              string `json:"editRole,omitempty"`
}

// ListWidgets returns one page of widgets in the active organization.
// If collectionID is non-empty the result is scoped to widgets in
// that collection (Favro's `collectionId` query parameter).
//
// Callers that paginate must pass collectionID on every page —
// dropping the filter mid-pagination silently switches the result
// set to org-wide widgets.
func (c *Client) ListWidgets(ctx context.Context, page int, requestID, collectionID string) (PageEnvelope[Widget], error) {
	q := url.Values{}
	if collectionID != "" {
		q.Set("collectionId", collectionID)
	}
	return listPageQ[Widget](ctx, c, "/widgets", q, page, requestID)
}

// GetWidget returns a single widget by its widgetCommonId. Returns
// *NotFoundError if no such widget exists in the active organization.
func (c *Client) GetWidget(ctx context.Context, widgetCommonID string) (Widget, error) {
	return getByID[Widget](ctx, c, "/widgets", widgetCommonID)
}
