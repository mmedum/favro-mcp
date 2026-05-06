package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const setCardCustomFieldToolName = "favro_set_card_custom_field"

// Custom-field type strings as Favro returns them on CustomField.Type.
// Centralized so the dispatch in setCardCustomField uses one source
// of truth and mismatches surface at compile-time when the resolver
// cache normalizes a new value.
const (
	cfTypeText           = "Text"
	cfTypeNumber         = "Number"
	cfTypeDate           = "Date"
	cfTypeCheckbox       = "Checkbox"
	cfTypeSingleSelect   = "Single select"
	cfTypeMembers        = "Members"
	cfTypeStatus         = "Status"
	cfTypeMultipleSelect = "Multiple select"
	cfTypeRating         = "Rating"
	cfTypeLink           = "Link"
)

// errLongTailCustomFieldType is returned when a caller targets a
// custom-field type the dispatch table doesn't handle. Still
// deferred: Tags, Timeline, Voting, Progress, Relations,
// Sequential ID, Date created.
var errLongTailCustomFieldType = errors.New("favro: custom-field type is not supported by favro_set_card_custom_field yet")

// setCardCustomFieldInput is the input for favro_set_card_custom_field.
// Exactly one value-input field must be set, and its kind must match
// the resolved field's Type.
//
// Pointer types on number / checkbox / rating_value / rating_total
// so an explicit zero/false isn't elided by omitempty — callers do
// legitimately want to set these.
type setCardCustomFieldInput struct {
	dryRunInput
	CardID             string   `json:"card_id" jsonschema:"the per-widget cardId to update"`
	CustomFieldID      string   `json:"custom_field_id" jsonschema:"the custom field's customFieldId. Resolve via favro_resolve_custom_field."`
	Text               string   `json:"text,omitempty" jsonschema:"new value for a Text-typed field"`
	Number             *float64 `json:"number,omitempty" jsonschema:"new value for a Number-typed field"`
	Date               string   `json:"date,omitempty" jsonschema:"new value for a Date-typed field, ISO 8601 (e.g. 2026-05-06T00:00:00Z)"`
	Checkbox           *bool    `json:"checkbox,omitempty" jsonschema:"new value for a Checkbox-typed field"`
	SingleSelectItemID string   `json:"single_select_item_id,omitempty" jsonschema:"customFieldItemId for a Single-select field. Look up item ids via favro_get_custom_field."`
	MemberUserIDs      []string `json:"member_user_ids,omitempty" jsonschema:"list of userIds for a Members-typed field; replaces the existing list. Empty list clears all members. Resolve userIds via favro_resolve_user."`
	StatusItemID       string   `json:"status_item_id,omitempty" jsonschema:"customFieldItemId for a Status-typed field (single status; the status options live on the field's customFieldItems with per-item color)."`
	MultiSelectItemIDs []string `json:"multi_select_item_ids,omitempty" jsonschema:"list of customFieldItemIds for a Multiple-select field; replaces the existing selection."`
	RatingValue        *float64 `json:"rating_value,omitempty" jsonschema:"the rating to set on a Rating-typed field (typically 0–rating_total). Pair with rating_total."`
	RatingTotal        *int     `json:"rating_total,omitempty" jsonschema:"the rating's max-stars on a Rating-typed field (e.g. 5 for 5-star). Pair with rating_value."`
	LinkURL            string   `json:"link_url,omitempty" jsonschema:"the URL to set on a Link-typed field. Pair with optional link_text."`
	LinkText           string   `json:"link_text,omitempty" jsonschema:"optional display text for a Link-typed field; only meaningful with link_url."`
	ForceRefresh       bool     `json:"force_refresh,omitempty" jsonschema:"if true, bypass the 5-minute custom-field cache when resolving the field's type. Useful when a field was just created or its options changed mid-session."`
}

