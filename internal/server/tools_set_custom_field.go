package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const setCardCustomFieldToolName = "favro_set_card_custom_field"

// Custom-field type strings as Favro returns them on CustomField.Type.
// Centralized so the dispatch in setCardCustomField uses one source
// of truth and mismatches surface at compile-time when the resolver
// cache normalizes a new value.
const (
	cfTypeText         = "Text"
	cfTypeNumber       = "Number"
	cfTypeDate         = "Date"
	cfTypeCheckbox     = "Checkbox"
	cfTypeSingleSelect = "Single select"
)

// errLongTailCustomFieldType is returned when a caller targets a
// custom-field type Phase 5.5 doesn't support yet (Members, Status,
// Multi-select, Rating, Link, Timeline, Voting, Progress, Relations,
// Sequential ID, Tags, Date created). Phase 7 covers the rest.
var errLongTailCustomFieldType = errors.New("favro: custom-field type is not supported by favro_set_card_custom_field yet (deferred to Phase 7)")

// setCardCustomFieldInput is the input for favro_set_card_custom_field.
// Exactly one of text / number / date / checkbox / single_select_item_id
// must be set, and its kind must match the resolved field's Type.
//
// Pointer types on number / checkbox so an explicit 0 / false isn't
// elided by omitempty (callers do legitimately want to set these
// values on Number / Checkbox fields).
type setCardCustomFieldInput struct {
	dryRunInput
	CardID             string   `json:"card_id" jsonschema:"the per-widget cardId to update"`
	CustomFieldID      string   `json:"custom_field_id" jsonschema:"the custom field's customFieldId. Resolve via favro_resolve_custom_field."`
	Text               string   `json:"text,omitempty" jsonschema:"new value for a Text-typed field; pass exactly one of text / number / date / checkbox / single_select_item_id"`
	Number             *float64 `json:"number,omitempty" jsonschema:"new value for a Number-typed field"`
	Date               string   `json:"date,omitempty" jsonschema:"new value for a Date-typed field, ISO 8601 (e.g. 2026-05-06T00:00:00Z)"`
	Checkbox           *bool    `json:"checkbox,omitempty" jsonschema:"new value for a Checkbox-typed field"`
	SingleSelectItemID string   `json:"single_select_item_id,omitempty" jsonschema:"customFieldItemId for a Single-select-typed field. Look up the item ids via favro_get_custom_field."`
	ForceRefresh       bool     `json:"force_refresh,omitempty" jsonschema:"if true, bypass the 5-minute custom-field cache when resolving the field's type. Useful when a field was just created or its options changed mid-session."`
}

func registerSetCardCustomField(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: setCardCustomFieldToolName,
		Description: "Set a single custom-field value on a Favro card. Phase 5.5 supports " +
			"the simple types: Text, Number, Date, Checkbox, Single select. Long-tail types " +
			"(Members, Status, Multi-select, Rating, Link) return a typed error pointing to " +
			"Phase 7. Pass exactly one of `text` / `number` / `date` / `checkbox` / " +
			"`single_select_item_id` matching the resolved field's Type. Successful live " +
			"writes invalidate the search-cards cache (cached card payloads carry stale " +
			"customFieldsValues otherwise). Pass `dry_run: true` to preview.",
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
// + applier. Driven by the table below — adding a Phase-7 long-tail
// kind is one new entry.
type setCFOption struct {
	inputName string
	fieldType string
	isSet     func(*setCardCustomFieldInput) bool
	apply     func(*setCardCustomFieldInput, *favro.CardCustomFieldUpdate)
}

var setCFOptions = []setCFOption{
	{
		"text", cfTypeText,
		func(in *setCardCustomFieldInput) bool { return in.Text != "" },
		func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) { u.Value = in.Text },
	},
	{
		"number", cfTypeNumber,
		func(in *setCardCustomFieldInput) bool { return in.Number != nil },
		func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) { u.Value = *in.Number },
	},
	{
		"date", cfTypeDate,
		func(in *setCardCustomFieldInput) bool { return in.Date != "" },
		func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) { u.Value = in.Date },
	},
	{
		"checkbox", cfTypeCheckbox,
		func(in *setCardCustomFieldInput) bool { return in.Checkbox != nil },
		func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) { u.Value = *in.Checkbox },
	},
	{
		"single_select_item_id", cfTypeSingleSelect,
		func(in *setCardCustomFieldInput) bool { return in.SingleSelectItemID != "" },
		func(in *setCardCustomFieldInput, u *favro.CardCustomFieldUpdate) {
			u.CustomFieldItemIDs = []string{in.SingleSelectItemID}
		},
	},
}

// buildCustomFieldUpdate validates that the caller supplied exactly
// one value-input field and that its kind matches the resolved field
// Type. Long-tail types (Members, Status, Multi-select, Rating,
// Link, etc.) — anything not covered by setCFOptions — return
// errLongTailCustomFieldType pointing the caller to Phase 7.
//
// Single pass over setCFOptions tracks both the picked-by-isSet
// option and the expected-by-fieldType option, avoiding redundant
// scans for the long-tail check / mismatch error message.
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
		return favro.CardCustomFieldUpdate{}, fmt.Errorf("favro: pass exactly one of text / number / date / checkbox / single_select_item_id (field %q is %q)", field.Name, field.Type)
	case 1:
		// fall through to type-match check below
	default:
		return favro.CardCustomFieldUpdate{}, fmt.Errorf("favro: pass exactly one value field; got %d", hits)
	}
	if expected == nil {
		return favro.CardCustomFieldUpdate{}, fmt.Errorf("%w: field %q is %q (call favro_get_custom_field to inspect; Phase 7 will add this type)", errLongTailCustomFieldType, field.Name, field.Type)
	}
	if picked != expected {
		return favro.CardCustomFieldUpdate{}, fmt.Errorf("favro: field %q is %q — set %q to match (got %q instead)", field.Name, field.Type, expected.inputName, picked.inputName)
	}
	update := favro.CardCustomFieldUpdate{CustomFieldID: field.CustomFieldID}
	picked.apply(in, &update)
	return update, nil
}
