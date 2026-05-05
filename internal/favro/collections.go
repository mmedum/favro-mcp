package favro

import (
	"context"
	"net/url"
)

// Collection is a Favro collection — a named grouping of widgets
// (boards) inside an organization. Fields outside this struct are
// ignored on decode (forward-compatible).
type Collection struct {
	CollectionID   string       `json:"collectionId"`
	OrganizationID string       `json:"organizationId,omitempty"`
	Name           string       `json:"name"`
	Color          string       `json:"color,omitempty"`
	Background     string       `json:"background,omitempty"`
	SharedToUsers  []SharedUser `json:"sharedToUsers,omitempty"`
	// PublicSharing is one of "off", "organization", "public" today.
	// Left as a string because Favro could extend the set without
	// notice and a typed enum would silently mask new values.
	PublicSharing string `json:"publicSharing,omitempty"`
	Archived      bool   `json:"archived,omitempty"`
	// FullMembersCanAddWidgets is the docs-canonical field name,
	// confirmed live during Phase 4.5. Phase 3.3 originally shipped
	// `fullMembersCanAddGuests` which never matched the wire key —
	// removed in Phase 4.5.
	FullMembersCanAddWidgets bool `json:"fullMembersCanAddWidgets,omitempty"`
}

// ListCollectionsFilter bundles the documented query parameters for
// /collections. Only `archived` is documented; a zero filter
// requests every collection.
type ListCollectionsFilter struct {
	Archived bool
}

// Values returns the filter as url.Values; empty fields are omitted.
func (f ListCollectionsFilter) Values() url.Values {
	q := url.Values{}
	if f.Archived {
		q.Set("archived", "true")
	}
	return q
}

// ListCollections returns one page of collections in the active
// organization. Pass ListCollectionsFilter{Archived: true} to
// include archived collections.
func (c *Client) ListCollections(ctx context.Context, page int, requestID string, filter ListCollectionsFilter) (PageEnvelope[Collection], error) {
	return listPageQ[Collection](ctx, c, "/collections", filter.Values(), page, requestID)
}

// GetCollection returns a single collection by id. Returns
// *NotFoundError if no such collection exists in the active org.
func (c *Client) GetCollection(ctx context.Context, collectionID string) (Collection, error) {
	return getByID[Collection](ctx, c, "/collections", collectionID)
}
