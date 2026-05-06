package favro

import (
	"context"
)

// Organization is a Favro organization. Fields outside this struct in
// the API response are ignored on decode (forward-compatible).
type Organization struct {
	OrganizationID string       `json:"organizationId"`
	Name           string       `json:"name"`
	Thumbnail      string       `json:"thumbnail,omitempty"`
	SharedToUsers  []SharedUser `json:"sharedToUsers,omitempty"`
}

// SharedUser is a (user, role) mapping that appears inside any Favro
// resource carrying access control — Organizations, Collections, and
// (later) Widgets / Cards. Favro returns the same shape on read; for
// writes (e.g. collection create / update) Favro accepts EITHER
// `email` OR `userId` to identify the user, so both are present and
// both `omitempty` so a caller passing only `Email` doesn't send an
// empty `userId` (Favro 400s on `{"userId":"","role":"..."}`).
//
// JoinDate is documented only on org members but is included here
// because the type is reused; absent fields decode as the zero
// string and are dropped on re-serialization by omitempty.
type SharedUser struct {
	Email    string `json:"email,omitempty"`
	UserID   string `json:"userId,omitempty"`
	Role     string `json:"role,omitempty"`
	JoinDate string `json:"joinDate,omitempty"`
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
