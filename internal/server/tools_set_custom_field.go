package server

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const setCardCustomFieldToolName = "favro_set_card_custom_field"

// errUnsupportedCustomFieldType is returned when a caller targets a
// custom-field type the dispatch table doesn't handle. That is either
// because Favro calculates the value server-side and ignores writes
// (Progress, Sequential ID) or because the type has no documented
// write contract (Relations, Date created).
var errUnsupportedCustomFieldType = errors.New("favro: custom-field type cannot be set by favro_set_card_custom_field")

// setCardCustomFieldInput is the input for favro_set_card_custom_field.
// Exactly one *kind* of value input must be supplied, and its kind must
// match the resolved field's Type. A kind may span more than one input
// field (a Timeline needs a start and a due date; Members takes an add
// list and a remove list).
//
// Pointer types wherever an explicit zero or false is a meaningful
// value, so omitempty doesn't elide it.
type setCardCustomFieldInput struct {
	dryRunInput
	CardID        string `json:"card_id" jsonschema:"the per-widget cardId to update"`
	CustomFieldID string `json:"custom_field_id" jsonschema:"the custom field's customFieldId. Resolve via favro_resolve_custom_field."`

	Text     string   `json:"text,omitempty" jsonschema:"new value for a Text-typed field"`
	Number   *float64 `json:"number,omitempty" jsonschema:"new value for a Number-typed field"`
	Date     string   `json:"date,omitempty" jsonschema:"new value for a Date-typed field, ISO 8601 (e.g. 2026-05-06T00:00:00Z)"`
	Checkbox *bool    `json:"checkbox,omitempty" jsonschema:"new value for a Checkbox-typed field"`
	Vote     *bool    `json:"vote,omitempty" jsonschema:"true to vote, false to unvote, on a Vote-typed field"`
	Color    string   `json:"color,omitempty" jsonschema:"card-color token for a Color-typed field (e.g. blue, green-300). Pass an empty-ish sentinel 'none' to clear."`

	SingleSelectItemID string   `json:"single_select_item_id,omitempty" jsonschema:"customFieldItemId for a Single-select field. Look up item ids via favro_get_custom_field."`
	StatusItemID       string   `json:"status_item_id,omitempty" jsonschema:"customFieldItemId for a Status-typed field. Look up item ids via favro_get_custom_field."`
	MultiSelectItemIDs []string `json:"multi_select_item_ids,omitempty" jsonschema:"list of customFieldItemIds for a Multiple-select field; replaces the existing selection. Empty list clears it."`

	AddMemberUserIDs    []string `json:"add_member_user_ids,omitempty" jsonschema:"userIds to add to a Members-typed field. Resolve via favro_resolve_user."`
	RemoveMemberUserIDs []string `json:"remove_member_user_ids,omitempty" jsonschema:"userIds to remove from a Members-typed field. Resolve via favro_resolve_user."`

	AddTagIDs    []string `json:"add_tag_ids,omitempty" jsonschema:"tagIds to add to a Tags-typed field. Resolve names to ids via favro_resolve_tag — adding by name would create unknown tags on typos."`
	RemoveTagIDs []string `json:"remove_tag_ids,omitempty" jsonschema:"tagIds to remove from a Tags-typed field."`

	RatingValue *float64 `json:"rating_value,omitempty" jsonschema:"the rating to set on a Rating-typed field. Favro fixes the scale at 0-5."`

	LinkURL  string `json:"link_url,omitempty" jsonschema:"the URL to set on a Link-typed field. Pair with optional link_text."`
	LinkText string `json:"link_text,omitempty" jsonschema:"optional display text for a Link-typed field; only meaningful with link_url."`

	TimelineStartDate string `json:"timeline_start_date,omitempty" jsonschema:"start date for a Timeline-typed field, ISO 8601. Required together with timeline_due_date."`
	TimelineDueDate   string `json:"timeline_due_date,omitempty" jsonschema:"due date for a Timeline-typed field, ISO 8601. Required together with timeline_start_date."`
	TimelineShowTime  bool   `json:"timeline_show_time,omitempty" jsonschema:"if true, the Timeline field's display text includes the time of day."`

	TimeReportMS          *float64 `json:"time_report_ms,omitempty" jsonschema:"milliseconds to log as a new timesheet entry on a Time-typed field. Adds an entry; it does not replace existing ones."`
	TimeReportDescription string   `json:"time_report_description,omitempty" jsonschema:"optional description for the timesheet entry added via time_report_ms."`

	ForceRefresh bool `json:"force_refresh,omitempty" jsonschema:"if true, bypass the 5-minute custom-field cache when resolving the field's type. Useful when a field was just created or its options changed mid-session."`
}

