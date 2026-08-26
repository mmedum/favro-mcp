package favro

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

// errMissingCardCommonID is returned by ListComments when the caller
// passes an empty cardCommonID. Favro's /comments endpoint scopes
// comments to a single card via the required `cardCommonId` query
// parameter (verified against API docs); short-circuiting locally
// surfaces the requirement before any HTTP round-trip.
var errMissingCardCommonID = errors.New("favro: card_common_id is required for listing comments")

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

// ListComments returns one page of comments on the card identified
// by cardCommonID. The cardCommonID is mandatory — Favro's /comments
// endpoint requires the filter; an unfiltered listing would return
// every comment in the org and is not supported.
//
// Callers that paginate must pass cardCommonID on every page;
// dropping the filter mid-pagination would silently switch the
// result set to a 400 from Favro.
func (c *Client) ListComments(ctx context.Context, page int, requestID, cardCommonID string) (PageEnvelope[Comment], error) {
	if cardCommonID == "" {
		return PageEnvelope[Comment]{}, errMissingCardCommonID
	}
	q := url.Values{}
	q.Set("cardCommonId", cardCommonID)
	return listPageQ[Comment](ctx, c, "/comments", q, page, requestID)
}

// GetComment returns a single comment by its commentId. Returns
// *NotFoundError if no such comment exists in the active organization.
func (c *Client) GetComment(ctx context.Context, commentID string) (Comment, error) {
	return getByID[Comment](ctx, c, "/comments", commentID)
}

// CreateCommentRequest is the body for POST /comments. Both fields
// are required: cardCommonId scopes the comment to a card, and
// `comment` carries the markdown body.
type CreateCommentRequest struct {
	CardCommonID string `json:"cardCommonId"`
	Comment      string `json:"comment"`
}

// CreateComment creates a new comment on the card identified by
// CardCommonID. Returns the created Comment (Favro echoes the row
// back with its assigned commentId, userId, timestamps).
func (c *Client) CreateComment(ctx context.Context, req CreateCommentRequest) (Comment, error) {
	if req.CardCommonID == "" {
		return Comment{}, fmt.Errorf("favro: card_common_id is required")
	}
	if req.Comment == "" {
		return Comment{}, fmt.Errorf("favro: comment body is required")
	}
	var out Comment
	if err := c.PostJSON(ctx, "/comments", req, &out); err != nil {
		return Comment{}, err
	}
	return out, nil
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

// UpdateComment updates the body of an existing comment. Returns
// the updated Comment with refreshed lastUpdated. Empty commentID
// short-circuits with errMissingID; an empty comment body is
// rejected client-side because Favro returns a 400 anyway.
func (c *Client) UpdateComment(ctx context.Context, commentID string, req UpdateCommentRequest) (Comment, error) {
	if commentID == "" {
		return Comment{}, errMissingID
	}
	if req.Comment == "" {
		return Comment{}, fmt.Errorf("favro: comment body is required")
	}
	// A presigned URL can never match what Favro stored — see
	// CanonicalAttachmentURL. Copy rather than mutate the caller's
	// slice in place.
	if len(req.RemoveAttachments) > 0 {
		canonical := make([]string, len(req.RemoveAttachments))
		for i, u := range req.RemoveAttachments {
			canonical[i] = CanonicalAttachmentURL(u)
		}
		req.RemoveAttachments = canonical
	}
	var out Comment
	if err := c.PutJSON(ctx, "/comments/"+url.PathEscape(commentID), req, &out); err != nil {
		return Comment{}, err
	}
	return out, nil
}

// DeleteComment deletes a comment by its commentId. Returns
// errMissingID for an empty id (no network call), *NotFoundError on
// a Favro 404, and the same typed errors as Do for other failures.
func (c *Client) DeleteComment(ctx context.Context, commentID string) error {
	return deleteByID(ctx, c, "/comments", commentID)
}
