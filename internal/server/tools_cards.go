package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	listCardsToolName = "favro_list_cards"
	getCardToolName   = "favro_get_card"
)

// listCardsInput collects every filter Favro's /cards endpoint
// understands. All filters are optional; an empty input asks for
// every card the token can see (callers should expect many pages).
//
// Filters compose at the API layer — passing widget_common_id and
// card_common_id together returns the intersection. When paginating,
// every filter must be re-passed on every page; Favro does not
// carry filter state across pages.
type listCardsInput struct {
	listInput
	WidgetCommonID    string `json:"widget_common_id,omitempty" jsonschema:"optional Favro widget id (widgetCommonId) to scope the listing to a single widget; pass it on EVERY page when paginating"`
	CollectionID      string `json:"collection_id,omitempty" jsonschema:"optional Favro collection id to scope the listing to a single collection"`
	CardCommonID      string `json:"card_common_id,omitempty" jsonschema:"optional Favro cardCommonId to fetch every instance of a single card across the widgets it lives on"`
	SequentialID      int    `json:"sequential_id,omitempty" jsonschema:"optional Favro card sequential id (the integer part of human-readable refs like 'BSC-123' — pass 123 here)"`
	ColumnID          string `json:"column_id,omitempty" jsonschema:"optional Favro columnId to scope the listing to a single column inside the widget/collection"`
	TodoList          bool   `json:"todo_list,omitempty" jsonschema:"if true, restrict the listing to the authenticated user's personal todo list"`
	Archived          bool   `json:"archived,omitempty" jsonschema:"if true, include archived cards in the result; default (false) hides them server-side"`
	Unique            bool   `json:"unique,omitempty" jsonschema:"if true, return one row per cardCommonId rather than one row per widget instance — useful when searching by name and cross-widget duplicates are noise"`
	DescriptionFormat string `json:"description_format,omitempty" jsonschema:"'plaintext' (default) or 'markdown' for the detailedDescription body"`
}

// getCardInput is the input for favro_get_card.
type getCardInput struct {
	CardID string `json:"card_id" jsonschema:"the Favro per-widget cardId (NOT the cross-widget cardCommonId — Favro 403s if you pass a cardCommonId here). To fetch a card known only by cardCommonId, call favro_list_cards with that filter."`
}

func registerCards(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listCardsToolName,
		Description: "List Favro cards with optional filters: `widget_common_id` (single " +
			"widget), `collection_id` (cards in any widget of that collection), `card_common_id` " +
			"(every instance of one card), `sequential_id` (per-card integer counter). Pass " +
			"`unique=true` to dedupe cards that live on multiple widgets. Returns one page; " +
			"pass `page` (1-indexed) plus the `request_id` from the prior response (and the " +
			"SAME filters again) to retrieve subsequent pages. Read-only.",
		Annotations: readOnly("List Favro cards"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listCardsInput) (*mcp.CallToolResult, listOutput[favro.Card], error) {
		env, err := client.ListCards(ctx, in.Page, in.RequestID, favro.ListCardsFilter{
			WidgetCommonID:    in.WidgetCommonID,
			CollectionID:      in.CollectionID,
			CardCommonID:      in.CardCommonID,
			SequentialID:      in.SequentialID,
			ColumnID:          in.ColumnID,
			TodoList:          in.TodoList,
			Archived:          in.Archived,
			Unique:            in.Unique,
			DescriptionFormat: in.DescriptionFormat,
		})
		if err != nil {
			return nil, listOutput[favro.Card]{}, err
		}
		return nil, newListOutput(env), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: getCardToolName,
		Description: "Get a single Favro card by its per-widget cardId. Favro's GET endpoint " +
			"only accepts the per-widget cardId — passing a cardCommonId here 403s. To fetch " +
			"a card known only by cardCommonId, use favro_list_cards with that filter. " +
			"Read-only.",
		Annotations: readOnly("Get Favro card"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getCardInput) (*mcp.CallToolResult, favro.Card, error) {
		card, err := client.GetCard(ctx, in.CardID)
		if err != nil {
			return nil, favro.Card{}, err
		}
		return nil, card, nil
	})
}
