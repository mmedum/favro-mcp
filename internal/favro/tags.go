package favro

import (
	"context"
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
// "pink", "yellow") — kept as a plain string because Favro extends
// the palette without notice and a typed alias would silently mask
// new values.
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
