package favroapi

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
)

// Paginate reads a listing page by page. The first page is requested
// with no page parameter; each later one sends ?page=N and puts the
// previous response's requestId in the X-Favro-Backend-Identifier
// header.
//
// Favro serves a later page from page alone. The requestId is not the
// backend identifier a response returns in that header, and sending it
// there changes nothing. The reference asks for requestId as a query
// parameter beside page, and for the header to carry the value a
// response returned in it. A card listing's second page holds the same
// rows in the reference's form, in the client's and with page alone.
// Checked by hand against a live organization; no live step reads a
// second page.
// https://favro.com/developer/#pagination
// https://favro.com/developer/#routing
//
// The visit callback runs once per response page with the parsed
// envelope. Returning a non-nil error stops the iteration. An empty
// page is still a valid page and triggers visit.
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

	q := cloneQuery(query)
	requestID := ""
	page := 0
	for {
		if requestID != "" {
			q.Set("page", strconv.Itoa(page))
		} else {
			q.Del("page")
		}

		env, err := fetchOne[T](ctx, c, path, q, requestID)
		if err != nil {
			return err
		}

		if err := visit(env); err != nil {
			return err
		}

		if !env.HasNextPage() {
			return nil
		}
		requestID = env.RequestID
		page = env.Page + 1
	}
}

// fetchOne sends one page request via GetJSON. The requestID, if
// non-empty, is sent as the X-Favro-Backend-Identifier header, which
// changes nothing Favro serves; see Paginate.
func fetchOne[T any](
	ctx context.Context,
	c *Client,
	path string,
	query url.Values,
	requestID string,
) (favro.PageEnvelope[T], error) {
	var opts []RequestOption
	if requestID != "" {
		opts = append(opts, WithHeader(headerRequestID, requestID))
	}
	var env favro.PageEnvelope[T]
	if err := c.GetJSON(ctx, path, query, &env, opts...); err != nil {
		return favro.PageEnvelope[T]{}, err
	}
	return env, nil
}

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
