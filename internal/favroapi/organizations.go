package favroapi

import (
	"context"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
)

// ListOrganizations returns one page of organizations the API token
// can see. See listPage for the pagination contract.
func (c *Client) ListOrganizations(ctx context.Context, page int, requestID string) (favro.PageEnvelope[favro.Organization], error) {
	return listPage[favro.Organization](ctx, c, "/organizations", page, requestID)
}

// GetOrganization returns the organization the organizationId header
// names: Favro answers GET /organizations/{id} for that organization
// whatever id the path carries (see auth.Token.Apply).
func (c *Client) GetOrganization(ctx context.Context, organizationID string) (favro.Organization, error) {
	return getByID[favro.Organization](ctx, c, "/organizations", organizationID)
}