func registerSetCardCustomField(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: setCardCustomFieldToolName,
		Description: "Set a single custom-field value on a Favro card. Pass exactly one " +
			"value-input field matching the resolved field's Type:\n" +
			"  • Text → `text`\n" +
			"  • Number → `number`\n" +
			"  • Date → `date` (ISO 8601)\n" +
			"  • Checkbox → `checkbox`\n" +
			"  • Single select → `single_select_item_id`\n" +
			"  • Members → `member_user_ids` (replaces the list; pass [] to clear)\n" +
			"  • Status → `status_item_id`\n" +
			"  • Multiple select → `multi_select_item_ids` (replaces the selection)\n" +
			"  • Rating → `rating_value` + `rating_total` (both required together)\n" +
			"  • Link → `link_url` + optional `link_text`\n" +
			"Resolve customFieldId via favro_resolve_custom_field; resolve member " +
			"userIds via favro_resolve_user; look up select / status / multi-select " +
			"item ids via favro_get_custom_field. Still-deferred types (Tags, " +
			"Timeline, Voting, Progress, Relations, Sequential ID, Date created) " +
			"return a typed error. Successful live writes invalidate the search-cards " +
			"cache. Pass `dry_run: true` to preview.",
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

// setCFOption maps one input value field to its matching field-type
// + applier. Adding a new kind is one new entry. validate is
// optional — fires after the picked option matches the field type
// and lets a per-kind extra check (e.g. Rating's required-pair)
// stay local to its own row instead of leaking into the generic
// builder.
type setCFOption struct {
	inputName string
	fieldType string
	isSet     func(*setCardCustomFieldInput) bool
	validate  func(*setCardCustomFieldInput) error
	apply     func(*setCardCustomFieldInput, *favro.CardCustomFieldUpdate)
}

var setCFOptions = []setCFOption{
	{
		inputName: "text", fieldType: cfTypeText,
		isSet: func(in *setCardCustomFieldInput) bool { return in.Text != "" },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) { u.Value = in.Text },
	},
	{
		inputName: "number", fieldType: cfTypeNumber,
		isSet: func(in *setCardCustomFieldInput) bool { return in.Number != nil },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) { u.Value = *in.Number },
	},
	{
		inputName: "date", fieldType: cfTypeDate,
		isSet: func(in *setCardCustomFieldInput) bool { return in.Date != "" },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) { u.Value = in.Date },
	},
	{
		inputName: "checkbox", fieldType: cfTypeCheckbox,
		isSet: func(in *setCardCustomFieldInput) bool { return in.Checkbox != nil },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) { u.Value = *in.Checkbox },
	},
	{
		inputName: "single_select_item_id", fieldType: cfTypeSingleSelect,
		isSet: func(in *setCardCustomFieldInput) bool { return in.SingleSelectItemID != "" },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.CustomFieldItemIDs = []string{in.SingleSelectItemID}
		},
	},
	{
		// Members input is non-nil-checked to allow an explicit empty
		// list (clear all members) — distinct from the omitted case.
		inputName: "member_user_ids", fieldType: cfTypeMembers,
		isSet: func(in *setCardCustomFieldInput) bool { return in.MemberUserIDs != nil },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.Value = in.MemberUserIDs
		},
	},
	{
		inputName: "status_item_id", fieldType: cfTypeStatus,
		isSet: func(in *setCardCustomFieldInput) bool { return in.StatusItemID != "" },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.CustomFieldItemIDs = []string{in.StatusItemID}
		},
	},
	{
		// Multi-select replaces the current selection; non-nil
		// signals "set the field" — empty slice clears the selection.
		inputName: "multi_select_item_ids", fieldType: cfTypeMultipleSelect,
		isSet: func(in *setCardCustomFieldInput) bool { return in.MultiSelectItemIDs != nil },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.CustomFieldItemIDs = in.MultiSelectItemIDs
		},
	},
	{
		// Rating requires both rating_value and rating_total — the
		// validate callback enforces the pair so a half-set request
		// fails fast, before any HTTP work.
		inputName: "rating_value", fieldType: cfTypeRating,
		isSet: func(in *setCardCustomFieldInput) bool { return in.RatingValue != nil },
		validate: func(in *setCardCustomFieldInput) error {
			if in.RatingTotal == nil {
				return errors.New("rating_value requires rating_total (max stars) to be set together")
			}
			return nil
		},
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.Value = *in.RatingValue
			u.Total = in.RatingTotal
		},
	},
	{
		inputName: "link_url", fieldType: cfTypeLink,
		isSet: func(in *setCardCustomFieldInput) bool { return in.LinkURL != "" },
		apply: func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.Value = in.LinkURL
			u.LinkText = in.LinkText
		},
	},
}

// buildCustomFieldUpdate validates that the caller supplied exactly
// one value-input field and that its kind matches the resolved
// field Type. Types outside the dispatch table return
// errLongTailCustomFieldType. Single pass over setCFOptions tracks
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
		if opt.fieldType == field.Type {
			expected = opt
		}
	}
	switch hits {
	case 0:
		return favro.CardCustomFieldUpdate{}, fmt.Errorf("favro: pass exactly one of %s (field %q is %q)", supportedInputNames(), field.Name, field.Type)
	case 1:
		// fall through to type-match check below
	default:
		return favro.CardCustomFieldUpdate{}, fmt.Errorf("favro: pass exactly one value field; got %d", hits)
	}
	if expected == nil {
		return favro.CardCustomFieldUpdate{}, fmt.Errorf("%w: field %q is %q (call favro_get_custom_field to inspect)", errLongTailCustomFieldType, field.Name, field.Type)
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
