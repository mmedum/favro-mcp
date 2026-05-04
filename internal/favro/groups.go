package favro

import "context"

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
	Members        []GroupMember `json:"members,omitempty"`
}

// GroupMember is one user assigned to a group with a per-membership
// role.
type GroupMember struct {
	UserID string `json:"userId"`
	Role   string `json:"role,omitempty"`
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
