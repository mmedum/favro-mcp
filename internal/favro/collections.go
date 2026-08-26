package favro

import (
	"context"
	"fmt"
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
	// PublicSharing is one of "users", "organization", "public" per
	// Favro's docs. Left as a string because Favro could extend the
	// set without notice and a typed enum would silently mask new
	// values.
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

// CreateCollectionRequest is the body for POST /collections. Name is
// required; the rest are optional.
//
// Note the asymmetry with the read shape: a Collection comes back
// with its members in `sharedToUsers`, but invites are *sent* in
// `shareToUsers` (no "d"). They are different wire keys for the same
// concept, and posting the read-shaped key silently drops the
// invites.
//
// Color and IconName are not in Favro's documented parameter list.
// They are kept because earlier phases sent them and Favro accepts
// the body; live observation in Phase 5.4 was that `color` is
// ignored on write and Favro derives `background` instead.
type CreateCollectionRequest struct {
	Name                     string       `json:"name"`
	ShareToUsers             []SharedUser `json:"shareToUsers,omitempty"`
	PublicSharing            string       `json:"publicSharing,omitempty"`
	Background               string       `json:"background,omitempty"`
	StarPage                 *bool        `json:"starPage,omitempty"`
	Color                    string       `json:"color,omitempty"`
	IconName                 string       `json:"iconName,omitempty"`
	FullMembersCanAddWidgets *bool        `json:"fullMembersCanAddWidgets,omitempty"`
}

// CreateCollection creates a new collection. Returns the created
// Collection (Favro echoes the row back with collectionId assigned).
func (c *Client) CreateCollection(ctx context.Context, req CreateCollectionRequest) (Collection, error) {
	if req.Name == "" {
		return Collection{}, fmt.Errorf("favro: collection name is required")
	}
	var out Collection
	if err := c.PostJSON(ctx, "/collections", req, &out); err != nil {
		return Collection{}, err
	}
	return out, nil
}

// UpdateCollectionRequest is the body for PUT /collections/{collectionId}.
// Every field is optional; absent ones are left untouched. Archive
// is *bool so &false (unarchive) is distinguishable from "don't touch".
//
// Membership changes travel through two distinct keys, matching
// Favro's documented parameter list: ShareToUsers *invites* people
// who aren't in the collection yet, while Members updates the role
// of — or, with Delete set, removes — someone already in it. Neither
// is the read-shaped `sharedToUsers` key, which Favro ignores here.
type UpdateCollectionRequest struct {
	Name                     string       `json:"name,omitempty"`
	PublicSharing            string       `json:"publicSharing,omitempty"`
	Background               string       `json:"background,omitempty"`
	StarPage                 *bool        `json:"starPage,omitempty"`
	Color                    string       `json:"color,omitempty"`
	IconName                 string       `json:"iconName,omitempty"`
	FullMembersCanAddWidgets *bool        `json:"fullMembersCanAddWidgets,omitempty"`
	ShareToUsers             []SharedUser `json:"shareToUsers,omitempty"`
	Members                  []SharedUser `json:"members,omitempty"`
	Archive                  *bool        `json:"archive,omitempty"`
}

// UpdateCollection updates a collection by id. Returns the updated
// Collection. Empty collectionID short-circuits with errMissingID.
func (c *Client) UpdateCollection(ctx context.Context, collectionID string, req UpdateCollectionRequest) (Collection, error) {
	if collectionID == "" {
		return Collection{}, errMissingID
	}
	var out Collection
	if err := c.PutJSON(ctx, "/collections/"+url.PathEscape(collectionID), req, &out); err != nil {
		return Collection{}, err
	}
	return out, nil
}

// DeleteCollection deletes a collection by id. Favro returns 204 No
// Content on success. Empty collectionID short-circuits with
// errMissingID; *NotFoundError on 404. Honors WithDryRun and
// ForceDryRun via the wrapped DeleteJSON.
func (c *Client) DeleteCollection(ctx context.Context, collectionID string) error {
	return deleteByID(ctx, c, "/collections", collectionID)
}