func registerSetCardCustomField(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: setCardCustomFieldToolName,
		Description: "Set a single custom-field value on a Favro card. Supply exactly one " +
			"kind of value input, matching the resolved field's Type:\n" +
			"  • Text → `text`\n" +
			"  • Number → `number`\n" +
			"  • Date → `date` (ISO 8601)\n" +
			"  • Checkbox → `checkbox`\n" +
			"  • Vote → `vote`\n" +
			"  • Color → `color`\n" +
			"  • Single select → `single_select_item_id`\n" +
			"  • Status → `status_item_id`\n" +
			"  • Multiple select → `multi_select_item_ids` (replaces the selection)\n" +
			"  • Members → `add_member_user_ids` and/or `remove_member_user_ids`\n" +
			"  • Tags → `add_tag_ids` and/or `remove_tag_ids`\n" +
			"  • Rating → `rating_value` (Favro fixes the scale at 0-5)\n" +
			"  • Link → `link_url` + optional `link_text`\n" +
			"  • Timeline → `timeline_start_date` + `timeline_due_date` (+ optional `timeline_show_time`)\n" +
			"  • Time → `time_report_ms` (+ optional `time_report_description`)\n" +
			"Members and Tags are add/remove deltas, not whole-list replacements — " +
			"Favro's API has no replace operation for them. Resolve customFieldId via " +
			"favro_resolve_custom_field; userIds via favro_resolve_user; tagIds via " +
			"favro_resolve_tag; select / status item ids via favro_get_custom_field. " +
			"Progress and Sequential ID are calculated by Favro and reject writes. " +
			"Successful live writes invalidate the search-cards cache. Pass " +
			"`dry_run: true` to preview. The per-type body shapes come from Favro's REST " +
			"docs and have not all been confirmed against a live tenant. Favro answers 200 " +
			"for a body it ignored, and a field not enabled on the widget is accepted and " +
			"discarded, so read the card back with favro_get_card_full to confirm the value " +
			"actually changed.",
		Annotations: mutating("Set Favro card custom field", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in setCardCustomFieldInput) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
		field, err := lookupCustomFieldType(ctx, r, in.CustomFieldID, in.ForceRefresh)
		if err != nil {
			return nil, writeOutput[favro.Card]{}, err
		}
		update, err := buildCustomFieldUpdate(field, &in)
		if err != nil {
			return nil, writeOutput[favro.Card]{}, err
		}
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Card, error) {
				return r.client.UpdateCard(writeCtx, in.CardID, favro.UpdateCardRequest{
					CustomFields: []favro.CardCustomFieldUpdate{update},
				})
			},
			func() string {
				return fmt.Sprintf("would set custom field %q (type %q) on card %q", field.Name, field.Type, in.CardID)
			},
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

// lookupCustomFieldType finds the CustomField in the resolver cache
// (or re-fetches when forceRefresh / cache miss). Returns a
// *NotFoundError-equivalent if the id isn't in the org.
func lookupCustomFieldType(ctx context.Context, r *Resolver, customFieldID string, forceRefresh bool) (favro.CustomField, error) {
	fields, _, err := r.listAllCustomFields(ctx, forceRefresh)
	if err != nil {
		return favro.CustomField{}, err
	}
	for _, f := range fields {
		if f.CustomFieldID == customFieldID {
			return f, nil
		}
	}
	return favro.CustomField{}, fmt.Errorf("favro: custom field %q not found in active organization", customFieldID)
}

// setCFOption maps one kind of value input to the field types it
// serves and the applier that renders it into the wire shape Favro
// documents. fieldTypes is a list because some kinds answer to more
// than one type string (Vote is documented as "Vote" but earlier
// revisions of this client used "Voting"). validate is optional — it
// fires after the picked option matches the field type, keeping
// per-kind extra checks local to their own row.
type setCFOption struct {
	inputName  string
	fieldTypes []string
	isSet      func(*setCardCustomFieldInput) bool
	validate   func(*setCardCustomFieldInput) error
	apply      func(*setCardCustomFieldInput, *favro.CardCustomFieldUpdate)
}

// colorClearSentinel lets a caller clear a Color-typed field. Favro
// clears the value on an empty string, but an empty string is also
// how "input omitted" looks, so the sentinel disambiguates.
const colorClearSentinel = "none"

