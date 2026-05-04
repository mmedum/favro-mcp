package favro

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

// errMissingID is returned by getByID (and the resource Get<X> methods
// that compose it) when the caller passes an empty id. The MCP layer
// relies on the SDK's own schema validation (required fields), so
// this sentinel mainly catches direct in-process callers.
var errMissingID = errors.New("favro: id is required")

// listPage requests one paginated page of T from path. page is
// 1-indexed; pass 0 for "first page" (the absent-page Favro default).
// requestID, when non-empty, rides as X-Favro-Backend-Identifier so
// Favro routes the call to the backend that holds the cursor — which
// is required for any page > 0.
//
// Pagination is intentionally not auto-aggregated; per-resource list
// methods expose pages explicitly because silent multi-page reads
// burn the per-organization rate-limit budget.
func listPage[T any](ctx context.Context, c *Client, path string, page int, requestID string) (PageEnvelope[T], error) {
	return listPageQ[T](ctx, c, path, nil, page, requestID)
}

// listPageQ is listPage with a caller-supplied query. Use this when
// the endpoint accepts filters (e.g. /widgets?collection=…). q may
// be nil or empty; the helper will add ?page=N when page > 0. The
// caller must NOT pre-set "page" — listPageQ owns that key.
func listPageQ[T any](ctx context.Context, c *Client, path string, q url.Values, page int, requestID string) (PageEnvelope[T], error) {
	if q == nil {
		q = url.Values{}
	}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	return fetchOne[T](ctx, c, path, q, requestID)
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
