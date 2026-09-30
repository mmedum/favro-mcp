package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v3/internal/favroapi"
	"github.com/mmedum/favro-mcp/v3/internal/render"
	"github.com/mmedum/favro-mcp/v3/internal/service"
)

const deleteTagToolName = "favro_delete_tag"

// deleteTagInput is the input for favro_delete_tag.
type deleteTagInput struct {
	dryRunInput
	TagID string `json:"tag_id" jsonschema:"the Favro tagId to delete. Resolve via favro_resolve_tag if you only have the tag name."`
}

func registerDeleteTag(reg *registry, r *service.Resolver) {
	addAsking(reg, &mcp.Tool{
		Name: deleteTagToolName,
		Description: "Delete an org-global Favro tag by its tagId. The tag is removed from " +
			"every card it was applied to — Favro does not soft-delete tags. On a " +
			"successful live delete the org's tag cache is invalidated so the next " +
			"resolve / list call re-fetches. Pass `dry_run: true` to preview the " +
			"request without contacting Favro. Destructive — MCP hosts may warn " +
			"users before auto-confirming." + asksFirst,
		Annotations: mutating("Delete Favro tag", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteTagInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		if err := confirmFirst(ctx, r.Client().DryRun(writeCtx), func() (render.Question, error) {
			tag, err := r.Client().GetTag(ctx, in.TagID)
			return render.AskDeleteTag(in.TagID, tag.Name), err
		}); err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		out, err := runWrite(
			func() (struct{}, error) {
				return struct{}{}, r.Client().DeleteTag(writeCtx, in.TagID)
			},
			func() string {
				return fmt.Sprintf("would delete tag %q from the active organization (and remove it from every card it was applied to)", in.TagID)
			},
		)
		if err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		if !out.DryRun {
			r.InvalidateTagCache()
		}
		return nil, out, nil
	})
}
