package favro

import (
	"context"
	"fmt"
	"net/http"
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
	OrganizationID string   `json:"organizationId,omitempty"`
	Name           string   `json:"name"`
	CollectionIDs  []string `json:"collectionIds,omitempty"`
	Type           string   `json:"type,omitempty"`
	Color          string   `json:"color,omitempty"`
	Archived       bool     `json:"archived,omitempty"`
	// BreakdownCardCommonID points at the breakdown card.
	BreakdownCardCommonID string `json:"breakdownCardCommonId,omitempty"`
	OwnerRole             string `json:"ownerRole,omitempty"`
	EditRole              string `json:"editRole,omitempty"`
	// Lanes is the list of horizontal groupings on the widget. Only
	// populated for widgets that use lanes (board / matrix views);
	// list views have no lanes.
	Lanes []WidgetLane `json:"lanes,omitempty"`
	// Columns is a denormalized summary of the widget's columns
	// (id + name + color). The full /columns endpoint returns more
	// (cardCount, timeSum, estimationSum aggregates). Use this for
	// quick widget-context lookups; use /columns for the full data.
	Columns []WidgetColumn `json:"columns,omitempty"`
}

// WidgetLane is one lane on a widget, embedded in the widget
// response. Lanes group cards horizontally on board/matrix views.
type WidgetLane struct {
	LaneID string `json:"laneId"`
	Name   string `json:"name"`
}

// WidgetColumn is one column summary embedded on a Widget response.
// It carries id + name + color only; for cardCount / timeSum /
// estimationSum aggregates fetch from /columns.
type WidgetColumn struct {
	ColumnID string `json:"columnId"`
	Name     string `json:"name"`
	Color    string `json:"color,omitempty"`
}

// ListWidgetsFilter bundles the documented query parameters for
// /widgets. Both fields are optional; a zero filter requests every
// widget the token can see in the organization.
type ListWidgetsFilter struct {
	CollectionID string
	Archived     bool
}

// Values returns the filter as url.Values; empty fields are omitted.
func (f ListWidgetsFilter) Values() url.Values {
	q := url.Values{}
	if f.CollectionID != "" {
		q.Set("collectionId", f.CollectionID)
	}
	if f.Archived {
		q.Set("archived", "true")
	}
	return q
}

// ListWidgets returns one page of widgets in the active organization.
// Filters are optional; a zero filter requests every widget the
// token can see.
//
// Callers that paginate must pass the same filter on every page —
// dropping a filter mid-pagination silently switches the result set.
func (c *Client) ListWidgets(ctx context.Context, page int, requestID string, filter ListWidgetsFilter) (PageEnvelope[Widget], error) {
	return listPageQ[Widget](ctx, c, "/widgets", filter.Values(), page, requestID)
}

// GetWidget returns a single widget by its widgetCommonId. Returns
// *NotFoundError if no such widget exists in the active organization.
func (c *Client) GetWidget(ctx context.Context, widgetCommonID string) (Widget, error) {
	return getByID[Widget](ctx, c, "/widgets", widgetCommonID)
}

// CreateWidgetRequest is the body for POST /widgets. CollectionID
// pins the widget to a primary collection; Name is required.
//
// Type is one of "backlog" / "board" / "calendar" / "table" /
// "matrix" — left as a plain string because Favro extends the value
// set without notice. Empty Type lets Favro pick the default.
type CreateWidgetRequest struct {
	CollectionID          string `json:"collectionId"`
	Name                  string `json:"name"`
	Type                  string `json:"type,omitempty"`
	Color                 string `json:"color,omitempty"`
	BreakdownCardCommonID string `json:"breakdownCardCommonId,omitempty"`
	OwnerRole             string `json:"ownerRole,omitempty"`
	EditRole              string `json:"editRole,omitempty"`
}

// CreateWidget creates a new widget in the given collection.
func (c *Client) CreateWidget(ctx context.Context, req CreateWidgetRequest) (Widget, error) {
	if req.CollectionID == "" {
		return Widget{}, fmt.Errorf("favro: collection_id is required")
	}
	if req.Name == "" {
		return Widget{}, fmt.Errorf("favro: widget name is required")
	}
	var out Widget
	if err := c.PostJSON(ctx, "/widgets", req, &out); err != nil {
		return Widget{}, err
	}
	return out, nil
}

// UpdateWidgetRequest is the body for PUT /widgets/{widgetCommonId}.
// Every field is optional; absent ones are left untouched. Archive
// is *bool so &false (unarchive) is distinguishable from "don't touch".
//
// CollectionID is REQUIRED whenever Archive is set: a widget can live
// in several collections, so Favro needs to know which one the
// archive applies to. UpdateWidget enforces the pairing client-side
// rather than letting the request fail at the API.
//
// Type and BreakdownCardCommonID are not in Favro's documented
// update parameter list; they are retained because earlier phases
// sent them and Favro accepts the body.
type UpdateWidgetRequest struct {
	Name                  string `json:"name,omitempty"`
	Type                  string `json:"type,omitempty"`
	Color                 string `json:"color,omitempty"`
	BreakdownCardCommonID string `json:"breakdownCardCommonId,omitempty"`
	OwnerRole             string `json:"ownerRole,omitempty"`
	EditRole              string `json:"editRole,omitempty"`
	Archive               *bool  `json:"archive,omitempty"`
	CollectionID          string `json:"collectionId,omitempty"`
}

// UpdateWidget updates a widget by its widgetCommonId. Archiving
// requires req.CollectionID — Favro scopes the archive to one of the
// collections the widget belongs to, and omitting it makes the call
// fail server-side with a less obvious message.
func (c *Client) UpdateWidget(ctx context.Context, widgetCommonID string, req UpdateWidgetRequest) (Widget, error) {
	if widgetCommonID == "" {
		return Widget{}, errMissingID
	}
	if req.Archive != nil && req.CollectionID == "" {
		return Widget{}, fmt.Errorf("favro: archiving a widget requires collection_id (a widget can belong to several collections)")
	}
	var out Widget
	if err := c.PutJSON(ctx, "/widgets/"+url.PathEscape(widgetCommonID), req, &out); err != nil {
		return Widget{}, err
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
