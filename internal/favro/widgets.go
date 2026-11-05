package favro

import "net/url"

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
// active widget the token can see in the organization.
type ListWidgetsFilter struct {
	CollectionID string
	// Archived, when true, includes archived widgets beside the active
	// ones.
	Archived bool
}

// Values returns the filter as url.Values; an empty CollectionID is
// omitted.
func (f ListWidgetsFilter) Values() url.Values {
	q := url.Values{}
	if f.CollectionID != "" {
		q.Set("collectionId", f.CollectionID)
	}
	// Favro answers no archived parameter with active and archived
	// widgets, archived=false with active ones only, and archived=true
	// with archived ones only. Its reference says only "If true, return
	// archived widgets. Optional." Checked by hand against a live
	// organization; the live steps "list widgets" and "list widgets,
	// archived ones included" read each form.
	// https://favro.com/developer/#get-all-widgets
	if !f.Archived {
		q.Set("archived", "false")
	}
	return q
}

// CreateWidgetRequest is the body for POST /widgets. CollectionID
// pins the widget to a primary collection; Name is required.
//
// Type is one of "backlog" / "board" / "calendar" / "table" /
// "matrix" — left as a plain string because Favro extends the value
// set without notice. Favro refuses a widget without a type, "type is
// expected as one of backlog, board", as its reference marks it required.
// https://favro.com/developer/#create-a-widget
type CreateWidgetRequest struct {
	CollectionID          string `json:"collectionId"`
	Name                  string `json:"name"`
	Type                  string `json:"type,omitempty"`
	Color                 string `json:"color,omitempty"`
	BreakdownCardCommonID string `json:"breakdownCardCommonId,omitempty"`
	OwnerRole             string `json:"ownerRole,omitempty"`
	EditRole              string `json:"editRole,omitempty"`
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
