package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const updateTagsToolName = "favro_update_tags"

// updateTagsInput is the input for favro_update_tags.
type updateTagsInput struct {
	dryRunInput
	Updates []bulkTagUpdateInput `json:"updates" jsonschema:"the list of tag updates to apply (at least one required). Each entry needs tag_id; name and/or color must be set on each entry to make a meaningful change."`
}

// bulkTagUpdateInput is one entry in updateTagsInput.Updates. Field
// names mirror favro_update_tag's input shape so the schema feels
// consistent across the tag-write surface.
type bulkTagUpdateInput struct {
	TagID string `json:"tag_id" jsonschema:"the Favro tagId to update. Resolve via favro_resolve_tag if you only have the tag name."`
	Name  string `json:"name,omitempty" jsonschema:"new tag name; leave empty on this entry to keep the current name"`
	Color string `json:"color,omitempty" jsonschema:"new palette color: blue, red, green, lime, purple, cyan, brown, orange, gray, pink, yellow, slategray. Leave empty on this entry to keep the current color."`
}

func registerUpdateTags(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: updateTagsToolName,
		Description: "Apply multiple tag updates in one tool call. Favro has no real " +
			"bulk-tag endpoint, so this fans out to N parallel `PUT /tags/{tagId}` " +
			"requests under the hood — same total round-trips as N sequential " +
			"`favro_update_tag` calls, but parallelized. Useful for renaming or " +
			"recoloring a set of tags (e.g. all 'priority-*' tags) in one shot. Each " +
			"entry needs `tag_id` plus at least one of `name` or `color`. Resolve " +
			"names to tagIds via favro_resolve_tag first. Returns the array of updated " +
			"tags in input order. On the first per-entry error remaining in-flight " +
			"requests cancel — partial successes may have already landed; the wrapped " +
			"error names the offending tagId and index so callers can recover. On a " +
			"successful live write the org's tag cache is invalidated. Pass " +
			"`dry_run: true` to preview without contacting Favro.",
		Annotations: mutating("Bulk-update Favro tags", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateTagsInput) (*mcp.CallToolResult, writeOutput[[]favro.Tag], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		updates := make([]favro.BulkTagUpdate, len(in.Updates))
		for i, u := range in.Updates {
			updates[i] = favro.BulkTagUpdate{TagID: u.TagID, Name: u.Name, Color: u.Color}
		}
		out, err := runWrite(
			func() ([]favro.Tag, error) {
				return r.client.UpdateTags(writeCtx, updates)
			},
			func() string { return updateTagsStateDiff(in) },
		)
		if err != nil {
			return nil, writeOutput[[]favro.Tag]{}, err
		}
		if !out.DryRun {
			r.invalidateTagCache()
		}
		return nil, out, nil
	})
}

// updateTagsStateDiff renders the bulk dry-run state-diff phrase.
// Each entry contributes a tagChangeFragment; entries with neither
// name nor color set surface as "no-op" so a malformed bulk request
// is visible before sending.
func updateTagsStateDiff(in updateTagsInput) string {
	parts := make([]string, 0, len(in.Updates))
	for _, u := range in.Updates {
		parts = append(parts, tagChangeFragment(u.TagID, u.Name, u.Color))
	}
	return fmt.Sprintf("would bulk-update %d tag(s): %s", len(in.Updates), strings.Join(parts, "; "))
}
