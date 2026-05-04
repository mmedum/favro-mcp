package favro

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// Paginate drives Favro's two-step pagination protocol: the first
// page is requested with no cursor; subsequent pages send the
// requestId from the prior response back as the
// X-Favro-Backend-Identifier header alongside ?page=N.
//
// The visit callback runs once per response page with the parsed
// envelope. Returning a non-nil error stops the iteration. An empty
// page is still a valid page and triggers visit.
//
// GET-only by construction: every Favro paginated endpoint is a GET.
//
// Pagination is a top-level helper rather than an auto-collector
// because silently iterating every page is the fastest way to burn
// the per-organization rate-limit budget — call sites must opt in
// explicitly.
func Paginate[T any](
	ctx context.Context,
	c *Client,
	path string,
	query url.Values,
	visit func(PageEnvelope[T]) error,
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
// non-empty, rides as X-Favro-Backend-Identifier so Favro routes the
// call to the backend that holds the cursor.
func fetchOne[T any](
	ctx context.Context,
	c *Client,
	path string,
	query url.Values,
	requestID string,
) (PageEnvelope[T], error) {
	var opts []RequestOption
	if requestID != "" {
		opts = append(opts, WithHeader(headerRequestID, requestID))
	}
	var env PageEnvelope[T]
	if err := c.GetJSON(ctx, path, query, &env, opts...); err != nil {
		return PageEnvelope[T]{}, err
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
