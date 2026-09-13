package favro

// Comment is a Favro comment on a card. Comments are scoped to a
// single CardCommonID (the cross-widget card identity) — the same
// comment thread is visible from every widget instance of the card.
//
// Body holds the comment text (markdown). LastUpdated is only set
// when the comment has been edited. Fields outside this struct are
// ignored on decode (forward-compatible).
type Comment struct {
	CommentID      string `json:"commentId"`
	OrganizationID string `json:"organizationId,omitempty"`
	CardCommonID   string `json:"cardCommonId"`
	UserID         string `json:"userId"`
	Body           string `json:"comment"`
	Created        string `json:"created,omitempty"`
	LastUpdated    string `json:"lastUpdated,omitempty"`
	// Attachments is the list of files on the comment. The shape
	// matches CardAttachment — Favro reuses one attachment object
	// shape across cards and comments.
	Attachments []CardAttachment `json:"attachments,omitempty"`
}

// CreateCommentRequest is the body for POST /comments. Both fields
// are required: cardCommonId scopes the comment to a card, and
// `comment` carries the markdown body.
type CreateCommentRequest struct {
	CardCommonID string `json:"cardCommonId"`
	Comment      string `json:"comment"`
}

// UpdateCommentRequest is the body for PUT /comments/{commentId}.
// cardCommonId and userId are fixed at creation time. Beyond the
// text, Favro accepts RemoveAttachments — a list of attachment URLs
// (Comment.Attachments[].FileURL) to detach from the comment.
//
// UpdateComment runs each entry through CanonicalAttachmentURL, so
// the presigned URL Favro hands back can be passed straight through.
type UpdateCommentRequest struct {
	Comment           string   `json:"comment"`
	RemoveAttachments []string `json:"removeAttachments,omitempty"`
}
