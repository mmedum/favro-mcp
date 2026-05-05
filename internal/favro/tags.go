package favro

import (
	"context"
	"fmt"
	"net/url"
)

// Tag is a Favro tag — org-global metadata applied to cards. Tags
// are not scoped to a widget or collection; one tag can appear on
// any card in the org. The plan's tag-resolution workflow
// (Phase 4) leans on the org-global property to cache the full tag
// list and resolve names with a single round-trip.
//
// Color is one of Favro's named palette values ("blue", "red",
// "green", "lime", "purple", "cyan", "brown", "orange", "gray",
// "pink", "yellow", "slategray") — kept as a plain string because
// Favro extends the palette without notice and a typed alias would
// silently mask new values.
type Tag struct {
	TagID          string `json:"tagId"`
	OrganizationID string `json:"organizationId,omitempty"`
	Name           string `json:"name"`
	Color          string `json:"color,omitempty"`
}

// ListTagsFilter bundles the documented query parameters for /tags.
// Name is Favro's server-side exact-match filter — useful for
// "add tag by name, hard-fail if missing" workflows without paying
// for a full tag-list scan.
type ListTagsFilter struct {
	Name string
}

// Values returns the filter as url.Values; empty fields are omitted.
func (f ListTagsFilter) Values() url.Values {
	q := url.Values{}
	if f.Name != "" {
		q.Set("name", f.Name)
	}
	return q
}

// ListTags returns one page of tags in the active organization. Tags
// are org-global; only `name` is filterable server-side.
func (c *Client) ListTags(ctx context.Context, page int, requestID string, filter ListTagsFilter) (PageEnvelope[Tag], error) {
	return listPageQ[Tag](ctx, c, "/tags", filter.Values(), page, requestID)
}

// GetTag returns a single tag by its tagId. Returns *NotFoundError
// if no such tag exists in the active organization.
func (c *Client) GetTag(ctx context.Context, tagID string) (Tag, error) {
	return getByID[Tag](ctx, c, "/tags", tagID)
}

// CreateTagRequest is the body for POST /tags. Name is required;
// Color is optional (Favro picks a random palette color when
// omitted, per the API docs).
type CreateTagRequest struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// CreateTag creates a new tag in the active organization. Returns
// the created Tag (Favro echoes the row back with its assigned
// tagId). Honors per-context WithDryRun and process-wide
// ForceDryRun via the wrapped PostJSON; in either case the call
// returns *DryRunRecord wrapped in ErrDryRun without touching the
// network.
func (c *Client) CreateTag(ctx context.Context, req CreateTagRequest) (Tag, error) {
	if req.Name == "" {
		return Tag{}, fmt.Errorf("favro: tag name is required")
	}
	var out Tag
	if err := c.PostJSON(ctx, "/tags", req, &out); err != nil {
		return Tag{}, err
	}
	return out, nil
}

// DeleteTag deletes a tag by its tagId. Returns errMissingID for an
// empty id (no network call), *NotFoundError on a Favro 404, and the
// same typed errors as Do for other failures. Honors WithDryRun /
// ForceDryRun via the wrapped DeleteJSON.
func (c *Client) DeleteTag(ctx context.Context, tagID string) error {
	return deleteByID(ctx, c, "/tags", tagID)
}
