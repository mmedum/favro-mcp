package favroapi

import (
	"context"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// ListCustomFields returns one page of custom fields in the active
// organization. Custom fields are org-global; there is no widget
// or card filter.
func (c *Client) ListCustomFields(ctx context.Context, page int, requestID string) (favro.PageEnvelope[favro.CustomField], error) {
	return listPage[favro.CustomField](ctx, c, "/customfields", page, requestID)
}

// GetCustomField returns a single custom field by its
// customFieldId. Returns *NotFoundError if no such custom field
// exists in the active organization.
func (c *Client) GetCustomField(ctx context.Context, customFieldID string) (favro.CustomField, error) {
	return getByID[favro.CustomField](ctx, c, "/customfields", customFieldID)
}
