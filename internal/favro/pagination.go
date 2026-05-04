package favro

import (
	"context"
	"encoding/json"
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
// envelope. Returning a non-nil error stops the iteration and
// surfaces the error to the caller. A non-nil envelope with no
// entities is a valid empty page and still triggers visit.
//
// The starting query (initial filters) is sent on every page; only
// the page index and X-Favro-Backend-Identifier header change between
// requests.
//
// Pagination is a top-level helper rather than an auto-collector
// because silently iterating every page is the fastest way to burn
// the per-organization rate-limit budget.
func Paginate[T any](
	ctx context.Context,
	c *Client,
	method, path string,
	query url.Values,
	body any,
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

		env, err := fetchOne[T](ctx, c, method, path, q, body, requestID)
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

// fetchOne sends one request and decodes the page envelope. The
// requestID, if non-empty, is sent as X-Favro-Backend-Identifier so
// Favro routes the call to the same backend that holds the cursor.
func fetchOne[T any](
	ctx context.Context,
	c *Client,
	method, path string,
	query url.Values,
	body any,
	requestID string,
) (PageEnvelope[T], error) {
	var opts []RequestOption
	if requestID != "" {
		opts = append(opts, WithHeader(headerRequestID, requestID))
	}
	resp, err := c.Do(ctx, method, path, query, body, opts...)
	if err != nil {
		return PageEnvelope[T]{}, err
	}
	defer drainAndClose(resp)

	var env PageEnvelope[T]
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return PageEnvelope[T]{}, fmt.Errorf("favro: decode page envelope: %w", err)
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
