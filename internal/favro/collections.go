package favro

import (
	"context"
)

// Collection is a Favro collection — a named grouping of widgets
// (boards) inside an organization. Fields outside this struct are
// ignored on decode (forward-compatible).
type Collection struct {
	CollectionID  string       `json:"collectionId"`
	Name          string       `json:"name"`
	Color         string       `json:"color,omitempty"`
	SharedToUsers []SharedUser `json:"sharedToUsers,omitempty"`
	// PublicSharing is one of "off", "organization", "public" today.
	// Left as a string because Favro could extend the set without
	// notice and a typed enum would silently mask new values.
	PublicSharing           string `json:"publicSharing,omitempty"`
	Archived                bool   `json:"archived,omitempty"`
	FullMembersCanAddGuests bool   `json:"fullMembersCanAddGuests,omitempty"`
}

// ListCollections returns one page of collections in the active
// organization. See listPage for the pagination contract.
func (c *Client) ListCollections(ctx context.Context, page int, requestID string) (PageEnvelope[Collection], error) {
	return listPage[Collection](ctx, c, "/collections", page, requestID)
}

// GetCollection returns a single collection by id. Returns
// *NotFoundError if no such collection exists in the active org.
func (c *Client) GetCollection(ctx context.Context, collectionID string) (Collection, error) {
	return getByID[Collection](ctx, c, "/collections", collectionID)
}
