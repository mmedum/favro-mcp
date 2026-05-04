package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	listCommentsToolName = "favro_list_comments"
	getCommentToolName   = "favro_get_comment"
)

// listCommentsInput requires card_common_id since Favro's /comments
// endpoint scopes comments to a single card via the cross-widget
// cardCommonId; an unfiltered listing is not supported.
type listCommentsInput struct {
	listInput
	CardCommonID string `json:"card_common_id" jsonschema:"the cross-widget cardCommonId whose comments to list; required by Favro on every page (must be passed on follow-up pages too, not just the first)"`
}

// getCommentInput is the input for favro_get_comment.
type getCommentInput struct {
	CommentID string `json:"comment_id" jsonschema:"the Favro commentId"`
}

func registerComments(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listCommentsToolName,
		Description: "List comments on a single Favro card. `card_common_id` is REQUIRED " +
			"(the cross-widget card identity, not the per-widget cardId). Returns one " +
			"page; pass `page` (1-indexed) plus the `request_id` from the prior response " +
			"(and `card_common_id` again) to retrieve subsequent pages. Read-only.",
		Annotations: readOnly("List Favro comments"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listCommentsInput) (*mcp.CallToolResult, listOutput[favro.Comment], error) {
		env, err := client.ListComments(ctx, in.Page, in.RequestID, in.CardCommonID)
		if err != nil {
			return nil, listOutput[favro.Comment]{}, err
		}
		return nil, newListOutput(env), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        getCommentToolName,
		Description: "Get a single Favro comment by its commentId. Read-only.",
		Annotations: readOnly("Get Favro comment"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getCommentInput) (*mcp.CallToolResult, favro.Comment, error) {
		cm, err := client.GetComment(ctx, in.CommentID)
		if err != nil {
			return nil, favro.Comment{}, err
		}
		return nil, cm, nil
	})
}
