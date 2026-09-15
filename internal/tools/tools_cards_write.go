package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
	"github.com/mmedum/favro-mcp/internal/favroapi"
	"github.com/mmedum/favro-mcp/internal/service"
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
//
// ClearParent has no counterpart in UpdateCardRequest: it is this
// server's way of saying "yes, detach it", because the handler
// otherwise refills parentCardId on a structural write. See
// preserveParent.
type updateCardInput struct {
	dryRunInput
	CardID              string   `json:"card_id" jsonschema:"the per-widget cardId to update (NOT cardCommonId — Favro PUT /cards/{id} expects the per-widget instance id)"`
	Name                string   `json:"name,omitempty" jsonschema:"new card name; omit to keep current"`
	DetailedDescription string   `json:"detailed_description,omitempty" jsonschema:"replacement markdown body. Phase 6's favro_append/prepend/replace_in_card_description tools provide surgical edits; this one whole-body replaces."`
	WidgetCommonID      string   `json:"widget_common_id,omitempty" jsonschema:"the board the card ends up on; **required whenever column_id or lane_id is set**, and for a same-board move it is the board the card is already on. For relocations prefer the dedicated favro_move_card tool"`
	ColumnID            string   `json:"column_id,omitempty" jsonschema:"move card to a different column. Requires widget_common_id."`
	LaneID              string   `json:"lane_id,omitempty" jsonschema:"move card to a different lane. Requires widget_common_id."`
	ParentCardID        string   `json:"parent_card_id,omitempty" jsonschema:"set or change the parent card"`
	ClearParent         bool     `json:"clear_parent,omitempty" jsonschema:"if true, detach the card from its parent and leave it at top level. Only meaningful on a structural write (one carrying widget_common_id), which is the only write Favro lets a parent be cleared by; without it such a write keeps the existing parent."`
	DragMode            string   `json:"drag_mode,omitempty" jsonschema:"'commit' (Favro's default — neighboring cards re-shuffle) or 'move' (no repositioning of siblings). Only relevant with column_id / list_position / sheet_position."`
	ListPosition        *float64 `json:"list_position,omitempty" jsonschema:"optional position on a kanban widget as a JSON number. 0 places the card at the top of the column; a number larger than the current max sends it to the bottom; fractional values (e.g. 3.5) slot between siblings. A column move does NOT need it — widget_common_id is the field Favro requires there."`
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
	WidgetCommonID string   `json:"widget_common_id,omitempty" jsonschema:"the board the card ends up on (resolve via favro_resolve_widget). **Required whenever column_id or lane_id is set** — Favro answers 200 and moves nothing without it. For a move within one board this is the board the card is already on; favro_get_card returns it. Passing a DIFFERENT board adds the card there and leaves the original in place."`
	ColumnID       string   `json:"column_id,omitempty" jsonschema:"target columnId on the destination widget. Requires widget_common_id."`
	LaneID         string   `json:"lane_id,omitempty" jsonschema:"target laneId on the destination widget. Requires widget_common_id."`
	ListPosition   *float64 `json:"list_position,omitempty" jsonschema:"optional insertion position in the destination column as a JSON number. 0 = top; high number = bottom; fractional slots between siblings. Omit to let Favro place the card."`
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

func registerCreateCard(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
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
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Card, error) {
				return r.Client().CreateCard(writeCtx, favro.CreateCardRequest{
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
			r.InvalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

func registerUpdateCard(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: updateCardToolName,
		Description: "Update a Favro card by its per-widget cardId. Every body field is " +
			"optional — pass at least one. For relocations prefer favro_move_card; for " +
			"archiving prefer favro_archive_card / favro_unarchive_card (clearer LLM intent). " +
			"`detailed_description` whole-body replaces; surgical markdown edits are Phase 6's " +
			"append/prepend/replace tools. Tag mutations use *_tag_ids only. Successful live " +
			"writes invalidate the search-cards cache. Pass `dry_run: true` to preview.\n" +
			"A write carrying `widget_common_id` is structural: Favro re-seats the card from " +
			"the body alone, so a body naming no parent leaves the card at top level — and the " +
			"response echoes a null parent either way, so the answer cannot tell you which " +
			"happened. This tool therefore reads the card first on such a write and carries its " +
			"existing parent through, saying so in `notes`. Pass `clear_parent: true` to detach " +
			"the card on purpose, or `parent_card_id` to re-parent it.",
		Annotations: mutating("Update Favro card", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateCardInput) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
		req := favro.UpdateCardRequest{
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
		}
		notes, err := settleParent(ctx, r, &in, &req)
		if err != nil {
			return nil, writeOutput[favro.Card]{}, err
		}
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Card, error) { return r.Client().UpdateCard(writeCtx, in.CardID, req) },
			func() string { return updateCardStateDiff(in.CardID, &req) },
		)
		if err != nil {
			return nil, writeOutput[favro.Card]{}, err
		}
		out.Notes = append(out.Notes, notes...)
		if !out.DryRun {
			r.InvalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

// settleParent decides what parentCardId the update body carries, and
// reads the card to find out when it has to.
//
// Favro treats a card write carrying widgetCommonId as structural and
// re-seats the card from the body alone: a body naming no parentCardId
// leaves the card at top level. Nothing in the answer says so — the
// response echoes a null parent whether or not the card was nested
// before — so renaming a nested card, or nudging it one column along,
// returns 200 and quietly detaches it. On a sectioned board that reads
// as the card having disappeared, because people read sections.
//
// Hard rule 2 already says a 200 is not confirmation and that a write
// is verified by reading the resource back. A caution in the tool
// description is a guard with no enforcement — it works only if the
// model reads it and obeys it — and the server is the better placed of
// the two to do the read. So this does it, and reports what it did
// rather than refusing: the caller asked for a rename, and a rename is
// what it gets.
//
// The read is skipped entirely unless it can change the outcome, and it
// runs under dry-run too, so the previewed body is the body that would
// be sent (the same choice the description-editor tools make).
func settleParent(ctx context.Context, r *service.Resolver, in *updateCardInput, req *favro.UpdateCardRequest) ([]string, error) {
	structural := req.WidgetCommonID != ""
	switch {
	case !structural && in.ClearParent:
		return []string{"clear_parent had no effect: Favro only re-seats a card on a structural write, which is one carrying widget_common_id. Pass the card's own widget_common_id alongside it to detach the card."}, nil
	case !structural, in.ClearParent, req.ParentCardID != "":
		return nil, nil
	}
	current, err := r.Client().GetCard(ctx, in.CardID)
	if err != nil {
		return nil, fmt.Errorf("refusing a structural update to card %q: its current parent could not be read, and a structural write naming no parent detaches the card from its parent: %w", in.CardID, err)
	}
	if current.ParentCardID == "" {
		return nil, nil
	}
	req.ParentCardID = current.ParentCardID
	return []string{"this write carries widget_common_id, which Favro treats as structural, and a structural write naming no parent leaves the card at top level. The card's existing parent was read back and carried through, so the card stays nested. Pass clear_parent: true to detach it on purpose, or parent_card_id to re-parent it."}, nil
}

func registerArchiveCard(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: archiveCardToolName,
		Description: "Archive a Favro card by its per-widget cardId. Convenience over " +
			"favro_update_card with `archive: true`. Reversible via favro_unarchive_card. " +
			"Successful live writes invalidate the search-cards cache. Pass `dry_run: true` " +
			"to preview without contacting Favro.",
		Annotations: mutating("Archive Favro card", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in archiveCardInput) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Card, error) { return r.Client().ArchiveCard(writeCtx, in.CardID) },
			func() string { return fmt.Sprintf("would archive card %q", in.CardID) },
		)
		if err != nil {
			return nil, writeOutput[favro.Card]{}, err
		}
		if !out.DryRun {
			r.InvalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

func registerUnarchiveCard(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: unarchiveCardToolName,
		Description: "Unarchive a Favro card by its per-widget cardId. Convenience over " +
			"favro_update_card with `archive: false`. Successful live writes invalidate " +
			"the search-cards cache. Pass `dry_run: true` to preview without contacting Favro.",
		Annotations: mutating("Unarchive Favro card", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in unarchiveCardInput) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Card, error) { return r.Client().UnarchiveCard(writeCtx, in.CardID) },
			func() string { return fmt.Sprintf("would unarchive card %q", in.CardID) },
		)
		if err != nil {
			return nil, writeOutput[favro.Card]{}, err
		}
		if !out.DryRun {
			r.InvalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

func registerMoveCard(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: moveCardToolName,
		Description: "Move a Favro card between columns or lanes on a board. At least one " +
			"of widget_common_id / column_id / lane_id must be set, and a column or lane " +
			"move must ALSO pass widget_common_id — the board the card is already on — " +
			"because Favro answers 200 and moves nothing without it. Passing a different " +
			"widget_common_id adds the card to that board and leaves the original where it " +
			"is; Favro has no cross-board relocation. The result is checked against what was " +
			"asked for, so a move Favro accepts and ignores comes back as an error rather " +
			"than a success. Successful live writes invalidate the search-cards cache. Pass " +
			"`dry_run: true` to preview.",
		Annotations: mutating("Move Favro card", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in moveCardInput) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Card, error) {
				return r.Client().MoveCard(writeCtx, in.CardID, favro.MoveCardRequest{
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
			r.InvalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

func registerDeleteCard(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
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
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.DeleteCardResponse, error) {
				return r.Client().DeleteCard(writeCtx, in.CardID, in.Everywhere)
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
			r.InvalidateSearchCardCache()
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
//
// It reads the request the tool built rather than the input it was
// given, so a parent settleParent carried through shows up in the
// preview as a parent the write sets — which is what the body says.
func updateCardStateDiff(cardID string, in *favro.UpdateCardRequest) string {
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
		return fmt.Sprintf("would PUT card %q with no changed fields (no-op)", cardID)
	}
	return fmt.Sprintf("would update card %q: %s", cardID, strings.Join(changes, ", "))
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
