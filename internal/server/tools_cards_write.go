package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	createCardToolName    = "favro_create_card"
	updateCardToolName    = "favro_update_card"
	archiveCardToolName   = "favro_archive_card"
	unarchiveCardToolName = "favro_unarchive_card"
	moveCardToolName      = "favro_move_card"
	deleteCardToolName    = "favro_delete_card"
)

// createCardInput is the input for favro_create_card. Tag *names*
// are deliberately not exposed — only tag_ids — because Favro's
// `tags: ["frontend"]` field auto-creates missing tags and is the
// kind of typo amplifier plan §6 wants gated behind Phase 6's
// hard-fail-on-unknown favro_add_tag_to_card.
type createCardInput struct {
	dryRunInput
	Name                string   `json:"name" jsonschema:"the card name (required)"`
	WidgetCommonID      string   `json:"widget_common_id,omitempty" jsonschema:"the widgetCommonId of the board to create the card on. Resolve via favro_resolve_widget. Omit to create the card on the authenticated user's todo list."`
	ColumnID            string   `json:"column_id,omitempty" jsonschema:"the columnId on the target widget. Resolve via favro_resolve_column."`
	LaneID              string   `json:"lane_id,omitempty" jsonschema:"the laneId on the target widget (only meaningful when the widget has lanes)"`
	ParentCardID        string   `json:"parent_card_id,omitempty" jsonschema:"the cardId of a parent card if this card should be a child"`
	DetailedDescription string   `json:"detailed_description,omitempty" jsonschema:"markdown body for the card description"`
	ListPosition        *float64 `json:"list_position,omitempty" jsonschema:"position on a kanban widget as a JSON number. 0 places the card at the top of the column; a number larger than the current max sends it to the bottom; fractional values (e.g. 3.5) slot between siblings without renumbering. (String values, even numeric, are rejected by Favro.)"`
	SheetPosition       *float64 `json:"sheet_position,omitempty" jsonschema:"position on a sheet widget as a JSON number — same numeric vocabulary as list_position"`
	AssignmentIDs       []string `json:"assignment_ids,omitempty" jsonschema:"userIds to assign on creation. Resolve via favro_resolve_user."`
	TagIDs              []string `json:"tag_ids,omitempty" jsonschema:"existing tagIds to apply on creation. Resolve via favro_resolve_tag — adding tags by name is intentionally not exposed here (use the Phase 6 favro_add_tag_to_card tool, which hard-fails on unknown names)."`
	StartDate           string   `json:"start_date,omitempty" jsonschema:"ISO-8601 start date (e.g. 2026-05-05T00:00:00Z)"`
	DueDate             string   `json:"due_date,omitempty" jsonschema:"ISO-8601 due date"`
}

// updateCardInput is the input for favro_update_card. Mirrors
// UpdateCardRequest minus the by-name tag fields and the Archive
// boolean (the dedicated archive/unarchive tools cover that case
// with clearer LLM ergonomics).
type updateCardInput struct {
	dryRunInput
	CardID              string   `json:"card_id" jsonschema:"the per-widget cardId to update (NOT cardCommonId — Favro PUT /cards/{id} expects the per-widget instance id)"`
	Name                string   `json:"name,omitempty" jsonschema:"new card name; omit to keep current"`
	DetailedDescription string   `json:"detailed_description,omitempty" jsonschema:"replacement markdown body. Phase 6's favro_append/prepend/replace_in_card_description tools provide surgical edits; this one whole-body replaces."`
	WidgetCommonID      string   `json:"widget_common_id,omitempty" jsonschema:"move card to a different widget; for relocations prefer the dedicated favro_move_card tool"`
	ColumnID            string   `json:"column_id,omitempty" jsonschema:"move card to a different column"`
	LaneID              string   `json:"lane_id,omitempty" jsonschema:"move card to a different lane"`
	ParentCardID        string   `json:"parent_card_id,omitempty" jsonschema:"set or change the parent card"`
	DragMode            string   `json:"drag_mode,omitempty" jsonschema:"'commit' (Favro's default — neighboring cards re-shuffle) or 'move' (no repositioning of siblings). Only relevant with column_id / list_position / sheet_position."`
	ListPosition        *float64 `json:"list_position,omitempty" jsonschema:"position on a kanban widget as a JSON number. 0 places the card at the top of the column; a number larger than the current max sends it to the bottom; fractional values (e.g. 3.5) slot between siblings. **Required for column moves** — a column_id change without list_position silently no-ops."`
	SheetPosition       *float64 `json:"sheet_position,omitempty" jsonschema:"position on a sheet widget as a JSON number — same numeric vocabulary as list_position"`
	AddAssignmentIDs    []string `json:"add_assignment_ids,omitempty" jsonschema:"userIds to add to the card's assignments. Resolve via favro_resolve_user."`
	RemoveAssignmentIDs []string `json:"remove_assignment_ids,omitempty" jsonschema:"userIds to remove from the card's assignments"`
	AddTagIDs           []string `json:"add_tag_ids,omitempty" jsonschema:"existing tagIds to add to the card. Resolve via favro_resolve_tag. Adding tags by name is intentionally not exposed here (Phase 6's favro_add_tag_to_card hard-fails on unknown names)."`
	RemoveTagIDs        []string `json:"remove_tag_ids,omitempty" jsonschema:"tagIds to remove from the card"`
	StartDate           string   `json:"start_date,omitempty" jsonschema:"ISO-8601 start date"`
	DueDate             string   `json:"due_date,omitempty" jsonschema:"ISO-8601 due date"`
}

