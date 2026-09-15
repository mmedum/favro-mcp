package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mmedum/favro-mcp/internal/favro"
	"github.com/mmedum/favro-mcp/internal/service"
)

// Hard rule 2: a 200 from Favro is not confirmation, and a write is
// verified by reading the resource back. Two card writes fail that way
// in particular, and both were documented in prose in the tool
// descriptions that produce them:
//
//   - a column change without listPosition is accepted and discarded;
//   - a custom-field write to a field the card's widget has not
//     enabled is accepted and discarded.
//
// A caution in a description is a guard with no enforcement — it fires
// only if the model reads it and obeys it — and the server already
// knows which fields it asked Favro to change. So it does the read and
// reports the verdict, rather than asking the caller to.
//
// The read is skipped in dry-run (nothing was written) and when the
// caller passes skip_verify. The verdict is a note, never an error:
// the write happened, and what did or did not come of it is
// information, not a failure of the call.

// verifyInput is embedded in the inputs of the write tools that read
// their own writes back.
type verifyInput struct {
	SkipVerify bool `json:"skip_verify,omitempty" jsonschema:"if true, do not read the card back after the write. The read-back is what turns Favro's 200 into evidence — skip it only when the caller is about to read the card anyway, or when the extra request per write is not affordable."`
}

// verifyCardFields reads the card back and reports, per field, whether
// the value the caller asked for is the value the card now carries.
//
// Only the fields the caller actually set are checked, so a write that
// touched nothing structural costs no read. The comparison is on the
// id fields alone — columnId, laneId, widgetCommonId — where equality
// is exact and a mismatch cannot be a formatting difference.
//
// A read that fails is itself a note rather than an error: the write
// already happened, and reporting "it went through, I could not check"
// is more useful than turning a successful write into a failed call.
func verifyCardFields(ctx context.Context, r *service.Resolver, cardID string, want map[string]string) []string {
	if len(want) == 0 {
		return nil
	}
	card, err := r.Client().GetCard(ctx, cardID)
	if err != nil {
		return []string{fmt.Sprintf("the write was sent, but reading the card back to verify it failed: %v. Favro answers 200 for a body it ignored, so treat this write as unconfirmed.", err)}
	}
	got := map[string]string{
		"column_id":        card.ColumnID,
		"lane_id":          card.LaneID,
		"widget_common_id": card.WidgetCommonID,
	}
	var notes []string
	for _, field := range []string{"widget_common_id", "column_id", "lane_id"} {
		asked, ok := want[field]
		if !ok {
			continue
		}
		if got[field] == asked {
			continue
		}
		notes = append(notes, fmt.Sprintf(
			"%s did not change: the card was read back after the write and still reports %q, not the requested value. Favro answers 200 for a body it ignored.%s",
			field, got[field], positionHint(field)))
	}
	if len(notes) == 0 {
		return []string{"verified by reading the card back: " + joinFields(want) + " match what was requested."}
	}
	return notes
}

// positionHint names the one cause of a discarded move that the caller
// can fix from here. Favro drops a column change whose body carries no
// listPosition, which is what `list_position` has documented in prose.
func positionHint(field string) string {
	if field != "column_id" {
		return ""
	}
	return " A column change needs list_position in the same body — pass it and retry."
}

// verifyCardCustomField reads the card back and reports whether the
// custom field is on it at all.
//
// Presence, not equality: the failure this catches is a field the
// card's widget has not enabled, which Favro accepts and discards, and
// an absent field is the signature of exactly that. Value equality is
// deliberately not asserted — Favro normalises dates and re-shapes
// select values, so comparing would raise false alarms on writes that
// worked. The value it does carry is reported instead, which is enough
// for the caller to see whether it is the one just written.
func verifyCardCustomField(ctx context.Context, r *service.Resolver, cardID string, field favro.CustomField) []string {
	card, err := r.Client().GetCard(ctx, cardID)
	if err != nil {
		return []string{fmt.Sprintf("the write was sent, but reading the card back to verify it failed: %v. Favro answers 200 for a body it ignored, so treat this write as unconfirmed.", err)}
	}
	for _, value := range card.CustomFields() {
		if value.CustomFieldID != field.CustomFieldID {
			continue
		}
		return []string{fmt.Sprintf(
			"verified by reading the card back: the %q field is on the card, carrying %s. Compare it with what was written — Favro normalises some values on the way in.",
			field.Name, renderCustomFieldValue(value))}
	}
	return []string{fmt.Sprintf(
		"the %q field is not on the card after the write. Favro answers 200 for a custom-field write it discarded, and it discards a write to a field the card's widget has not enabled — call favro_get_custom_field to see which widget this field belongs to.",
		field.Name)}
}

// renderCustomFieldValue renders whichever carrier the read-back
// entry populated. The shape depends on the field's type, so this
// picks the first non-empty one rather than assuming.
func renderCustomFieldValue(value favro.CardCustomFieldValue) string {
	switch {
	case len(value.Value) > 0:
		return "value " + string(value.Value)
	case value.Total != nil:
		return fmt.Sprintf("total %g", *value.Total)
	case len(value.CustomFieldItemIDs) > 0:
		return fmt.Sprintf("%d selected item(s)", len(value.CustomFieldItemIDs))
	case value.Color != "":
		return "color " + value.Color
	}
	for _, raw := range []json.RawMessage{value.Timeline, value.Link, value.Reports} {
		if len(raw) > 0 {
			return string(raw)
		}
	}
	return "no value"
}

// joinFields renders the checked field names for the all-clear note,
// in a stable order so the note does not change between identical
// calls (Go randomises map iteration).
func joinFields(want map[string]string) string {
	var names []string
	for _, field := range []string{"widget_common_id", "column_id", "lane_id"} {
		if _, ok := want[field]; ok {
			names = append(names, field)
		}
	}
	return strings.Join(names, ", ")
}

// requestedCardPlacement collects the placement fields a card write
// asked Favro to change. Empty when the write was not a placement
// change, which is what keeps the read-back off every other write.
func requestedCardPlacement(widgetCommonID, columnID, laneID string) map[string]string {
	want := map[string]string{}
	for field, value := range map[string]string{
		"widget_common_id": widgetCommonID,
		"column_id":        columnID,
		"lane_id":          laneID,
	} {
		if value != "" {
			want[field] = value
		}
	}
	return want
}
