package server

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const deleteTagToolName = "favro_delete_tag"

// deleteTagInput is the input for favro_delete_tag.
type deleteTagInput struct {
	dryRunInput
	TagID string `json:"tag_id" jsonschema:"the Favro tagId to delete. Resolve via favro_resolve_tag if you only have the tag name."`
}

func registerDeleteTag(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: deleteTagToolName,
		Description: "Delete an org-global Favro tag by its tagId. The tag is removed from " +
			"every card it was applied to — Favro does not soft-delete tags. On a " +
			"successful live delete the org's tag cache is invalidated so the next " +
			"resolve / list call re-fetches. Pass `dry_run: true` to preview the " +
			"request without contacting Favro. Destructive — MCP hosts may warn " +
			"users before auto-confirming.",
		Annotations: mutating("Delete Favro tag", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteTagInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (struct{}, error) {
				return struct{}{}, r.client.DeleteTag(writeCtx, in.TagID)
			},
			func() string {
				return fmt.Sprintf("would delete tag %q from the active organization (and remove it from every card it was applied to)", in.TagID)
			},
		)
		if err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		if !out.DryRun {
			r.invalidateTagCache()
		}
		return nil, out, nil
	})
}
