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

// updateTagStateDiff renders the per-tool dry-run state-diff phrase.
// Pulled out of the closure so the empty-name / empty-color branches
// don't bloat the registration flow.
func updateTagStateDiff(in updateTagInput) string {
	var changes []string
	if in.Name != "" {
		changes = append(changes, fmt.Sprintf("name → %q", in.Name))
	}
	if in.Color != "" {
		changes = append(changes, fmt.Sprintf("color → %q", in.Color))
	}
	if len(changes) == 0 {
		return fmt.Sprintf("would PUT tag %q with no changed fields (no-op)", in.TagID)
	}
	return fmt.Sprintf("would update tag %q: %s", in.TagID, strings.Join(changes, ", "))
}
