package favro

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

// CreateGroupRequest is the body for POST /groups. Name is required;
// Members is optional (Favro creates an empty group when omitted).
type CreateGroupRequest struct {
	Name    string        `json:"name"`
	Members []GroupMember `json:"members,omitempty"`
}

// UpdateGroupRequest is the body for PUT /groups/{groupId}. Both
// fields are optional. Note: Members, when set, REPLACES the group's
// member list — Favro's update endpoint does not have add/remove
// semantics on this field.
type UpdateGroupRequest struct {
	Name    string        `json:"name,omitempty"`
	Members []GroupMember `json:"members,omitempty"`
}