var setCFOptions = []setCFOption{
	{
		inputName: "text", fieldTypes: []string{cfTypeText},
		isSet: func(in *setCardCustomFieldInput) bool { return in.Text != "" },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) { u.Value = in.Text },
	},
	{
		// Number's documented carrier is `total`, not `value`.
		inputName: "number", fieldTypes: []string{cfTypeNumber},
		isSet: func(in *setCardCustomFieldInput) bool { return in.Number != nil },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) { u.Total = in.Number },
	},
	{
		inputName: "date", fieldTypes: []string{cfTypeDate},
		isSet: func(in *setCardCustomFieldInput) bool { return in.Date != "" },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) { u.Value = in.Date },
	},
	{
		inputName: "checkbox", fieldTypes: []string{cfTypeCheckbox},
		isSet: func(in *setCardCustomFieldInput) bool { return in.Checkbox != nil },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) { u.Value = *in.Checkbox },
	},
	{
		inputName: "vote", fieldTypes: []string{cfTypeVote, cfTypeVoting},
		isSet: func(in *setCardCustomFieldInput) bool { return in.Vote != nil },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) { u.Value = *in.Vote },
	},
	{
		inputName: "color", fieldTypes: []string{cfTypeColor},
		isSet: func(in *setCardCustomFieldInput) bool { return in.Color != "" },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			if in.Color == colorClearSentinel {
				u.Color = ""
				u.Value = ""
				return
			}
			u.Color = in.Color
		},
	},
	{
		// Select-flavored types carry their item ids in `value` as a
		// JSON array, per the documented "Status / Multiple select
		// custom field parameter" shape.
		inputName: "single_select_item_id", fieldTypes: []string{cfTypeSingleSelect},
		isSet: func(in *setCardCustomFieldInput) bool { return in.SingleSelectItemID != "" },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.Value = []string{in.SingleSelectItemID}
		},
	},
	{
		inputName: "status_item_id", fieldTypes: []string{cfTypeStatus},
		isSet: func(in *setCardCustomFieldInput) bool { return in.StatusItemID != "" },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.Value = []string{in.StatusItemID}
		},
	},
	{
		// Non-nil rather than non-empty so an explicit empty array
		// clears the selection — distinct from the omitted case.
		inputName: "multi_select_item_ids", fieldTypes: []string{cfTypeMultipleSelect},
		isSet: func(in *setCardCustomFieldInput) bool { return in.MultiSelectItemIDs != nil },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.Value = in.MultiSelectItemIDs
		},
	},
	{
		// Members is an add/remove delta in Favro's API — there is no
		// whole-list replace, so the tool exposes the delta directly
		// rather than faking a replace with a read-modify-write.
		inputName: "add_member_user_ids / remove_member_user_ids", fieldTypes: []string{cfTypeMembers},
		isSet: func(in *setCardCustomFieldInput) bool {
			return in.AddMemberUserIDs != nil || in.RemoveMemberUserIDs != nil
		},
		validate: func(in *setCardCustomFieldInput) error {
			if len(in.AddMemberUserIDs) == 0 && len(in.RemoveMemberUserIDs) == 0 {
				return errors.New("add_member_user_ids or remove_member_user_ids must name at least one userId")
			}
			return nil
		},
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.Members = &favro.CustomFieldMembersUpdate{
				AddUserIDs:    in.AddMemberUserIDs,
				RemoveUserIDs: in.RemoveMemberUserIDs,
			}
		},
	},
	{
		// Only the by-id forms are exposed. Favro's addTags takes tag
		// *names* and silently creates any that don't exist, which is
		// exactly the typo-created-tag failure the card-level tag
		// tools hard-fail to prevent.
		inputName: "add_tag_ids / remove_tag_ids", fieldTypes: []string{cfTypeTags},
		isSet: func(in *setCardCustomFieldInput) bool {
			return in.AddTagIDs != nil || in.RemoveTagIDs != nil
		},
		validate: func(in *setCardCustomFieldInput) error {
			if len(in.AddTagIDs) == 0 && len(in.RemoveTagIDs) == 0 {
				return errors.New("add_tag_ids or remove_tag_ids must name at least one tagId")
			}
			return nil
		},
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.Tags = &favro.CustomFieldTagsUpdate{
				AddTagIDs:    in.AddTagIDs,
				RemoveTagIDs: in.RemoveTagIDs,
			}
		},
	},
	{
		// Rating's documented carrier is `total`, and the scale is
		// fixed at 0-5 — there is no caller-supplied maximum.
		inputName: "rating_value", fieldTypes: []string{cfTypeRating},
		isSet: func(in *setCardCustomFieldInput) bool { return in.RatingValue != nil },
		validate: func(in *setCardCustomFieldInput) error {
			if *in.RatingValue < 0 || *in.RatingValue > ratingMaxStars {
				return fmt.Errorf("rating_value must be between 0 and %d", ratingMaxStars)
			}
			return nil
		},
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.Total = in.RatingValue
		},
	},
	{
		inputName: "link_url", fieldTypes: []string{cfTypeLink},
		isSet: func(in *setCardCustomFieldInput) bool { return in.LinkURL != "" },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.Link = &favro.CustomFieldLink{URL: in.LinkURL, Text: in.LinkText}
		},
	},
	{
		inputName: "timeline_start_date / timeline_due_date", fieldTypes: []string{cfTypeTimeline},
		isSet: func(in *setCardCustomFieldInput) bool {
			return in.TimelineStartDate != "" || in.TimelineDueDate != ""
		},
		validate: func(in *setCardCustomFieldInput) error {
			if in.TimelineStartDate == "" || in.TimelineDueDate == "" {
				return errors.New("timeline_start_date and timeline_due_date are both required")
			}
			return nil
		},
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.Timeline = &favro.CustomFieldTimeline{
				StartDate: in.TimelineStartDate,
				DueDate:   in.TimelineDueDate,
				ShowTime:  in.TimelineShowTime,
			}
		},
	},
	{
		// Only the "add an entry" case is exposed. Updating or removing
		// an entry needs its reportId, which nothing in the MCP surface
		// currently returns; callers who need it use the raw client.
		inputName: "time_report_ms", fieldTypes: []string{cfTypeTime},
		isSet: func(in *setCardCustomFieldInput) bool { return in.TimeReportMS != nil },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.AddUserReports = []favro.CustomFieldTimeReport{{
				Value:       in.TimeReportMS,
				Description: in.TimeReportDescription,
			}}
		},
	},
}

