package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// FullCard is the output of favro_get_card_full: a card with every
// id field dereferenced into a human-readable name, plus optional
// comments. Saves the LLM 4–7 follow-up calls in the typical
// "fetch card → resolve tags → resolve users → resolve widget →
// resolve column → list comments" flow.
//
// Both the raw favro.Card (with bare IDs) and the per-aspect
// Resolved* projections are kept on purpose: the LLM uses the raw
// IDs when chaining to mutating tools and the resolved names when
// composing prose. Trimming either view costs a follow-up call.
type FullCard struct {
	favro.Card

	WidgetName           string                    `json:"widget_name,omitempty"`
	ColumnName           string                    `json:"column_name,omitempty"`
	CollectionNames      []string                  `json:"collection_names,omitempty"`
	ResolvedTags         []ResolvedCardTag         `json:"resolved_tags,omitempty"`
	ResolvedAssignments  []ResolvedCardAssignment  `json:"resolved_assignments,omitempty"`
	ResolvedCustomFields []ResolvedCardCustomField `json:"resolved_custom_fields,omitempty"`
	Comments             []favro.Comment           `json:"comments,omitempty"`
}

// ResolvedCardTag is one tag from a card's Tags list, dereferenced
// against the org-global tag list.
type ResolvedCardTag struct {
	TagID string `json:"tag_id"`
	Name  string `json:"name,omitempty"`
	Color string `json:"color,omitempty"`
}

