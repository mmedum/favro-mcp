package favroapi

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// ListGroups returns one page of groups in the active organization.
// Groups are org-global; there is no widget or card filter.
func (c *Client) ListGroups(ctx context.Context, page int, requestID string) (favro.PageEnvelope[favro.Group], error) {
	return listPage[favro.Group](ctx, c, "/groups", page, requestID)
}

// GetGroup returns a single group by its groupId. Returns
// *NotFoundError if no such group exists in the active organization.
func (c *Client) GetGroup(ctx context.Context, groupID string) (favro.Group, error) {
	return getByID[favro.Group](ctx, c, "/groups", groupID)
}

// CreateGroup creates a new org-global group. Returns the created
// favro.Group (Favro echoes the row back with groupId assigned).
func (c *Client) CreateGroup(ctx context.Context, req favro.CreateGroupRequest) (favro.Group, error) {
	if req.Name == "" {
		return favro.Group{}, fmt.Errorf("favro: group name is required")
	}
	var out favro.Group
	if err := c.PostJSON(ctx, "/groups", req, &out); err != nil {
		return favro.Group{}, err
	}
	return out, nil
}

// UpdateGroup updates a group by id. Returns the updated favro.Group.
// Empty groupID short-circuits with errMissingID.
func (c *Client) UpdateGroup(ctx context.Context, groupID string, req favro.UpdateGroupRequest) (favro.Group, error) {
	if groupID == "" {
		return favro.Group{}, errMissingID
	}
	var out favro.Group
	if err := c.PutJSON(ctx, "/groups/"+url.PathEscape(groupID), req, &out); err != nil {
		return favro.Group{}, err
	}
	return out, nil
}

// DeleteGroup deletes a group by id. Empty groupID short-circuits
// with errMissingID; *NotFoundError on 404. Honors WithDryRun /
// ForceDryRun via the wrapped DeleteJSON.
func (c *Client) DeleteGroup(ctx context.Context, groupID string) error {
	return deleteByID(ctx, c, "/groups", groupID)
}
