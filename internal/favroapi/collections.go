package favroapi

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// ListCollections returns one page of collections in the active
// organization. Pass favro.ListCollectionsFilter{Archived: true} to
// include archived collections.
func (c *Client) ListCollections(ctx context.Context, page int, requestID string, filter favro.ListCollectionsFilter) (favro.PageEnvelope[favro.Collection], error) {
	return listPageQ[favro.Collection](ctx, c, "/collections", filter.Values(), page, requestID)
}

// GetCollection returns a single collection by id. Returns
// *NotFoundError if no such collection exists in the active org.
func (c *Client) GetCollection(ctx context.Context, collectionID string) (favro.Collection, error) {
	return getByID[favro.Collection](ctx, c, "/collections", collectionID)
}

// CreateCollection creates a new collection. Returns the created
// favro.Collection (Favro echoes the row back with collectionId assigned).
func (c *Client) CreateCollection(ctx context.Context, req favro.CreateCollectionRequest) (favro.Collection, error) {
	if req.Name == "" {
		return favro.Collection{}, fmt.Errorf("favro: collection name is required")
	}
	var out favro.Collection
	if err := c.PostJSON(ctx, "/collections", req, &out); err != nil {
		return favro.Collection{}, err
	}
	return out, nil
}

// UpdateCollection updates a collection by id. Returns the updated
// favro.Collection. Empty collectionID short-circuits with errMissingID.
func (c *Client) UpdateCollection(ctx context.Context, collectionID string, req favro.UpdateCollectionRequest) (favro.Collection, error) {
	if collectionID == "" {
		return favro.Collection{}, errMissingID
	}
	var out favro.Collection
	if err := c.PutJSON(ctx, "/collections/"+url.PathEscape(collectionID), req, &out); err != nil {
		return favro.Collection{}, err
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
