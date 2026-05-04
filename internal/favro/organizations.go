package favro

import (
	"context"
)

// Organization is a Favro organization. Fields outside this struct in
// the API response are ignored on decode (forward-compatible).
type Organization struct {
	OrganizationID string       `json:"organizationId"`
	Name           string       `json:"name"`
	SharedToUsers  []SharedUser `json:"sharedToUsers,omitempty"`
}

// SharedUser is a (userId, role) mapping that appears inside any
// Favro resource carrying access control — Organizations, Collections,
// and (later) Widgets / Cards. Favro returns the same shape on every
// such resource.
type SharedUser struct {
	UserID string `json:"userId"`
	Role   string `json:"role,omitempty"`
}

// ListOrganizations returns one page of organizations the API token
// can see. See listPage for the pagination contract.
func (c *Client) ListOrganizations(ctx context.Context, page int, requestID string) (PageEnvelope[Organization], error) {
	return listPage[Organization](ctx, c, "/organizations", page, requestID)
}

// GetOrganization returns a single organization by id. Returns
// *NotFoundError if no such organization exists for the active token.
func (c *Client) GetOrganization(ctx context.Context, organizationID string) (Organization, error) {
	return getByID[Organization](ctx, c, "/organizations", organizationID)
}
