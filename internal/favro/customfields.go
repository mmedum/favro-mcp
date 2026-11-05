package favro

// CustomField is a Favro custom field — org-global metadata that
// can be attached to any card, with the active value living on
// the card's CustomFieldsValues. Two reads matter for an LLM:
//
//   - The schema (this type): "what custom fields exist, what types
//     they are, and what the legal options are for select/status
//     types".
//   - The per-card value, which comes in the card payload's custom
//     fields and references CustomFieldID + CustomFieldItemID for
//     select-flavored fields.
//
// Type is the human-facing display label Favro uses for the field
// kind — values read live include "Text", "Number", "Date",
// "Date created", "Checkbox", "Single select", "Multiple select",
// "Members", "Tags", "Rating", "Link", "Progress", "Voting",
// "Relations", "Sequential ID", "Timeline". Favro extends this set
// without notice, so the field stays a plain string; a typed alias
// would silently mask new values. CustomFieldItems is populated only
// for select-flavored types ("Single select" / "Multiple select");
// for primitive types it's absent (omitempty drops the JSON field).
// Fields outside this struct are ignored on decode.
type CustomField struct {
	CustomFieldID  string `json:"customFieldId"`
	OrganizationID string `json:"organizationId,omitempty"`
	// WidgetCommonID is the widget the field is local to, in Favro's
	// words, and Favro sends it on every row. It does not say where a
	// field is enabled: a board's cards carry values for fields whose
	// widgetCommonId names another board. Fields are org-global and
	// enabled per widget, and the reference says neither.
	// https://favro.com/developer/#custom-field-object
	WidgetCommonID   string            `json:"widgetCommonId,omitempty"`
	Type             string            `json:"type"`
	Name             string            `json:"name"`
	Enabled          bool              `json:"enabled,omitempty"`
	CustomFieldItems []CustomFieldItem `json:"customFieldItems,omitempty"`
}

// CustomFieldItem is one option in a select / multi-select / status
// custom field. Color is only present on status-flavored fields;
// primitive selects don't carry a color. CustomFieldItemID is the
// stable identifier the per-card value references.
type CustomFieldItem struct {
	CustomFieldItemID string `json:"customFieldItemId"`
	Name              string `json:"name"`
	Color             string `json:"color,omitempty"`
	Enabled           bool   `json:"enabled,omitempty"`
}
