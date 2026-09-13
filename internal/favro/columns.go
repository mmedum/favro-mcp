package favro

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

// UpdateColumnRequest is the body for PUT /columns/{columnId}. All
// fields are optional.
type UpdateColumnRequest struct {
	Name     string `json:"name,omitempty"`
	Color    string `json:"color,omitempty"`
	Position *int   `json:"position,omitempty"`
}
