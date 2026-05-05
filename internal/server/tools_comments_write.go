package server

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	createCommentToolName = "favro_create_comment"
	updateCommentToolName = "favro_update_comment"
	deleteCommentToolName = "favro_delete_comment"
)

// createCommentInput is the input for favro_create_comment.
type createCommentInput struct {
	dryRunInput
	CardCommonID string `json:"card_common_id" jsonschema:"the cross-widget Favro cardCommonId the comment is attached to. Resolve via favro_search_cards or favro_get_card_full if you only have the card name."`
	Comment      string `json:"comment" jsonschema:"the comment text (markdown supported)"`
}

// updateCommentInput is the input for favro_update_comment.
type updateCommentInput struct {
	dryRunInput
	CommentID string `json:"comment_id" jsonschema:"the Favro commentId to update"`
	Comment   string `json:"comment" jsonschema:"the new comment text (markdown supported); replaces the existing body in full"`
}

// deleteCommentInput is the input for favro_delete_comment.
type deleteCommentInput struct {
	dryRunInput
	CommentID string `json:"comment_id" jsonschema:"the Favro commentId to delete"`
}

func registerCreateComment(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: createCommentToolName,
		Description: "Add a new comment to a Favro card identified by `card_common_id`. " +
			"`comment` is the markdown body. Pass `dry_run: true` to preview the " +
			"request without contacting Favro. Comments are not cached at the " +
			"resolver layer, so no invalidation is needed on success.",
		Annotations: mutating("Create Favro comment", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createCommentInput) (*mcp.CallToolResult, writeOutput[favro.Comment], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Comment, error) {
				return r.client.CreateComment(writeCtx, favro.CreateCommentRequest{
					CardCommonID: in.CardCommonID,
					Comment:      in.Comment,
				})
			},
			func() string {
				return fmt.Sprintf("would create a new comment on card %q", in.CardCommonID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.Comment]{}, err
		}
		return nil, out, nil
	})
}

func registerUpdateComment(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: updateCommentToolName,
		Description: "Replace the body of an existing Favro comment. The new `comment` " +
			"text replaces the existing body in full; surgical edit-in-place is " +
			"not yet supported. Pass `dry_run: true` to preview without writing.",
		Annotations: mutating("Update Favro comment", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateCommentInput) (*mcp.CallToolResult, writeOutput[favro.Comment], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Comment, error) {
				return r.client.UpdateComment(writeCtx, in.CommentID, favro.UpdateCommentRequest{Comment: in.Comment})
			},
			func() string {
				return fmt.Sprintf("would replace the body of comment %q", in.CommentID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.Comment]{}, err
		}
		return nil, out, nil
	})
}

func registerDeleteComment(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: deleteCommentToolName,
		Description: "Delete a Favro comment by its commentId. Destructive — MCP hosts " +
			"may warn before auto-confirming. Pass `dry_run: true` to preview.",
		Annotations: mutating("Delete Favro comment", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteCommentInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (struct{}, error) {
				return struct{}{}, r.client.DeleteComment(writeCtx, in.CommentID)
			},
			func() string {
				return fmt.Sprintf("would delete comment %q", in.CommentID)
			},
		)
		if err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		return nil, out, nil
	})
}
