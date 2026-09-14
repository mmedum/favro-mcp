package favro

import "net/url"

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

// CreateTagRequest is the body for POST /tags. Name is required;
// Color is optional (Favro picks a random palette color when
// omitted, per the API docs).
type CreateTagRequest struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// UpdateTagRequest is the body for PUT /tags/{tagId}. Both Name and
// Color are optional — Favro accepts updating either, both, or
// neither (per the API docs); the caller is responsible for sending
// at least one if they expect a change.
type UpdateTagRequest struct {
	Name  string `json:"name,omitempty"`
	Color string `json:"color,omitempty"`
}

// BulkTagUpdate is one entry in an UpdateTags bulk-write request.
// TagID identifies the tag to update; Name and Color are optional —
// at least one should be set on each entry to make a meaningful
// change. The wire shape mirrors the single-tag UpdateTagRequest
// (plus a tagId) so callers can compose bulk requests by pairing a
// resolved id with the same field set they'd pass to UpdateTag.
type BulkTagUpdate struct {
	TagID string `json:"tagId"`
	Name  string `json:"name,omitempty"`
	Color string `json:"color,omitempty"`
}
