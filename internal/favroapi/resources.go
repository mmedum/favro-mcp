package favroapi

import (
	"context"
	"net/url"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
	"github.com/mmedum/favro-mcp/v4/internal/render"
)

// errMissingID is returned by getByID (and the resource Get<X> methods
// that compose it) when the caller passes an empty id. The MCP layer
// relies on the SDK's own schema validation (required fields), so
// this sentinel mainly catches direct in-process callers.
var errMissingID = render.Sentinel(render.ClassInvalid, "favro: id is required")

// listPage requests one paginated page of T from path; see fetchOne for
// how pg reaches Favro.
//
// Pagination is intentionally not auto-aggregated; per-resource list
// methods expose pages explicitly because silent multi-page reads
// burn the hourly rate limit.
func listPage[T any](ctx context.Context, c *Client, path string, pg favro.Page) (favro.PageEnvelope[T], error) {
	return listPageQ[T](ctx, c, path, nil, pg)
}

// listPageQ is listPage with a caller-supplied query. Use this when
// the endpoint accepts filters (e.g. /widgets?collectionId=…). q may
// be nil or empty. The caller must NOT pre-set "page", "requestId" or
// "limit" — fetchOne owns those keys.
func listPageQ[T any](ctx context.Context, c *Client, path string, q url.Values, pg favro.Page) (favro.PageEnvelope[T], error) {
	return fetchOne[T](ctx, c, path, cloneQuery(q), pg)
}

// getByID fetches a single resource by id from basePath/{id}. id is
// URL-path-escaped. Returns errMissingID for empty id (no network
// call), *NotFoundError on 404, and the same typed errors as Do for
// any other failure.
func getByID[T any](ctx context.Context, c *Client, basePath, id string) (T, error) {
	var zero T
	if id == "" {
		return zero, errMissingID
	}
	var out T
	if err := c.GetJSON(ctx, basePath+"/"+url.PathEscape(id), nil, &out); err != nil {
		return zero, err
	}
	return out, nil
}

// deleteByID issues a DELETE on basePath/{id}. id is URL-path-escaped.
// Returns errMissingID for empty id (no network call), *NotFoundError
// on 404, and the same typed errors as Do (including the
// *DryRunRecord-wrapping-ErrDryRun returned when the dry-run gate is
// in effect). Most Favro DELETE endpoints return 204 No Content; we
// pass nil as the decode target.
func deleteByID(ctx context.Context, c *Client, basePath, id string) error {
	if id == "" {
		return errMissingID
	}
	return c.DeleteJSON(ctx, basePath+"/"+url.PathEscape(id), nil)
}