// buildCustomFieldUpdate validates that the caller supplied exactly
// one kind of value input and that its kind matches the resolved
// field Type. Types outside the dispatch table return
// errUnsupportedCustomFieldType. Single pass over setCFOptions tracks
// both the picked-by-isSet option and the expected-by-fieldType
// option, avoiding redundant scans.
func buildCustomFieldUpdate(field favro.CustomField, in *setCardCustomFieldInput) (favro.CardCustomFieldUpdate, error) {
	var picked, expected *setCFOption
	hits := 0
	for i := range setCFOptions {
		opt := &setCFOptions[i]
		if opt.isSet(in) {
			hits++
			picked = opt
		}
		if slices.Contains(opt.fieldTypes, field.Type) {
			expected = opt
		}
	}
	switch hits {
	case 0:
		return favro.CardCustomFieldUpdate{}, fmt.Errorf("favro: pass exactly one of %s (field %q is %q)", supportedInputNames(), field.Name, field.Type)
	case 1:
		// fall through to type-match check below
	default:
		return favro.CardCustomFieldUpdate{}, fmt.Errorf("favro: pass exactly one kind of value input; got %d", hits)
	}
	if expected == nil {
		return favro.CardCustomFieldUpdate{}, fmt.Errorf("%w: field %q is %q (call favro_get_custom_field to inspect)", errUnsupportedCustomFieldType, field.Name, field.Type)
	}
	if picked != expected {
		return favro.CardCustomFieldUpdate{}, fmt.Errorf("favro: field %q is %q — set %q to match (got %q instead)", field.Name, field.Type, expected.inputName, picked.inputName)
	}
	if picked.validate != nil {
		if err := picked.validate(in); err != nil {
			return favro.CardCustomFieldUpdate{}, fmt.Errorf("favro: field %q is %q — %w", field.Name, field.Type, err)
		}
	}
	update := favro.CardCustomFieldUpdate{CustomFieldID: field.CustomFieldID}
	picked.apply(in, &update)
	return update, nil
}

// supportedInputNames returns the dispatch-table input names joined
// for the no-hits error message, so adding a kind to setCFOptions
// keeps the error in sync without a manual edit.
func supportedInputNames() string {
	names := make([]string, len(setCFOptions))
	for i, opt := range setCFOptions {
		names[i] = opt.inputName
	}
	return strings.Join(names, " / ")
}
