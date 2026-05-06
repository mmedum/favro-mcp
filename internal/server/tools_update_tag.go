package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const updateTagToolName = "favro_update_tag"

// updateTagInput is the input for favro_update_tag.
type updateTagInput struct {
	dryRunInput
	TagID string `json:"tag_id" jsonschema:"the Favro tagId to update. Resolve via favro_resolve_tag if you only have the tag name."`
	Name  string `json:"name,omitempty" jsonschema:"new tag name; leave empty to keep the current name"`
	Color string `json:"color,omitempty" jsonschema:"new palette color: blue, red, green, lime, purple, cyan, brown, orange, gray, pink, yellow, slategray. Leave empty to keep the current color."`
}

func registerUpdateTag(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: updateTagToolName,
		Description: "Update an org-global Favro tag's name and/or color. Both fields are " +
			"optional; pass at least one to make a meaningful change. The change " +
			"propagates to every card the tag is applied to. On a successful live " +
			"update the org's tag cache is invalidated. Pass `dry_run: true` to " +
			"preview the request without contacting Favro.",
		Annotations: mutating("Update Favro tag", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateTagInput) (*mcp.CallToolResult, writeOutput[favro.Tag], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Tag, error) {
				return r.client.UpdateTag(writeCtx, in.TagID, favro.UpdateTagRequest{Name: in.Name, Color: in.Color})
			},
			func() string { return updateTagStateDiff(in) },
		)
		if err != nil {
			return nil, writeOutput[favro.Tag]{}, err
		}
		if !out.DryRun {
			r.invalidateTagCache()
		}
		return nil, out, nil
	})
}

// updateTagStateDiff renders the single-tool dry-run state-diff phrase.
func updateTagStateDiff(in updateTagInput) string {
	frag := tagChangeFragment(in.TagID, in.Name, in.Color)
	if strings.HasSuffix(frag, "no-op") {
		return fmt.Sprintf("would PUT %s (no changed fields)", frag)
	}
	return fmt.Sprintf("would update %s", frag)
}

// tagChangeFragment renders one "tag {id}: name → \"x\", color → \"y\""
// (or `no-op`) phrase. Shared by favro_update_tag's single-tag diff and
// favro_update_tags' per-entry diff so the formatting stays in lockstep.
func tagChangeFragment(tagID, name, color string) string {
	var changes []string
	if name != "" {
		changes = append(changes, fmt.Sprintf("name → %q", name))
	}
	if color != "" {
		changes = append(changes, fmt.Sprintf("color → %q", color))
	}
	if len(changes) == 0 {
		return fmt.Sprintf("tag %q: no-op", tagID)
	}
	return fmt.Sprintf("tag %q: %s", tagID, strings.Join(changes, ", "))
}
