package favroapi

import (
	"context"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// ListUsers returns one page of users (members of the active
// organization). See listPage for the pagination contract.
func (c *Client) ListUsers(ctx context.Context, page int, requestID string) (favro.PageEnvelope[favro.User], error) {
	return listPage[favro.User](ctx, c, "/users", page, requestID)
}

// GetUser returns a single user by id. Returns *NotFoundError if no
// such user exists in the active organization.
func (c *Client) GetUser(ctx context.Context, userID string) (favro.User, error) {
	return getByID[favro.User](ctx, c, "/users", userID)
}
