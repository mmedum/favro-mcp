package favro

import (
	"context"
	"errors"
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
