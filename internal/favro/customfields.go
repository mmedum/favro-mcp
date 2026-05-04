package favro

import "context"

// CustomField is a Favro custom field — org-global metadata that
// can be attached to any card, with the active value living on
// the card's CustomFieldsValues. Two reads matter for an LLM:
//
//   - The schema (this type): "what custom fields exist, what types
//     they are, and what the legal options are for select/status
//     types".
//   - The per-card value (Phase 4 / 6 — comes via the card payload's
//     customFieldsValues field, which references CustomFieldID +
//     CustomFieldItemID for select-flavored fields).
//
// Type is the human-facing display label Favro uses for the field
// kind — verified live values include "Text", "Number", "Date",
// "Date created", "Checkbox", "Single select", "Multiple select",
// "Members", "Tags", "Rating", "Link", "Progress", "Voting",
// "Relations", "Sequential ID", "Timeline". Favro extends this set
// without notice, so the field stays a plain string; a typed alias
// would silently mask new values. CustomFieldItems is populated only
// for select-flavored types ("Single select" / "Multiple select");
// for primitive types it's absent (omitempty drops the JSON field).
// Fields outside this struct are ignored on decode.
type CustomField struct {
	CustomFieldID    string            `json:"customFieldId"`
	OrganizationID   string            `json:"organizationId,omitempty"`
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

// ListCustomFields returns one page of custom fields in the active
// organization. Custom fields are org-global; there is no widget
// or card filter.
func (c *Client) ListCustomFields(ctx context.Context, page int, requestID string) (PageEnvelope[CustomField], error) {
	return listPage[CustomField](ctx, c, "/customfields", page, requestID)
}

// GetCustomField returns a single custom field by its
// customFieldId. Returns *NotFoundError if no such custom field
// exists in the active organization.
func (c *Client) GetCustomField(ctx context.Context, customFieldID string) (CustomField, error) {
	return getByID[CustomField](ctx, c, "/customfields", customFieldID)
}
