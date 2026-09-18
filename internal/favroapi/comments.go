package favroapi

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mmedum/favro-mcp/internal/favro"
	"github.com/mmedum/favro-mcp/internal/render"
)

// errMissingCardCommonID is returned by ListComments when the caller
// passes an empty cardCommonID. Favro's /comments endpoint scopes
// comments to a single card via the required `cardCommonId` query
// parameter (verified against API docs); short-circuiting locally
// surfaces the requirement before any HTTP round-trip.
var errMissingCardCommonID = render.Sentinel(render.ClassInvalid, "favro: card_common_id is required for listing comments")

// ListComments returns one page of comments on the card identified
// by cardCommonID. The cardCommonID is mandatory — Favro's /comments
// endpoint requires the filter; an unfiltered listing would return
// every comment in the org and is not supported.
//
// Callers that paginate must pass cardCommonID on every page;
// dropping the filter mid-pagination would silently switch the
// result set to a 400 from Favro.
func (c *Client) ListComments(ctx context.Context, page int, requestID, cardCommonID string) (favro.PageEnvelope[favro.Comment], error) {
	if cardCommonID == "" {
		return favro.PageEnvelope[favro.Comment]{}, errMissingCardCommonID
	}
	q := url.Values{}
	q.Set("cardCommonId", cardCommonID)
	return listPageQ[favro.Comment](ctx, c, "/comments", q, page, requestID)
}

// GetComment returns a single comment by its commentId. Returns
// *NotFoundError if no such comment exists in the active organization.
func (c *Client) GetComment(ctx context.Context, commentID string) (favro.Comment, error) {
	return getByID[favro.Comment](ctx, c, "/comments", commentID)
}

// CreateComment creates a new comment on the card identified by
// CardCommonID. Returns the created favro.Comment (Favro echoes the row
// back with its assigned commentId, userId, timestamps).
func (c *Client) CreateComment(ctx context.Context, req favro.CreateCommentRequest) (favro.Comment, error) {
	if req.CardCommonID == "" {
		return favro.Comment{}, fmt.Errorf("favro: card_common_id is required")
	}
	if req.Comment == "" {
		return favro.Comment{}, fmt.Errorf("favro: comment body is required")
	}
	var out favro.Comment
	if err := c.PostJSON(ctx, "/comments", req, &out); err != nil {
		return favro.Comment{}, err
	}
	return out, nil
}

// UpdateComment updates the body of an existing comment. Returns
// the updated favro.Comment with refreshed lastUpdated. Empty commentID
// short-circuits with errMissingID; an empty comment body is
// rejected client-side because Favro returns a 400 anyway.
func (c *Client) UpdateComment(ctx context.Context, commentID string, req favro.UpdateCommentRequest) (favro.Comment, error) {
	if commentID == "" {
		return favro.Comment{}, errMissingID
	}
	if req.Comment == "" {
		return favro.Comment{}, fmt.Errorf("favro: comment body is required")
	}

	if len(req.RemoveAttachments) > 0 {
		canonical := make([]string, len(req.RemoveAttachments))
		for i, u := range req.RemoveAttachments {
			canonical[i] = favro.CanonicalAttachmentURL(u)
		}
		req.RemoveAttachments = canonical
	}
	var out favro.Comment
	if err := c.PutJSON(ctx, "/comments/"+url.PathEscape(commentID), req, &out); err != nil {
		return favro.Comment{}, err
	}
	return out, nil
}

// DeleteComment deletes a comment by its commentId. Returns
// errMissingID for an empty id (no network call), *NotFoundError on
// a Favro 404, and the same typed errors as Do for other failures.
func (c *Client) DeleteComment(ctx context.Context, commentID string) error {
	return deleteByID(ctx, c, "/comments", commentID)
}
