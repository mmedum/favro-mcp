package favro

import (
	"context"
	"fmt"
	"net/url"
)

// Group is a Favro group — an org-scoped named collection of users.
// Groups are org-global; one group can be referenced from anywhere
// in the org (sharing, assignments, custom-field "Members" values).
//
// Members is the per-user membership list; each entry pairs a
// userId with a role string. Role is kept as a plain string —
// Favro's documented set ("administrator", "member", "viewer") may
// extend without notice and a typed alias would silently mask new
// values. Fields outside this struct are ignored on decode
// (forward-compatible).
type Group struct {
	GroupID        string        `json:"groupId"`
	OrganizationID string        `json:"organizationId,omitempty"`
	Name           string        `json:"name"`
	CreatorUserID  string        `json:"creatorUserId,omitempty"`
	MemberCount    int           `json:"memberCount,omitempty"`
	Members        []GroupMember `json:"members,omitempty"`
}

// GroupMember is one member of a group. On read Favro fills UserID
// and Role. On write it accepts EITHER UserID or Email to identify
// the person, and Delete removes them instead of re-roling them —
// when Delete is set, Role becomes optional.
type GroupMember struct {
	UserID string `json:"userId,omitempty"`
	Email  string `json:"email,omitempty"`
	Role   string `json:"role,omitempty"`
	Delete *bool  `json:"delete,omitempty"`
}

// ListGroups returns one page of groups in the active organization.
// Groups are org-global; there is no widget or card filter.
func (c *Client) ListGroups(ctx context.Context, page int, requestID string) (PageEnvelope[Group], error) {
	return listPage[Group](ctx, c, "/groups", page, requestID)
}

// GetGroup returns a single group by its groupId. Returns
// *NotFoundError if no such group exists in the active organization.
func (c *Client) GetGroup(ctx context.Context, groupID string) (Group, error) {
	return getByID[Group](ctx, c, "/groups", groupID)
}

// CreateGroupRequest is the body for POST /groups. Name is required;
// Members is optional (Favro creates an empty group when omitted).
type CreateGroupRequest struct {
	Name    string        `json:"name"`
	Members []GroupMember `json:"members,omitempty"`
}

// CreateGroup creates a new org-global group. Returns the created
// Group (Favro echoes the row back with groupId assigned).
func (c *Client) CreateGroup(ctx context.Context, req CreateGroupRequest) (Group, error) {
	if req.Name == "" {
		return Group{}, fmt.Errorf("favro: group name is required")
	}
	var out Group
	if err := c.PostJSON(ctx, "/groups", req, &out); err != nil {
		return Group{}, err
	}
	return out, nil
}

// UpdateGroupRequest is the body for PUT /groups/{groupId}. Both
// fields are optional. Note: Members, when set, REPLACES the group's
// member list — Favro's update endpoint does not have add/remove
// semantics on this field.
type UpdateGroupRequest struct {
	Name    string        `json:"name,omitempty"`
	Members []GroupMember `json:"members,omitempty"`
}

// UpdateGroup updates a group by id. Returns the updated Group.
// Empty groupID short-circuits with errMissingID.
func (c *Client) UpdateGroup(ctx context.Context, groupID string, req UpdateGroupRequest) (Group, error) {
	if groupID == "" {
		return Group{}, errMissingID
	}
	var out Group
	if err := c.PutJSON(ctx, "/groups/"+url.PathEscape(groupID), req, &out); err != nil {
		return Group{}, err
	}
	return out, nil
}

// DeleteGroup deletes a group by id. Empty groupID short-circuits
// with errMissingID; *NotFoundError on 404. Honors WithDryRun /
// ForceDryRun via the wrapped DeleteJSON.
func (c *Client) DeleteGroup(ctx context.Context, groupID string) error {
	return deleteByID(ctx, c, "/groups", groupID)
}
