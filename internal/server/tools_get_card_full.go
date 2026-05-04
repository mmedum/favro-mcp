package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const getCardFullToolName = "favro_get_card_full"

// getCardFullInput is the input for favro_get_card_full. Exactly
// one of card_id / card_common_id / sequential_id must be set; the
// resolver short-circuits with errFullCardIdentityRequired otherwise.
type getCardFullInput struct {
	CardID          string `json:"card_id,omitempty" jsonschema:"the per-widget Favro cardId; mutually exclusive with card_common_id and sequential_id. Exactly one identity is required."`
	CardCommonID    string `json:"card_common_id,omitempty" jsonschema:"the cross-widget Favro cardCommonId; mutually exclusive with card_id and sequential_id. Exactly one identity is required."`
	SequentialID    int    `json:"sequential_id,omitempty" jsonschema:"the integer part of a human-readable card ref (e.g. 123 for 'BSC-123'); mutually exclusive with card_id and card_common_id. Exactly one identity is required."`
	IncludeComments bool   `json:"include_comments,omitempty" jsonschema:"include the first page of comments on the card (default false). Comments are scoped per cardCommonId so they are shared across every widget instance of the card."`
	CommentLimit    int    `json:"comment_limit,omitempty" jsonschema:"max comments to return when include_comments=true; trims the first page locally. Default 20."`
}

func registerGetCardFull(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: getCardFullToolName,
		Description: "Fetch one Favro card with every id field dereferenced into a " +
			"human-readable name: tag IDs → tag names, assignee userIds → user names + " +
			"emails, widgetCommonId → widget name, columnId → column name, " +
			"collectionIds (from the parent widget) → collection names, customFieldId → " +
			"custom field name + display value (for simple types: Text, Number, Date, " +
			"Checkbox, Link, Single select, Multiple select; long-tail types pass " +
			"through with the raw value attached). Saves 4–7 follow-up calls in the " +
			"typical 'fetch card → resolve everything' flow. Pass exactly one of " +
			"`card_id` (per-widget), `card_common_id` (cross-widget), or `sequential_id` " +
			"(integer of a 'BSC-123' ref). Comments are off by default — set " +
			"`include_comments: true` to fetch the first page (cap with `comment_limit`, " +
			"default 20). Read-only.",
		Annotations: readOnly("Get a Favro card with names dereferenced"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getCardFullInput) (*mcp.CallToolResult, FullCard, error) {
		full, err := r.GetFullCard(ctx, FullCardIdentity{
			CardID:       in.CardID,
			CardCommonID: in.CardCommonID,
			SequentialID: in.SequentialID,
		}, in.IncludeComments, in.CommentLimit)
		if err != nil {
			return nil, FullCard{}, err
		}
		return nil, full, nil
	})
}
