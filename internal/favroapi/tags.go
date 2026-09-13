package favroapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"golang.org/x/sync/errgroup"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// updateTagsConcurrency caps the parallel PUT /tags/{tagId} calls
// UpdateTags issues. Bounded so a wide bulk doesn't burn the
// rate-limit budget all at once; Favro has no real bulk-tag
// endpoint so the bulk surface is a client-side fan-out.
const updateTagsConcurrency = 4

// ListTags returns one page of tags in the active organization. Tags
// are org-global; only `name` is filterable server-side.
func (c *Client) ListTags(ctx context.Context, page int, requestID string, filter favro.ListTagsFilter) (favro.PageEnvelope[favro.Tag], error) {
	return listPageQ[favro.Tag](ctx, c, "/tags", filter.Values(), page, requestID)
}

// GetTag returns a single tag by its tagId. Returns *NotFoundError
// if no such tag exists in the active organization.
func (c *Client) GetTag(ctx context.Context, tagID string) (favro.Tag, error) {
	return getByID[favro.Tag](ctx, c, "/tags", tagID)
}

// CreateTag creates a new tag in the active organization. Returns
// the created favro.Tag (Favro echoes the row back with its assigned
// tagId). Honors per-context WithDryRun and process-wide
// ForceDryRun via the wrapped PostJSON; in either case the call
// returns *DryRunRecord wrapped in ErrDryRun without touching the
// network.
func (c *Client) CreateTag(ctx context.Context, req favro.CreateTagRequest) (favro.Tag, error) {
	if req.Name == "" {
		return favro.Tag{}, fmt.Errorf("favro: tag name is required")
	}
	var out favro.Tag
	if err := c.PostJSON(ctx, "/tags", req, &out); err != nil {
		return favro.Tag{}, err
	}
	return out, nil
}

// DeleteTag deletes a tag by its tagId. Returns errMissingID for an
// empty id (no network call), *NotFoundError on a Favro 404, and the
// same typed errors as Do for other failures. Honors WithDryRun /
// ForceDryRun via the wrapped DeleteJSON.
func (c *Client) DeleteTag(ctx context.Context, tagID string) error {
	return deleteByID(ctx, c, "/tags", tagID)
}

// UpdateTag updates an existing tag's name and/or color. Returns
// the updated favro.Tag. Empty tagID short-circuits with errMissingID;
// other Favro errors propagate via the wrapped PutJSON, including
// *DryRunRecord wrapping ErrDryRun while dry-run is in effect.
func (c *Client) UpdateTag(ctx context.Context, tagID string, req favro.UpdateTagRequest) (favro.Tag, error) {
	if tagID == "" {
		return favro.Tag{}, errMissingID
	}
	var out favro.Tag
	if err := c.PutJSON(ctx, "/tags/"+url.PathEscape(tagID), req, &out); err != nil {
		return favro.Tag{}, err
	}
	return out, nil
}

// UpdateTags applies multiple tag updates concurrently. Favro does
// not expose a true bulk endpoint — `PUT /tags` (no tagId) returns
// the SPA fallback HTML page — so this is a client-side fan-out:
// one `PUT /tags/{tagId}` per entry, dispatched in parallel via
// errgroup with a small concurrency cap.
//
// Returned `[]favro.Tag` is in input order. Validation short-circuits
// before any HTTP work: empty updates returns an error, and any
// entry missing TagID names the offending index. On the first
// per-entry error errgroup cancels the rest; partial successes may
// have already landed on Favro — the wrapped error names the
// offending tagId and index so callers can recover.
//
// Honors per-context WithDryRun and process-wide ForceDryRun by
// synthesizing a single conceptual *DryRunRecord and returning it
// wrapped in ErrDryRun without dispatching any HTTP work.
func (c *Client) UpdateTags(ctx context.Context, updates []favro.BulkTagUpdate) ([]favro.Tag, error) {
	if len(updates) == 0 {
		return nil, fmt.Errorf("favro: at least one tag update is required")
	}
	for i, u := range updates {
		if u.TagID == "" {
			return nil, fmt.Errorf("favro: bulk tag update at index %d missing tagId", i)
		}
	}
	if shouldDryRun(c, ctx, http.MethodPut) {
		return nil, c.buildBulkTagUpdateDryRun(ctx, updates)
	}
	out := make([]favro.Tag, len(updates))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(updateTagsConcurrency)
	for i, u := range updates {
		g.Go(func() error {
			t, err := c.UpdateTag(gctx, u.TagID, favro.UpdateTagRequest{Name: u.Name, Color: u.Color})
			if err != nil {
				return fmt.Errorf("favro: bulk update tag %q (index %d): %w", u.TagID, i, err)
			}
			out[i] = t
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

// buildBulkTagUpdateDryRun synthesizes a single *DryRunRecord
// representing the whole bulk as one conceptual PUT. The URL names
// the per-tag fan-out and parallel count so the LLM sees the real
// wire pattern; body is the input array (informational — there is
// no literal bulk-request payload because Favro has no bulk endpoint).
// Header composition is delegated to buildDryRunRecord so the
// redaction + Content-Type rules stay in one place.
func (c *Client) buildBulkTagUpdateDryRun(ctx context.Context, updates []favro.BulkTagUpdate) *DryRunRecord {
	body, _ := json.Marshal(updates)
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	url := fmt.Sprintf("%s/tags/{tagId} × %d (client-side parallel fan-out)", base, len(updates))
	return c.buildDryRunRecord(ctx, http.MethodPut, url, body, nil)
}