type archiveCardInput struct {
	dryRunInput
	CardID string `json:"card_id" jsonschema:"the per-widget cardId to archive"`
}

type unarchiveCardInput struct {
	dryRunInput
	CardID string `json:"card_id" jsonschema:"the per-widget cardId to unarchive"`
}

// moveCardInput is the input for favro_move_card. At least one of
// widget_common_id / column_id / lane_id must be set; an all-empty
// move would PUT a no-op and silently succeed, which the favro layer
// rejects with a typed error.
type moveCardInput struct {
	dryRunInput
	CardID         string   `json:"card_id" jsonschema:"the per-widget cardId to move"`
	WidgetCommonID string   `json:"widget_common_id,omitempty" jsonschema:"target widgetCommonId (resolve via favro_resolve_widget). At least one of widget_common_id, column_id, or lane_id must be set."`
	ColumnID       string   `json:"column_id,omitempty" jsonschema:"target columnId on the destination widget"`
	LaneID         string   `json:"lane_id,omitempty" jsonschema:"target laneId on the destination widget"`
	ListPosition   *float64 `json:"list_position,omitempty" jsonschema:"insertion position in the destination column as a JSON number. **Required for column moves** — Favro silently no-ops a column move that omits this. 0 = top; high number = bottom; fractional slots between siblings."`
	SheetPosition  *float64 `json:"sheet_position,omitempty" jsonschema:"insertion position on a sheet widget as a JSON number — same numeric vocabulary as list_position"`
	DragMode       string   `json:"drag_mode,omitempty" jsonschema:"'commit' (Favro's default — neighboring cards re-shuffle) or 'move' (no repositioning of siblings)"`
}

// deleteCardInput is the input for favro_delete_card. The destructive
// `everywhere` flag is loud in the schema description because the two
// behaviors (per-widget vs cross-widget purge) are not interchangeable
// and the LLM has to pick correctly.
type deleteCardInput struct {
	dryRunInput
	CardID     string `json:"card_id" jsonschema:"the per-widget cardId to delete"`
	Everywhere bool   `json:"everywhere,omitempty" jsonschema:"if false (default), deletes only this per-widget card instance; other widgets sharing the same cardCommonId keep their copies. If true, deletes the cardCommonId across EVERY widget — irreversible."`
}

