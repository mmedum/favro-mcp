package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
	"github.com/mmedum/favro-mcp/v4/internal/favroapi"
	"github.com/mmedum/favro-mcp/v4/internal/service"
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
	ThreadID     string `json:"thread_id,omitempty" jsonschema:"reply in this thread of the card: a comment's threadId from favro_list_comments. Omit to start a new thread."`
}

// updateCommentInput is the input for favro_update_comment.
type updateCommentInput struct {
	dryRunInput
	CommentID         string   `json:"comment_id" jsonschema:"the Favro commentId to update"`
	Comment           string   `json:"comment" jsonschema:"the new comment text (markdown supported); replaces the existing body in full"`
	RemoveAttachments []string `json:"remove_attachments,omitempty" jsonschema:"attachment URLs to detach from the comment. Use the fileURL values from favro_get_comment / favro_list_comments."`
}

// deleteCommentInput is the input for favro_delete_comment.
type deleteCommentInput struct {
	dryRunInput
	confirmInput
	CommentID string `json:"comment_id" jsonschema:"the Favro commentId to delete"`
}

func registerCreateComment(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: createCommentToolName,
		Description: "Add a new comment to a Favro card identified by `card_common_id`. " +
			"`comment` is the markdown body. `thread_id` replies in that thread; " +
			"without it the comment starts a new one. Pass `dry_run: true` to preview the " +
			"request without contacting Favro. Comments are not cached at the " +
			"resolver layer, so no invalidation is needed on success.",
		Annotations: adding("Create Favro comment"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createCommentInput) (*mcp.CallToolResult, writeOutput[favro.Comment], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Comment, error) {
				return r.Client().CreateComment(writeCtx, favro.CreateCommentRequest{
					CardCommonID: in.CardCommonID,
					Comment:      in.Comment,
					ThreadID:     in.ThreadID,
				})
			},
			func() string {
				if in.ThreadID != "" {
					return fmt.Sprintf("would reply in thread %q on card %q", in.ThreadID, in.CardCommonID)
				}
				return fmt.Sprintf("would create a new comment on card %q", in.CardCommonID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.Comment]{}, err
		}
		// A 200 is not confirmation (D-24): a reply Favro filed elsewhere
		// answers with the thread it went to. Checked by the live step
		// "favro_create_comment with thread_id replies in that thread".
		if in.ThreadID != "" && out.Result != nil && out.Result.ThreadID != in.ThreadID {
			out.Notes = append(out.Notes, replyThreadNote(out.Result.ThreadID, in.ThreadID))
		}
		return nil, out, nil
	})
}

// replyThreadNote says where a reply went when Favro's answer named
// another thread than the one asked for, or none.
func replyThreadNote(got, want string) string {
	if got == "" {
		return fmt.Sprintf("Favro's answer named no thread, so whether the comment is in thread %q is unconfirmed; "+
			"read it with favro_list_comments and thread_id before posting again", want)
	}
	return fmt.Sprintf("Favro filed the comment in thread %q, not the requested %q; it may have started a new thread", got, want)
}

func registerUpdateComment(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: updateCommentToolName,
		Description: "Replace the body of an existing Favro comment. The new `comment` " +
			"text replaces the existing body in full; surgical edit-in-place is " +
			"not yet supported. Pass `dry_run: true` to preview without writing.",
		Annotations: changing("Update Favro comment"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateCommentInput) (*mcp.CallToolResult, writeOutput[favro.Comment], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Comment, error) {
				return r.Client().UpdateComment(writeCtx, in.CommentID, favro.UpdateCommentRequest{
					Comment:           in.Comment,
					RemoveAttachments: in.RemoveAttachments,
				})
			},
			func() string {
				if len(in.RemoveAttachments) > 0 {
					return fmt.Sprintf("would replace the body of comment %q and detach %d attachment(s)", in.CommentID, len(in.RemoveAttachments))
				}
				return fmt.Sprintf("would replace the body of comment %q", in.CommentID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.Comment]{}, err
		}
		return nil, out, nil
	})
}

func registerDeleteComment(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: deleteCommentToolName,
		Description: "Delete a Favro comment by its commentId. Destructive — MCP hosts " +
			"may warn before auto-confirming. Pass `dry_run: true` to preview.",
		Annotations: changing("Delete Favro comment"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteCommentInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (struct{}, error) {
				return struct{}{}, r.Client().DeleteComment(writeCtx, in.CommentID)
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
