package favro

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"golang.org/x/sync/errgroup"
)

// updateTagsConcurrency caps the parallel PUT /tags/{tagId} calls
// UpdateTags issues. Bounded so a wide bulk doesn't burn the
// rate-limit budget all at once; Favro has no real bulk-tag
// endpoint so the bulk surface is a client-side fan-out.
const updateTagsConcurrency = 4

// Tag is a Favro tag — org-global metadata applied to cards. Tags
// are not scoped to a widget or collection; one tag can appear on
// any card in the org. The plan's tag-resolution workflow
// (Phase 4) leans on the org-global property to cache the full tag
// list and resolve names with a single round-trip.
//
// Color is one of Favro's named palette values ("blue", "red",
// "green", "lime", "purple", "cyan", "brown", "orange", "gray",
// "pink", "yellow", "slategray") — kept as a plain string because
// Favro extends the palette without notice and a typed alias would
// silently mask new values.
type Tag struct {
	TagID          string `json:"tagId"`
	OrganizationID string `json:"organizationId,omitempty"`
	Name           string `json:"name"`
	Color          string `json:"color,omitempty"`
}

// ListTagsFilter bundles the documented query parameters for /tags.
// Name is Favro's server-side exact-match filter — useful for
// "add tag by name, hard-fail if missing" workflows without paying
// for a full tag-list scan.
type ListTagsFilter struct {
	Name string
}

// Values returns the filter as url.Values; empty fields are omitted.
func (f ListTagsFilter) Values() url.Values {
	q := url.Values{}
	if f.Name != "" {
		q.Set("name", f.Name)
	}
	return q
}

// ListTags returns one page of tags in the active organization. Tags
// are org-global; only `name` is filterable server-side.
func (c *Client) ListTags(ctx context.Context, page int, requestID string, filter ListTagsFilter) (PageEnvelope[Tag], error) {
	return listPageQ[Tag](ctx, c, "/tags", filter.Values(), page, requestID)
}

// GetTag returns a single tag by its tagId. Returns *NotFoundError
// if no such tag exists in the active organization.
func (c *Client) GetTag(ctx context.Context, tagID string) (Tag, error) {
	return getByID[Tag](ctx, c, "/tags", tagID)
}

// CreateTagRequest is the body for POST /tags. Name is required;
// Color is optional (Favro picks a random palette color when
// omitted, per the API docs).
type CreateTagRequest struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// CreateTag creates a new tag in the active organization. Returns
// the created Tag (Favro echoes the row back with its assigned
// tagId). Honors per-context WithDryRun and process-wide
// ForceDryRun via the wrapped PostJSON; in either case the call
// returns *DryRunRecord wrapped in ErrDryRun without touching the
// network.
func (c *Client) CreateTag(ctx context.Context, req CreateTagRequest) (Tag, error) {
	if req.Name == "" {
		return Tag{}, fmt.Errorf("favro: tag name is required")
	}
	var out Tag
	if err := c.PostJSON(ctx, "/tags", req, &out); err != nil {
		return Tag{}, err
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

// UpdateTagRequest is the body for PUT /tags/{tagId}. Both Name and
// Color are optional — Favro accepts updating either, both, or
// neither (per the API docs); the caller is responsible for sending
// at least one if they expect a change.
type UpdateTagRequest struct {
	Name  string `json:"name,omitempty"`
	Color string `json:"color,omitempty"`
}

// UpdateTag updates an existing tag's name and/or color. Returns
// the updated Tag. Empty tagID short-circuits with errMissingID;
// other Favro errors propagate via the wrapped PutJSON, including
// *DryRunRecord wrapping ErrDryRun while dry-run is in effect.
func (c *Client) UpdateTag(ctx context.Context, tagID string, req UpdateTagRequest) (Tag, error) {
	if tagID == "" {
		return Tag{}, errMissingID
	}
	var out Tag
	if err := c.PutJSON(ctx, "/tags/"+url.PathEscape(tagID), req, &out); err != nil {
		return Tag{}, err
	}
	return out, nil
}

// BulkTagUpdate is one entry in an UpdateTags bulk-write request.
// TagID identifies the tag to update; Name and Color are optional —
// at least one should be set on each entry to make a meaningful
// change. The wire shape mirrors the single-tag UpdateTagRequest
// (plus a tagId) so callers can compose bulk requests by pairing a
// resolved id with the same field set they'd pass to UpdateTag.
type BulkTagUpdate struct {
	TagID string `json:"tagId"`
	Name  string `json:"name,omitempty"`
	Color string `json:"color,omitempty"`
}

// UpdateTags applies multiple tag updates concurrently. Favro does
// not expose a true bulk endpoint — `PUT /tags` (no tagId) returns
// the SPA fallback HTML page — so this is a client-side fan-out:
// one `PUT /tags/{tagId}` per entry, dispatched in parallel via
// errgroup with a small concurrency cap.
//
// Returned `[]Tag` is in input order. Validation short-circuits
// before any HTTP work: empty updates returns an error, and any
// entry missing TagID names the offending index. On the first
// per-entry error errgroup cancels the rest; partial successes may
// have already landed on Favro — the wrapped error names the
// offending tagId and index so callers can recover.
//
// Honors per-context WithDryRun and process-wide ForceDryRun by
// synthesizing a single conceptual *DryRunRecord and returning it
// wrapped in ErrDryRun without dispatching any HTTP work.
func (c *Client) UpdateTags(ctx context.Context, updates []BulkTagUpdate) ([]Tag, error) {
	if len(updates) == 0 {
		return nil, fmt.Errorf("favro: at least one tag update is required")
	}
	for i, u := range updates {
		if u.TagID == "" {
			return nil, fmt.Errorf("favro: bulk tag update at index %d missing tagId", i)
		}
	}
	if shouldDryRun(c, ctx, http.MethodPut) {
		return nil, c.buildBulkTagUpdateDryRun(updates)
	}
	out := make([]Tag, len(updates))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(updateTagsConcurrency)
	for i, u := range updates {
		g.Go(func() error {
			t, err := c.UpdateTag(gctx, u.TagID, UpdateTagRequest{Name: u.Name, Color: u.Color})
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
func (c *Client) buildBulkTagUpdateDryRun(updates []BulkTagUpdate) *DryRunRecord {
	body, _ := json.Marshal(updates)
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	url := fmt.Sprintf("%s/tags/{tagId} × %d (client-side parallel fan-out)", base, len(updates))
	return c.buildDryRunRecord(http.MethodPut, url, body, nil)
}
