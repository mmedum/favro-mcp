package favro

// Organization is a Favro organization. Fields outside this struct in
// the API response are ignored on decode (forward-compatible).
type Organization struct {
	OrganizationID string       `json:"organizationId"`
	Name           string       `json:"name"`
	Thumbnail      string       `json:"thumbnail,omitempty"`
	SharedToUsers  []SharedUser `json:"sharedToUsers,omitempty"`
}

// SharedUser is a (member, role) mapping that appears inside any
// Favro resource carrying access control — Organizations,
// Collections, Widgets. Favro returns the same shape on read; for
// writes it accepts EITHER `email` OR `userId` to identify the
// member, so both are present and both `omitempty` — a caller
// passing only Email must not send an empty `userId` (Favro 400s on
// `{"userId":"","role":"..."}`).
//
// Type distinguishes a user share from a group or organization
// share; GroupID carries the target when Type is "group". Delete
// removes the member instead of re-roling them, and is only
// meaningful in an update's `members` list — when it is set, Role
// becomes optional.
//
// JoinDate is documented only on org members but is included here
// because the type is reused; absent fields decode as the zero
// string and are dropped on re-serialization by omitempty.
type SharedUser struct {
	// Type is "user", "group", or "organization". Empty means user.
	Type     string `json:"type,omitempty"`
	Email    string `json:"email,omitempty"`
	UserID   string `json:"userId,omitempty"`
	GroupID  string `json:"groupId,omitempty"`
	Role     string `json:"role,omitempty"`
	Delete   *bool  `json:"delete,omitempty"`
	JoinDate string `json:"joinDate,omitempty"`
}