func registerCreateCard(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: createCardToolName,
		Description: "Create a new Favro card. `name` is required; pass `widget_common_id` " +
			"(resolve via favro_resolve_widget) to create on a board, or omit to put the " +
			"card on the authenticated user's todo list. Tag attachment uses tag_ids only " +
			"— add-tag-by-name with hard-fail-on-unknown is the Phase 6 favro_add_tag_to_card " +
			"tool. Successful live writes invalidate the search-cards cache. Pass `dry_run: true` " +
			"to preview the request without contacting Favro.",
		Annotations: mutating("Create Favro card", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createCardInput) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Card, error) {
				return r.client.CreateCard(writeCtx, favro.CreateCardRequest{
					Name:                in.Name,
					WidgetCommonID:      in.WidgetCommonID,
					ColumnID:            in.ColumnID,
					LaneID:              in.LaneID,
					ParentCardID:        in.ParentCardID,
					DetailedDescription: in.DetailedDescription,
					ListPosition:        in.ListPosition,
					SheetPosition:       in.SheetPosition,
					AssignmentIDs:       in.AssignmentIDs,
					TagIDs:              in.TagIDs,
					StartDate:           in.StartDate,
					DueDate:             in.DueDate,
				})
			},
			func() string { return createCardStateDiff(in) },
		)
		if err != nil {
			return nil, writeOutput[favro.Card]{}, err
		}
		if !out.DryRun {
			r.invalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

func registerUpdateCard(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: updateCardToolName,
		Description: "Update a Favro card by its per-widget cardId. Every body field is " +
			"optional — pass at least one. For relocations prefer favro_move_card; for " +
			"archiving prefer favro_archive_card / favro_unarchive_card (clearer LLM intent). " +
			"`detailed_description` whole-body replaces; surgical markdown edits are Phase 6's " +
			"append/prepend/replace tools. Tag mutations use *_tag_ids only. Successful live " +
			"writes invalidate the search-cards cache. Pass `dry_run: true` to preview.",
		Annotations: mutating("Update Favro card", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateCardInput) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Card, error) {
				return r.client.UpdateCard(writeCtx, in.CardID, favro.UpdateCardRequest{
					Name:                in.Name,
					DetailedDescription: in.DetailedDescription,
					WidgetCommonID:      in.WidgetCommonID,
					ColumnID:            in.ColumnID,
					LaneID:              in.LaneID,
					ParentCardID:        in.ParentCardID,
					DragMode:            in.DragMode,
					ListPosition:        in.ListPosition,
					SheetPosition:       in.SheetPosition,
					AddAssignmentIDs:    in.AddAssignmentIDs,
					RemoveAssignmentIDs: in.RemoveAssignmentIDs,
					AddTagIDs:           in.AddTagIDs,
					RemoveTagIDs:        in.RemoveTagIDs,
					StartDate:           in.StartDate,
					DueDate:             in.DueDate,
				})
			},
			func() string { return updateCardStateDiff(&in) },
		)
		if err != nil {
			return nil, writeOutput[favro.Card]{}, err
		}
		if !out.DryRun {
			r.invalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

func registerArchiveCard(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: archiveCardToolName,
		Description: "Archive a Favro card by its per-widget cardId. Convenience over " +
			"favro_update_card with `archive: true`. Reversible via favro_unarchive_card. " +
			"Successful live writes invalidate the search-cards cache. Pass `dry_run: true` " +
			"to preview without contacting Favro.",
		Annotations: mutating("Archive Favro card", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in archiveCardInput) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Card, error) { return r.client.ArchiveCard(writeCtx, in.CardID) },
			func() string { return fmt.Sprintf("would archive card %q", in.CardID) },
		)
		if err != nil {
			return nil, writeOutput[favro.Card]{}, err
		}
		if !out.DryRun {
			r.invalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

func registerUnarchiveCard(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: unarchiveCardToolName,
		Description: "Unarchive a Favro card by its per-widget cardId. Convenience over " +
			"favro_update_card with `archive: false`. Successful live writes invalidate " +
			"the search-cards cache. Pass `dry_run: true` to preview without contacting Favro.",
		Annotations: mutating("Unarchive Favro card", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in unarchiveCardInput) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Card, error) { return r.client.UnarchiveCard(writeCtx, in.CardID) },
			func() string { return fmt.Sprintf("would unarchive card %q", in.CardID) },
		)
		if err != nil {
			return nil, writeOutput[favro.Card]{}, err
		}
		if !out.DryRun {
			r.invalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

func registerMoveCard(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: moveCardToolName,
		Description: "Move a Favro card to a different widget, column, and/or lane. At " +
			"least one of widget_common_id / column_id / lane_id must be set. Convenience " +
			"over favro_update_card for the common 'move card to <board>' workflow. " +
			"Successful live writes invalidate the search-cards cache. Pass `dry_run: true` " +
			"to preview.",
		Annotations: mutating("Move Favro card", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in moveCardInput) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Card, error) {
				return r.client.MoveCard(writeCtx, in.CardID, favro.MoveCardRequest{
					WidgetCommonID: in.WidgetCommonID,
					ColumnID:       in.ColumnID,
					LaneID:         in.LaneID,
					ListPosition:   in.ListPosition,
					SheetPosition:  in.SheetPosition,
					DragMode:       in.DragMode,
				})
			},
			func() string { return moveCardStateDiff(in) },
		)
		if err != nil {
			return nil, writeOutput[favro.Card]{}, err
		}
		if !out.DryRun {
			r.invalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

func registerDeleteCard(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: deleteCardToolName,
		Description: "Delete a Favro card by its per-widget cardId. With `everywhere: false` " +
			"(default) only this widget's instance is removed — other widgets sharing the " +
			"same cardCommonId keep their copies. With `everywhere: true` the card is purged " +
			"from EVERY widget — irreversible. Returns the list of cardIds Favro deleted. " +
			"Successful live writes invalidate the search-cards cache. Destructive — MCP hosts " +
			"may warn before auto-confirming. Pass `dry_run: true` to preview.",
		Annotations: mutating("Delete Favro card", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteCardInput) (*mcp.CallToolResult, writeOutput[favro.DeleteCardResponse], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.DeleteCardResponse, error) {
				return r.client.DeleteCard(writeCtx, in.CardID, in.Everywhere)
			},
			func() string {
				if in.Everywhere {
					return fmt.Sprintf("would delete card %q from EVERY widget (everywhere=true) — irreversible", in.CardID)
				}
				return fmt.Sprintf("would delete this widget's instance of card %q (other widgets sharing the same cardCommonId keep their copies)", in.CardID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.DeleteCardResponse]{}, err
		}
		if !out.DryRun {
			r.invalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

// createCardStateDiff renders the dry-run state-diff phrase. Pulled
// out of the closure so the parent / column / widget branches don't
// bloat the registration body.
func createCardStateDiff(in createCardInput) string {
	dest := "the authenticated user's todo list"
	switch {
	case in.WidgetCommonID != "" && in.ColumnID != "":
		dest = fmt.Sprintf("widget %q column %q", in.WidgetCommonID, in.ColumnID)
	case in.WidgetCommonID != "":
		dest = fmt.Sprintf("widget %q", in.WidgetCommonID)
	}
	return fmt.Sprintf("would create a new card %q on %s", in.Name, dest)
}

// updateCardStateDiff renders the dry-run state-diff phrase. Mirrors
// updateTagStateDiff: list each changed field, fall back to a no-op
// message when no field is set so the LLM doesn't think a change
// happened. Driven by a small dispatch table — `desc` is a closure
// so each entry's value is computed lazily and pointer derefs only
// fire under their `when` guard.
func updateCardStateDiff(in *updateCardInput) string {
	type field struct {
		when bool
		desc func() string
	}
	str := func(label, val string) func() string {
		return func() string { return fmt.Sprintf("%s → %q", label, val) }
	}
	num := func(label string, p *float64) func() string {
		return func() string { return fmt.Sprintf("%s → %g", label, *p) }
	}
	count := func(prefix, label string, n int) func() string {
		return func() string { return fmt.Sprintf("%s%d %s", prefix, n, label) }
	}
	lit := func(s string) func() string { return func() string { return s } }
	fields := []field{
		{in.Name != "", str("name", in.Name)},
		{in.DetailedDescription != "", lit("description (whole-body replace)")},
		{in.WidgetCommonID != "", str("widget", in.WidgetCommonID)},
		{in.ColumnID != "", str("column", in.ColumnID)},
		{in.LaneID != "", str("lane", in.LaneID)},
		{in.ParentCardID != "", str("parent", in.ParentCardID)},
		{in.ListPosition != nil, num("list_position", in.ListPosition)},
		{in.SheetPosition != nil, num("sheet_position", in.SheetPosition)},
		{len(in.AddAssignmentIDs) > 0, count("+", "assignment(s)", len(in.AddAssignmentIDs))},
		{len(in.RemoveAssignmentIDs) > 0, count("-", "assignment(s)", len(in.RemoveAssignmentIDs))},
		{len(in.AddTagIDs) > 0, count("+", "tag(s)", len(in.AddTagIDs))},
		{len(in.RemoveTagIDs) > 0, count("-", "tag(s)", len(in.RemoveTagIDs))},
		{in.StartDate != "", str("start_date", in.StartDate)},
		{in.DueDate != "", str("due_date", in.DueDate)},
	}
	var changes []string
	for _, f := range fields {
		if f.when {
			changes = append(changes, f.desc())
		}
	}
	if len(changes) == 0 {
		return fmt.Sprintf("would PUT card %q with no changed fields (no-op)", in.CardID)
	}
	return fmt.Sprintf("would update card %q: %s", in.CardID, strings.Join(changes, ", "))
}

// moveCardStateDiff renders the dry-run state-diff phrase for
// favro_move_card. Listing the destination fields explicitly helps the
// LLM verify it's about to relocate to the right place.
func moveCardStateDiff(in moveCardInput) string {
	var dest []string
	if in.WidgetCommonID != "" {
		dest = append(dest, fmt.Sprintf("widget=%q", in.WidgetCommonID))
	}
	if in.ColumnID != "" {
		dest = append(dest, fmt.Sprintf("column=%q", in.ColumnID))
	}
	if in.LaneID != "" {
		dest = append(dest, fmt.Sprintf("lane=%q", in.LaneID))
	}
	if len(dest) == 0 {
		return fmt.Sprintf("would PUT card %q with no destination set (favro_move_card requires at least one of widget_common_id, column_id, or lane_id)", in.CardID)
	}
	return fmt.Sprintf("would move card %q to %s", in.CardID, strings.Join(dest, ", "))
}
