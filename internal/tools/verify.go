package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mmedum/favro-mcp/v3/internal/favro"
	"github.com/mmedum/favro-mcp/v3/internal/service"
)

// Hard rule 2: a 200 from Favro is not confirmation, and a write is
// verified by reading the resource back. favroapi.UpdateCard already
// compares a move's own response with what was asked for, but that
// response comes from the same request handling that ignored the body,
// so a response echoing the requested column while storing nothing
// passes it. Only a GET afterwards is evidence.
//
// The same holds for a custom-field write: Favro answers 200, with the
// full card, for a write to a field the card's widget has not enabled,
// and the field is simply absent from the card.
//
// So the tools that make those writes read the card back and report
// what they saw as a note. The response check in favroapi stays an
// error: it fires only when Favro's own answer says the move did not
// happen. The read-back is a note because the write was accepted, and
// because an absent custom field is also what a write that cleared the
// field leaves. The read is skipped in dry-run, on a write with nothing
// to check, and when the caller passes skip_verify.

// verifyInput is embedded in the inputs of the write tools that read
// their own writes back.
type verifyInput struct {
	SkipVerify bool `json:"skip_verify,omitempty" jsonschema:"if true, do not read the card back after the write. The read-back costs one extra request and is what turns Favro's 200 into evidence — skip it only when the caller is about to read the card anyway."`
}

// readBack appends check's notes to a live write's output, unless the
// caller passed skip_verify. Dry-run wrote nothing, so there is nothing
// to read.
func (v verifyInput) readBack(out *writeOutput[favro.Card], check func() []string) {
	if out.DryRun || v.SkipVerify {
		return
	}
	out.Notes = append(out.Notes, check()...)
}

// verifyPlacementDescription is the part of favro_update_card's and
// favro_move_card's descriptions that says what the read-back does.
const verifyPlacementDescription = "A write that sets `column_id` or `lane_id` is then read " +
	"back, and whether the card is where it was asked to be is reported in `notes` — the " +
	"response alone cannot say, because a 200 echoing the requested column is also what an " +
	"ignored write returns. That costs one request; pass `skip_verify: true` to drop it. "

// The notes the read-backs emit, next to each other so their wording
// can be read as one vocabulary.
const (
	noteReadBackFailed    = "the write was sent, but reading the card back to verify it failed (%v). Favro answers 200 for a body it ignored, so treat this write as unconfirmed."
	notePlacementVerified = "verified by reading the card back: it is in the requested %s."
	notePlacementIgnored  = "%s did not change: the card was read back after the write and reports %q, not %q. Favro answered 200 for a move it did not store."
	notePlacementElsewise = "not verified: the card read back is on board %q, not the %q this write named. A write naming another board adds the card there and leaves this instance where it was, so compare on the target board."
	noteLaneUnreadable    = "lane_id not verified: the card read back carries no laneId, which is also what a card on a board without lanes carries."
	noteFieldPresent      = "verified by reading the card back: the %q field is on the card, carrying %s. Favro normalizes some values on the way in, so compare it with what was written."
	noteFieldAbsent       = "the %q field is not on the card after the write. Favro answers 200 for a custom-field write it discarded, which is what it does when the card's widget has not enabled the field. If this write cleared the field, absence is expected."
)

// placement is where a write asked Favro to put a card.
type placement struct {
	WidgetCommonID, ColumnID, LaneID string
}

// verifyPlacement reads back the card a move produced and reports
// whether it sits in the column and lane the write asked for. Only
// columnId and laneId are compared: they are ids, so a mismatch cannot
// be a formatting difference.
//
// widgetCommonId is deliberately not compared. A write naming another
// board adds an instance there and leaves the source where it is, so
// the source correctly still reports its old board. When the card read
// back is not on the requested board, the column cannot be judged from
// it either, and the note says so rather than guessing.
//
// The cardId in Favro's answer is read in preference to the one the
// caller passed, falling back when the answer carried none.
func verifyPlacement(ctx context.Context, r *service.Resolver, cardID string, written *favro.Card, want placement) []string {
	if want.ColumnID == "" && want.LaneID == "" {
		return nil
	}
	if written != nil && written.CardID != "" {
		cardID = written.CardID
	}
	card, err := r.Client().GetCard(ctx, cardID)
	if err != nil {
		return []string{fmt.Sprintf(noteReadBackFailed, err)}
	}
	if card.WidgetCommonID != want.WidgetCommonID {
		return []string{fmt.Sprintf(notePlacementElsewise, card.WidgetCommonID, want.WidgetCommonID)}
	}
	return placementVerdict(card, want)
}

// placementVerdict compares a card read back on the requested board
// with the column and lane the write asked for.
func placementVerdict(card favro.Card, want placement) []string {
	columnOK := want.ColumnID != "" && card.ColumnID == want.ColumnID
	laneOK := want.LaneID != "" && card.LaneID == want.LaneID

	var notes []string
	if verified := verifiedPlaces(columnOK, laneOK); verified != "" {
		notes = append(notes, fmt.Sprintf(notePlacementVerified, verified))
	}
	if want.ColumnID != "" && !columnOK {
		notes = append(notes, fmt.Sprintf(notePlacementIgnored, "column_id", card.ColumnID, want.ColumnID))
	}
	if want.LaneID != "" && !laneOK {
		notes = append(notes, laneMismatch(card.LaneID, want.LaneID))
	}
	return notes
}

// verifiedPlaces names what the read-back confirmed, or "" for nothing.
func verifiedPlaces(columnOK, laneOK bool) string {
	switch {
	case columnOK && laneOK:
		return "column and lane"
	case columnOK:
		return "column"
	case laneOK:
		return "lane"
	}
	return ""
}

// laneMismatch explains a lane that did not read back as requested. A
// card on a board without lanes comes back with no laneId, so an empty
// one is not evidence either way (the same reason favroapi.UpdateCard
// leaves lanes out of its response check).
func laneMismatch(got, want string) string {
	if got == "" {
		return noteLaneUnreadable
	}
	return fmt.Sprintf(notePlacementIgnored, "lane_id", got, want)
}

// verifyCustomField reads the card back and reports whether the field
// is on it, and with what value.
//
// Presence, not equality: Favro normalizes dates and re-shapes select
// values, so comparing values would raise false alarms on writes that
// worked. An absent field is the signature of the documented failure.
// The value is rendered the way favro_get_card_full renders it, so the
// caller can judge the rest.
func verifyCustomField(ctx context.Context, r *service.Resolver, cardID string, field favro.CustomField) []string {
	card, err := r.Client().GetCard(ctx, cardID)
	if err != nil {
		return []string{fmt.Sprintf(noteReadBackFailed, err)}
	}
	for _, value := range card.CustomFields() {
		if value.CustomFieldID != field.CustomFieldID {
			continue
		}
		shown, ok := service.FormatCustomFieldValue(value, field)
		if !ok {
			raw, _ := json.Marshal(value)
			shown = string(raw)
		}
		return []string{fmt.Sprintf(noteFieldPresent, field.Name, shown)}
	}
	return []string{fmt.Sprintf(noteFieldAbsent, field.Name)}
}