// ResolvedCardAssignment is one entry in a card's Assignments list,
// dereferenced against the org-global user list. Email is included
// because two users may share a display name and the email is the
// disambiguator humans actually use.
type ResolvedCardAssignment struct {
	UserID    string `json:"user_id"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
	Completed bool   `json:"completed,omitempty"`
}

// ResolvedCardCustomField is one custom-field value, dereferenced
// against the org-global custom-field list. DisplayValue is a
// human-readable string when the kind has a clear rendering;
// Dereferenced is true when DisplayValue was produced from a
// known-shape value, false when the kind isn't supported or the
// raw value couldn't be decoded. Raw always carries the original
// Favro value so callers can fall back when they need to.
type ResolvedCardCustomField struct {
	CustomFieldID string          `json:"custom_field_id"`
	Name          string          `json:"name,omitempty"`
	Type          string          `json:"type,omitempty"`
	DisplayValue  string          `json:"display_value,omitempty"`
	Dereferenced  bool            `json:"dereferenced"`
	Raw           json.RawMessage `json:"raw,omitempty"`
}

// FullCardIdentity locates one card. Exactly one of CardID,
// CardCommonID, SequentialID must be non-zero; Validate enforces
// the contract before any HTTP work.
type FullCardIdentity struct {
	CardID       string
	CardCommonID string
	SequentialID int
}

// Validate returns errFullCardIdentityRequired when fewer or more
// than one identity field is set.
func (id FullCardIdentity) Validate() error {
	set := 0
	if id.CardID != "" {
		set++
	}
	if id.CardCommonID != "" {
		set++
	}
	if id.SequentialID > 0 {
		set++
	}
	if set != 1 {
		return errFullCardIdentityRequired
	}
	return nil
}

// errFullCardIdentityRequired is returned when the caller does not
// supply exactly one of card_id / card_common_id / sequential_id.
var errFullCardIdentityRequired = errors.New("favro_get_card_full: pass exactly one of card_id, card_common_id, sequential_id")

// errFullCardNotFound is returned when the identity matches no card
// in the bound organization. Distinct from a HTTP 404 on
// /cards/{cardId} — this fires when ListCards with the filter
// returns zero entities.
var errFullCardNotFound = errors.New("favro_get_card_full: no card matched the supplied identity")

// fullCardCommentDefaultLimit caps comments fetched on the implicit
// path. 20 is enough for the typical card's recent activity without
// burning a slot of the rate-limit budget on a 100-comment thread
// when the LLM only asked for context.
const fullCardCommentDefaultLimit = 20

// GetFullCard composes resolver caches plus a single comments call
// to dereference one card's id fields into human-readable names.
//
// The five dereference steps run in parallel via errgroup. They
// touch disjoint fields of the FullCard, so no synchronization is
// needed beyond errgroup.Wait. On a cold cache the parallel
// fan-out collapses ~1.5s of sequential round-trips down to ~300ms;
// on a warm cache every step is in-process map lookups and the
// goroutine overhead is trivial.
func (r *Resolver) GetFullCard(ctx context.Context, id FullCardIdentity, includeComments bool, commentLimit int) (FullCard, error) {
	if err := id.Validate(); err != nil {
		return FullCard{}, err
	}
	card, err := r.fetchCardForIdentity(ctx, id)
	if err != nil {
		return FullCard{}, err
	}

	full := FullCard{Card: card}
	if commentLimit <= 0 {
		commentLimit = fullCardCommentDefaultLimit
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return r.dereferenceTags(gctx, &full) })
	g.Go(func() error { return r.dereferenceAssignments(gctx, &full) })
	g.Go(func() error { return r.dereferenceWidgetContext(gctx, &full) })
	g.Go(func() error { return r.dereferenceCustomFields(gctx, &full) })
	if includeComments {
		g.Go(func() error { return r.dereferenceComments(gctx, &full, commentLimit) })
	}
	if err := g.Wait(); err != nil {
		return FullCard{}, err
	}
	return full, nil
}

func (r *Resolver) dereferenceTags(ctx context.Context, full *FullCard) error {
	if len(full.Tags) == 0 {
		return nil
	}
	tags, _, err := r.listAllTags(ctx, false)
	if err != nil {
		return err
	}
	full.ResolvedTags = projectCardTags(full.Tags, tags)
	return nil
}

func (r *Resolver) dereferenceAssignments(ctx context.Context, full *FullCard) error {
	if len(full.Assignments) == 0 {
		return nil
	}
	users, _, err := r.listAllUsers(ctx, false)
	if err != nil {
		return err
	}
	full.ResolvedAssignments = projectCardAssignments(full.Assignments, users)
	return nil
}

func (r *Resolver) dereferenceCustomFields(ctx context.Context, full *FullCard) error {
	if len(full.CustomFieldsValues) == 0 {
		return nil
	}
	fields, _, err := r.listAllCustomFields(ctx, false)
	if err != nil {
		return err
	}
	// Build the field-byID lookup once and share it with both the
	// kind-presence check and the per-value projection — the
	// reads-side hot path scans the same map up to three times
	// otherwise (once per cardHasFieldType call, once again inside
	// projectCustomFieldValues).
	fieldsByID := indexByID(fields, func(f favro.CustomField) string { return f.CustomFieldID })
	present := presentFieldTypes(full.CustomFieldsValues, fieldsByID)

	// Members + Tags formatters need the org-global user / tag list
	// to dereference IDs into names. Fetch only when the card
	// actually has a value of that kind so the typical card
	// (no Members or Tags custom field) still pays one
	// listAllCustomFields call.
	var users []favro.User
	var tags []favro.Tag
	if present[cfTypeMembers] {
		users, _, err = r.listAllUsers(ctx, false)
		if err != nil {
			return err
		}
	}
	if present[cfTypeTags] {
		tags, _, err = r.listAllTags(ctx, false)
		if err != nil {
			return err
		}
	}
	full.ResolvedCustomFields = projectCustomFieldValues(full.CustomFieldsValues, fieldsByID, users, tags)
	return nil
}

// presentFieldTypes returns the set of CustomField.Type values
// represented on the card. Built from the precomputed fieldsByID
// so callers don't pay the indexByID cost twice.
func presentFieldTypes(values []favro.CardCustomFieldValue, fieldsByID map[string]favro.CustomField) map[string]bool {
	present := map[string]bool{}
	for _, v := range values {
		if f, ok := fieldsByID[v.CustomFieldID]; ok {
			present[f.Type] = true
		}
	}
	return present
}

func (r *Resolver) dereferenceComments(ctx context.Context, full *FullCard, commentLimit int) error {
	if full.CardCommonID == "" {
		return nil
	}
	comments, err := r.listFirstComments(ctx, full.CardCommonID, commentLimit)
	if err != nil {
		return err
	}
	full.Comments = comments
	return nil
}

// fetchCardForIdentity normalises the three identity flavors into a
// single Card. card_id calls GET /cards/{cardId} directly; the
// other two call ListCards with the matching filter and take the
// first row.
//
// Unique is left false so the response includes per-widget
// instances (each carries widgetCommonId / columnId). Setting
// Unique=true with cardCommonId returns a cross-widget rollup row
// that omits widget context — observed live and breaks the
// downstream widget/column/collection dereference. The caller must
// Validate the identity first; this function trusts that exactly
// one field is set.
func (r *Resolver) fetchCardForIdentity(ctx context.Context, id FullCardIdentity) (favro.Card, error) {
	if id.CardID != "" {
		return r.client.GetCard(ctx, id.CardID)
	}

	filter := favro.ListCardsFilter{}
	if id.CardCommonID != "" {
		filter.CardCommonID = id.CardCommonID
	}
	if id.SequentialID > 0 {
		filter.SequentialID = id.SequentialID
	}
	env, err := r.client.ListCards(ctx, 0, "", filter)
	if err != nil {
		return favro.Card{}, err
	}
	if len(env.Entities) == 0 {
		return favro.Card{}, errFullCardNotFound
	}
	return env.Entities[0], nil
}

// dereferenceWidgetContext fills the widget name, the column name
// (looked up against the per-widget column cache), and the parent
// collection names. Missing widget / column / collection cached
// entries leave the relevant output field empty rather than
// erroring — the rest of the dereference is still useful.
func (r *Resolver) dereferenceWidgetContext(ctx context.Context, full *FullCard) error {
	if full.WidgetCommonID == "" {
		return nil
	}
	widgets, _, err := r.listAllWidgets(ctx, false)
	if err != nil {
		return err
	}
	w := findWidget(widgets, full.WidgetCommonID)
	if w == nil {
		return nil
	}
	full.WidgetName = w.Name

	if len(w.CollectionIDs) > 0 {
		collections, _, err := r.listAllCollections(ctx, false)
		if err != nil {
			return err
		}
		full.CollectionNames = projectCollectionNames(w.CollectionIDs, collections)
	}

	if full.ColumnID != "" {
		// Prefer the embedded Widget.Columns summary (id+name+color)
		// when populated — saves a /columns round-trip on cold cache.
		// Fall back to the standalone /columns endpoint when the
		// widget object didn't carry the summary.
		if name := findColumnNameInWidget(w.Columns, full.ColumnID); name != "" {
			full.ColumnName = name
		} else {
			columns, _, err := r.listColumnsForWidget(ctx, full.WidgetCommonID, false)
			if err != nil {
				return err
			}
			full.ColumnName = findColumnName(columns, full.ColumnID)
		}
	}
	return nil
}

// listFirstComments fetches the first page of /comments scoped to
// cardCommonID and trims to limit. Plan §6 says list tools never
// auto-aggregate; full-card composition is the one exception
// because the caller asked explicitly via include_comments.
func (r *Resolver) listFirstComments(ctx context.Context, cardCommonID string, limit int) ([]favro.Comment, error) {
	env, err := r.client.ListComments(ctx, 0, "", cardCommonID)
	if err != nil {
		return nil, err
	}
	comments := env.Entities
	if len(comments) > limit {
		comments = comments[:limit]
	}
	return comments, nil
}

// ============================================================
// Projections — pure helpers, exported for unit tests.
// ============================================================

func findWidget(widgets []favro.Widget, widgetCommonID string) *favro.Widget {
	for i, w := range widgets {
		if w.WidgetCommonID == widgetCommonID {
			return &widgets[i]
		}
	}
	return nil
}

func findColumnName(columns []favro.Column, columnID string) string {
	for _, c := range columns {
		if c.ColumnID == columnID {
			return c.Name
		}
	}
	return ""
}

// findColumnNameInWidget looks up columnID against the widget's
// embedded denormalized columns summary. Returns "" when the widget
// object didn't carry the summary, so the caller can fall back to a
// /columns fetch.
func findColumnNameInWidget(columns []favro.WidgetColumn, columnID string) string {
	for _, c := range columns {
		if c.ColumnID == columnID {
			return c.Name
		}
	}
	return ""
}

// indexByID builds an id → item lookup map from a slice. Keeps the
// three project<X> helpers below from re-implementing the same
// 4-line loop with type-specific fields.
func indexByID[T any](items []T, key func(T) string) map[string]T {
	out := make(map[string]T, len(items))
	for _, item := range items {
		out[key(item)] = item
	}
	return out
}

func projectCardTags(tagIDs []string, tags []favro.Tag) []ResolvedCardTag {
	byID := indexByID(tags, func(t favro.Tag) string { return t.TagID })
	out := make([]ResolvedCardTag, 0, len(tagIDs))
	for _, id := range tagIDs {
		rt := ResolvedCardTag{TagID: id}
		if t, ok := byID[id]; ok {
			rt.Name = t.Name
			rt.Color = t.Color
		}
		out = append(out, rt)
	}
	return out
}

func projectCardAssignments(assignments []favro.CardAssignment, users []favro.User) []ResolvedCardAssignment {
	byID := indexByID(users, func(u favro.User) string { return u.UserID })
	out := make([]ResolvedCardAssignment, 0, len(assignments))
	for _, a := range assignments {
		ra := ResolvedCardAssignment{UserID: a.UserID, Completed: a.Completed}
		if u, ok := byID[a.UserID]; ok {
			ra.Name = u.Name
			ra.Email = u.Email
		}
		out = append(out, ra)
	}
	return out
}

func projectCollectionNames(collectionIDs []string, collections []favro.Collection) []string {
	byID := indexByID(collections, func(c favro.Collection) string { return c.CollectionID })
	out := make([]string, 0, len(collectionIDs))
	for _, id := range collectionIDs {
		if c, ok := byID[id]; ok {
			out = append(out, c.Name)
		}
	}
	return out
}

// projectCustomFieldValues projects each per-card value into a
// ResolvedCardCustomField by name + Type + display value. The
// caller supplies a precomputed fields-by-id map so the same map
// can serve the kind-presence check and this projection without
// rebuilding (the reads-side hot path scans the same map otherwise).
func projectCustomFieldValues(values []favro.CardCustomFieldValue, fieldsByID map[string]favro.CustomField, users []favro.User, tags []favro.Tag) []ResolvedCardCustomField {
	out := make([]ResolvedCardCustomField, 0, len(values))
	for _, v := range values {
		rc := ResolvedCardCustomField{CustomFieldID: v.CustomFieldID, Raw: v.Value}
		if f, ok := fieldsByID[v.CustomFieldID]; ok {
			rc.Name = f.Name
			rc.Type = f.Type
			rc.DisplayValue, rc.Dereferenced = formatCustomFieldValue(v, f, users, tags)
		}
		out = append(out, rc)
	}
	return out
}

// cfValueFormatter is the uniform signature every per-kind
// formatter is wrapped to so the dispatch in
// formatCustomFieldValue stays a single map lookup. Each entry in
// customFieldFormatters consumes whichever subset of (v, f, users,
// tags) it needs and returns the (display, dereferenced) tuple.
type cfValueFormatter func(v favro.CardCustomFieldValue, f favro.CustomField, users []favro.User, tags []favro.Tag) (string, bool)

// customFieldFormatters maps the Favro custom-field Type to the
// formatter that renders one per-card value. Multiple Types may
// map to the same formatter (Checkbox/Voting share the JSON-bool
// decode; Date/Date created share the string decode). Adding a
// new kind is one row.
var customFieldFormatters = map[string]cfValueFormatter{
	cfTypeText: func(v favro.CardCustomFieldValue, _ favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return decodeJSONString(v.Value)
	},
	cfTypeLink: func(v favro.CardCustomFieldValue, _ favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return decodeJSONString(v.Value)
	},
	cfTypeDate: func(v favro.CardCustomFieldValue, _ favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return decodeJSONString(v.Value)
	},
	cfTypeDateCreated: func(v favro.CardCustomFieldValue, _ favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return decodeJSONString(v.Value)
	},
	cfTypeNumber: func(v favro.CardCustomFieldValue, _ favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return decodeJSONNumber(v.Value)
	},
	cfTypeCheckbox: func(v favro.CardCustomFieldValue, _ favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return decodeJSONBool(v.Value)
	},
	cfTypeVoting: func(v favro.CardCustomFieldValue, _ favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return decodeJSONBool(v.Value)
	},
	cfTypeSingleSelect: func(v favro.CardCustomFieldValue, f favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return formatSelectValue(v.CustomFieldItemIDs, f.CustomFieldItems)
	},
	cfTypeMultipleSelect: func(v favro.CardCustomFieldValue, f favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return formatSelectValue(v.CustomFieldItemIDs, f.CustomFieldItems)
	},
	cfTypeStatus: func(v favro.CardCustomFieldValue, f favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return formatStatusValue(v.CustomFieldItemIDs, f.CustomFieldItems)
	},
	cfTypeMembers: func(v favro.CardCustomFieldValue, _ favro.CustomField, users []favro.User, _ []favro.Tag) (string, bool) {
		return formatMembersValue(v.Value, users)
	},
	cfTypeRating: func(v favro.CardCustomFieldValue, _ favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return formatRatingValue(v.Value, v.Total)
	},
	cfTypeTimeline: func(v favro.CardCustomFieldValue, _ favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return formatTimelineValue(v.Value)
	},
	cfTypeProgress: func(v favro.CardCustomFieldValue, _ favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return formatProgressValue(v.Value)
	},
	cfTypeTags: func(v favro.CardCustomFieldValue, _ favro.CustomField, _ []favro.User, tags []favro.Tag) (string, bool) {
		return formatTagsValue(v.Value, tags)
	},
	cfTypeSequentialID: func(v favro.CardCustomFieldValue, _ favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return formatSequentialIDValue(v.Value)
	},
	cfTypeRelations: func(v favro.CardCustomFieldValue, _ favro.CustomField, _ []favro.User, _ []favro.Tag) (string, bool) {
		return formatRelationsValue(v.Value)
	},
}

// formatCustomFieldValue projects a raw Favro custom-field value
// into a human-readable string. Returns ("", false) when no clear
// human-readable rendering exists for the kind or its raw value
// can't be decoded — the caller still receives the raw JSON via
// ResolvedCardCustomField.Raw.
//
// users and tags are the org-global lists the Members and Tags
// formatters dereference IDs against. Pass nil when neither kind
// is present on the card.
func formatCustomFieldValue(v favro.CardCustomFieldValue, f favro.CustomField, users []favro.User, tags []favro.Tag) (string, bool) {
	if fn, ok := customFieldFormatters[f.Type]; ok {
		return fn(v, f, users, tags)
	}
	return "", false
}

// decodeJSONString unmarshals raw as a JSON string.
func decodeJSONString(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	return "", false
}

// decodeJSONNumber unmarshals raw as a JSON number and returns it
// in the shortest float-or-int representation.
func decodeJSONNumber(raw json.RawMessage) (string, bool) {
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return strconv.FormatFloat(n, 'f', -1, 64), true
	}
	return "", false
}

// decodeJSONBool unmarshals raw as a JSON bool.
func decodeJSONBool(raw json.RawMessage) (string, bool) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return strconv.FormatBool(b), true
	}
	return "", false
}

// formatSelectValue dereferences one (single select) or many
// (multi select) CustomFieldItemIDs against the field's defined
// items, joining names with ", ". Returns ("", false) when the
// caller passed no item IDs (no value set on the card).
func formatSelectValue(itemIDs []string, items []favro.CustomFieldItem) (string, bool) {
	if len(itemIDs) == 0 {
		return "", false
	}
	byID := indexByID(items, func(it favro.CustomFieldItem) string { return it.CustomFieldItemID })
	names := make([]string, 0, len(itemIDs))
	for _, id := range itemIDs {
		if it, ok := byID[id]; ok {
			names = append(names, it.Name)
		}
	}
	if len(names) == 0 {
		return "", false
	}
	return strings.Join(names, ", "), true
}

// formatStatusValue renders a Status field's single CustomFieldItemID
// as "<name> (<color>)" — the per-item color is the disambiguator
// users actually see in the Favro UI. Falls back to plain name when
// the item has no color set.
func formatStatusValue(itemIDs []string, items []favro.CustomFieldItem) (string, bool) {
	if len(itemIDs) == 0 {
		return "", false
	}
	byID := indexByID(items, func(it favro.CustomFieldItem) string { return it.CustomFieldItemID })
	it, ok := byID[itemIDs[0]]
	if !ok {
		return "", false
	}
	if it.Color != "" {
		return fmt.Sprintf("%s (%s)", it.Name, it.Color), true
	}
	return it.Name, true
}

// formatMembersValue dereferences the JSON-array-of-userIds against
// the org-global user list, joining display names with ", ". Empty
// list reports the field as not-set (dereferenced=false).
func formatMembersValue(raw json.RawMessage, users []favro.User) (string, bool) {
	return formatIDListValue(raw, users,
		func(u favro.User) string { return u.UserID },
		func(u favro.User) string { return u.Name })
}

// formatIDListValue is the shared "JSON array of IDs → resolve
// against an org-global list → join names" projection. Underpins
// formatMembersValue (users) and formatTagsValue (tags) — same
// shape, only the index/name accessors differ. Returns ("", false)
// for empty arrays, decode failures, or all-IDs-unknown.
func formatIDListValue[T any](raw json.RawMessage, items []T, getID, getName func(T) string) (string, bool) {
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return "", false
	}
	if len(ids) == 0 {
		return "", false
	}
	byID := indexByID(items, getID)
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if it, ok := byID[id]; ok {
			names = append(names, getName(it))
		}
	}
	if len(names) == 0 {
		return "", false
	}
	return strings.Join(names, ", "), true
}

// formatRatingValue renders a Rating field as "<value> / <total>"
// when total is known, falling back to the bare number otherwise.
// Both parts use the same shortest-form float-or-int formatting as
// the Number kind so star integers don't render with trailing
// zeros.
func formatRatingValue(raw json.RawMessage, total float64) (string, bool) {
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false
	}
	value := strconv.FormatFloat(v, 'f', -1, 64)
	if total > 0 {
		return fmt.Sprintf("%s / %s", value, strconv.FormatFloat(total, 'f', -1, 64)), true
	}
	return value, true
}

// formatTimelineValue renders a Timeline field's {startDate, dueDate}
// pair as "<start> → <due>", or "due <due>" / "from <start>" when
// only one bound is set. Returns ("", false) when neither bound is
// present (Favro emits an empty object on a cleared timeline).
func formatTimelineValue(raw json.RawMessage) (string, bool) {
	var t struct {
		StartDate string `json:"startDate"`
		DueDate   string `json:"dueDate"`
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return "", false
	}
	switch {
	case t.StartDate != "" && t.DueDate != "":
		return fmt.Sprintf("%s → %s", t.StartDate, t.DueDate), true
	case t.DueDate != "":
		return "due " + t.DueDate, true
	case t.StartDate != "":
		return "from " + t.StartDate, true
	}
	return "", false
}

// formatProgressValue renders a Progress field's percentage with a
// trailing "%" — Favro stores progress as a 0–100 number.
func formatProgressValue(raw json.RawMessage) (string, bool) {
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return "", false
	}
	return strconv.FormatFloat(n, 'f', -1, 64) + "%", true
}

// formatTagsValue dereferences the JSON-array-of-tagIds against the
// org-global tag list. This is the custom-field "Tags" type,
// distinct from the top-level Card.Tags list dereferenced by
// dereferenceTags.
func formatTagsValue(raw json.RawMessage, tags []favro.Tag) (string, bool) {
	return formatIDListValue(raw, tags,
		func(t favro.Tag) string { return t.TagID },
		func(t favro.Tag) string { return t.Name })
}

// formatSequentialIDValue renders the per-card auto-counter value.
// Favro emits this as a JSON number (or sometimes a quoted string);
// the field's display prefix (e.g. "BSC-") is field-defined and
// not echoed in the per-card value. Callers who need the prefix
// chain to favro_get_custom_field on the field id.
func formatSequentialIDValue(raw json.RawMessage) (string, bool) {
	if s, ok := decodeJSONString(raw); ok && s != "" {
		return s, true
	}
	return decodeJSONNumber(raw)
}

// formatRelationsValue renders a Relations field as a count summary
// rather than dereferencing each related-card id to a name —
// per-relation card lookups would burn the rate-limit budget on
// what is typically context-rich field. The raw IDs remain
// available via ResolvedCardCustomField.Raw for callers who want them.
func formatRelationsValue(raw json.RawMessage) (string, bool) {
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return "", false
	}
	if len(ids) == 0 {
		return "", false
	}
	if len(ids) == 1 {
		return "1 related card", true
	}
	return fmt.Sprintf("%d related cards", len(ids)), true
}
