package favroapi

import (
	"context"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// ListOrganizations returns one page of organizations the API token
// can see. See listPage for the pagination contract.
func (c *Client) ListOrganizations(ctx context.Context, page int, requestID string) (favro.PageEnvelope[favro.Organization], error) {
	return listPage[favro.Organization](ctx, c, "/organizations", page, requestID)
}

// GetOrganization returns a single organization by id. Returns
// *NotFoundError if no such organization exists for the active token.
func (c *Client) GetOrganization(ctx context.Context, organizationID string) (favro.Organization, error) {
	return getByID[favro.Organization](ctx, c, "/organizations", organizationID)
}
