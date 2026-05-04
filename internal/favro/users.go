package favro

import (
	"context"
)

// User is a Favro user as returned by /users — a member of the
// organization the active token is scoped to. Fields outside this
// struct are ignored on decode (forward-compatible).
//
// Email is populated for users with email visible at the org level;
// it is the email of the *member*, not of the authenticated token
// owner. The "never log FAVRO_USER_EMAIL" rule targets the
// authenticated identity, not third-party member records the user
// already sees in the Favro UI.
//
// OrganizationRole is this user's role in the active organization;
// the org-wide membership map (every member + role) lives on
// Organization.SharedToUsers. Asymmetry mirrors Favro's API: a User
// dto returned at top level only knows its own role.
type User struct {
	UserID           string `json:"userId"`
	Name             string `json:"name,omitempty"`
	Email            string `json:"email,omitempty"`
	OrganizationRole string `json:"organizationRole,omitempty"`
}

// ListUsers returns one page of users (members of the active
// organization). See listPage for the pagination contract.
func (c *Client) ListUsers(ctx context.Context, page int, requestID string) (PageEnvelope[User], error) {
	return listPage[User](ctx, c, "/users", page, requestID)
}

// GetUser returns a single user by id. Returns *NotFoundError if no
// such user exists in the active organization.
func (c *Client) GetUser(ctx context.Context, userID string) (User, error) {
	return getByID[User](ctx, c, "/users", userID)
}
