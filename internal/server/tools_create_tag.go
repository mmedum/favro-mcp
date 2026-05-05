package server

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const createTagToolName = "favro_create_tag"

// createTagInput is the input for favro_create_tag.
type createTagInput struct {
	dryRunInput
	Name  string `json:"name" jsonschema:"the tag name (required). Favro does not enforce name uniqueness server-side, so duplicates are possible — call favro_resolve_tag first if avoiding duplicates matters."`
	Color string `json:"color,omitempty" jsonschema:"optional palette color: blue, red, green, lime, purple, cyan, brown, orange, gray, pink, yellow, slategray. Omit to let Favro pick randomly."`
}

func registerCreateTag(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: createTagToolName,
		Description: "Create a new Favro tag in the active organization. Favro does not " +
			"enforce name uniqueness, so calling this with an existing name will create a " +
			"second tag with the same display name (different tagId) — resolve first if " +
			"you want idempotent behavior. `color` is optional; omit to let Favro pick " +
			"randomly. On success the org-global tag list cache is invalidated so the next " +
			"resolve / list call re-fetches. Pass `dry_run: true` to preview the request " +
			"(method + URL + body + predicted state change) without contacting Favro.",
		Annotations: mutating("Create Favro tag", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createTagInput) (*mcp.CallToolResult, writeOutput[favro.Tag], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Tag, error) {
				return r.client.CreateTag(writeCtx, favro.CreateTagRequest{Name: in.Name, Color: in.Color})
			},
			func() string {
				if in.Color == "" {
					return fmt.Sprintf("would create a new tag named %q (Favro will pick a random color) in the active organization", in.Name)
				}
				return fmt.Sprintf("would create a new tag named %q with color %q in the active organization", in.Name, in.Color)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.Tag]{}, err
		}
		// Successful live writes invalidate the resolver's tag cache
		// so the next favro_resolve_tag / favro_list_tags re-fetches
		// and sees the new tag. Dry-run intentionally does NOT
		// invalidate — no state changed, the cache is still correct.
		if !out.DryRun {
			r.invalidateTagCache()
		}
		return nil, out, nil
	})
}
