package favroapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
	"github.com/mmedum/favro-mcp/v4/internal/render"
)

// Paginate reads a listing page by page, each later page as fetchOne
// sends it. The visit callback runs once per response page with the
// parsed envelope. Returning a non-nil error stops the iteration. An
// empty page is still a valid page and triggers visit.
//
// GET-only by construction: every Favro paginated endpoint is a GET.
//
// Pagination is a top-level helper rather than an auto-collector
// because silently iterating every page is the fastest way to burn
// the hourly rate limit — call sites must opt in explicitly.
func Paginate[T any](
	ctx context.Context,
	c *Client,
	path string,
	query url.Values,
	visit func(favro.PageEnvelope[T]) error,
) error {
	if c == nil {
		return fmt.Errorf("favro: nil client")
	}
	if visit == nil {
		return fmt.Errorf("favro: nil visit callback")
	}

	var pg favro.Page
	for {
		env, err := fetchOne[T](ctx, c, path, cloneQuery(query), pg)
		if err != nil {
			return err
		}

		if err := visit(env); err != nil {
			return err
		}

		if !env.HasNextPage() {
			return nil
		}
		pg = favro.Page{Number: env.Page + 1, RequestID: env.RequestID}
	}
}

// fetchOne fetches one page of a paginated GET into a typed envelope,
// adding pg to q, which it owns. A later page sends `page` and the first
// page's `requestId` as query parameters, and the X-Favro-Backend-
// Identifier header the first page's response carried, so the same
// Favro process serves it. The reference asks for all three.
// https://favro.com/developer/#pagination
// https://favro.com/developer/#routing
//
// Favro has also served a later page from `page` alone, with neither the
// requestId nor the header, so a header this process never saw, after a
// restart, is left out rather than refused. Checked by the live step
// "read a second page", which reads one as the reference asks.
func fetchOne[T any](
	ctx context.Context,
	c *Client,
	path string,
	q url.Values,
	pg favro.Page,
) (favro.PageEnvelope[T], error) {
	if pg.Limit < 0 || pg.Limit > maxPageLimit {
		return favro.PageEnvelope[T]{}, errLimitOutOfRange
	}
	var env favro.PageEnvelope[T]
	var headers http.Header
	if err := c.GetJSON(ctx, path, q, &env, append(c.pageRouting(q, pg), withResponseHeaders(&headers))...); err != nil {
		return favro.PageEnvelope[T]{}, err
	}
	if backend := headers.Get(headerBackendID); env.RequestID != "" && backend != "" {
		c.backends.put(env.RequestID, backend)
	}
	return env, nil
}

// pageRouting adds pg's page, limit and request id to q, and returns the
// backend identifier header a later page sends. Only a later page sends
// the routing: on the first, a request id a caller carried over from
// another listing would name the wrong one.
func (c *Client) pageRouting(q url.Values, pg favro.Page) []RequestOption {
	if pg.Limit > 0 {
		q.Set("limit", strconv.Itoa(pg.Limit))
	}
	if pg.Number == 0 {
		return nil
	}
	q.Set("page", strconv.Itoa(pg.Number))
	if pg.RequestID == "" {
		return nil
	}
	q.Set("requestId", pg.RequestID)
	if backend := c.backends.get(pg.RequestID); backend != "" {
		return []RequestOption{WithHeader(headerBackendID, backend)}
	}
	return nil
}

// maxPageLimit is the largest page Favro serves; it caps a larger limit.
const maxPageLimit = 100

// errLimitOutOfRange refuses a page size Favro would not serve as asked:
// it reads a negative one as invalid and answers with its default, and
// caps one above 100.
var errLimitOutOfRange = render.Sentinel(render.ClassInvalid, "favro: limit must be 1 to 100, or omitted for 100")

// cloneQuery deep-copies q so callers' maps are not mutated.
func cloneQuery(q url.Values) url.Values {
	out := url.Values{}
	for k, v := range q {
		vv := make([]string, len(v))
		copy(vv, v)
		out[k] = vv
	}
	return out
}
