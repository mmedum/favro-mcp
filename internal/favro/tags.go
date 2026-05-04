package favro

import "context"

// Tag is a Favro tag — org-global metadata applied to cards. Tags
// are not scoped to a widget or collection; one tag can appear on
// any card in the org. The plan's tag-resolution workflow
// (Phase 4) leans on the org-global property to cache the full tag
// list and resolve names with a single round-trip.
//
// Color is one of Favro's named palette values ("blue", "red",
// "green", "lime", "purple", "cyan", "brown", "orange", "gray",
// "pink", "yellow") — kept as a plain string because Favro extends
// the palette without notice and a typed alias would silently mask
// new values.
type Tag struct {
	TagID          string `json:"tagId"`
	OrganizationID string `json:"organizationId,omitempty"`
	Name           string `json:"name"`
	Color          string `json:"color,omitempty"`
}

// ListTags returns one page of tags in the active organization. Tags
// are org-global; there is no widget or card filter.
func (c *Client) ListTags(ctx context.Context, page int, requestID string) (PageEnvelope[Tag], error) {
	return listPage[Tag](ctx, c, "/tags", page, requestID)
}

// GetTag returns a single tag by its tagId. Returns *NotFoundError
// if no such tag exists in the active organization.
func (c *Client) GetTag(ctx context.Context, tagID string) (Tag, error) {
	return getByID[Tag](ctx, c, "/tags", tagID)
}
