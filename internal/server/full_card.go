package server

import (
	"context"
	"encoding/json"
	"errors"
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
// human-readable string for the formatted types (Text, Number,
// Date, Date created, Checkbox, Link, Single select, Multiple
// select). For the long-tail types (Members, Status, Rating,
// Timeline, Voting, Progress, Relations, Sequential ID, Tags) it
// is empty and Dereferenced is false. Raw always carries the
// original Favro value so callers can fall back when they need to.
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
	full.ResolvedCustomFields = projectCustomFieldValues(full.CustomFieldsValues, fields)
	return nil
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
		columns, _, err := r.listColumnsForWidget(ctx, full.WidgetCommonID, false)
		if err != nil {
			return err
		}
		full.ColumnName = findColumnName(columns, full.ColumnID)
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

func projectCustomFieldValues(values []favro.CardCustomFieldValue, fields []favro.CustomField) []ResolvedCardCustomField {
	byID := indexByID(fields, func(f favro.CustomField) string { return f.CustomFieldID })
	out := make([]ResolvedCardCustomField, 0, len(values))
	for _, v := range values {
		rc := ResolvedCardCustomField{CustomFieldID: v.CustomFieldID, Raw: v.Value}
		if f, ok := byID[v.CustomFieldID]; ok {
			rc.Name = f.Name
			rc.Type = f.Type
			rc.DisplayValue, rc.Dereferenced = formatCustomFieldValue(v, f)
		}
		out = append(out, rc)
	}
	return out
}

// formatCustomFieldValue projects a raw Favro custom-field value
// into a human-readable string. Returns ("", false) for the
// long-tail types this layer does not yet dereference (Members,
// Status, Rating, Timeline, Voting, Progress, Relations, Sequential
// ID, Tags). The caller still gets the raw JSON via
// ResolvedCardCustomField.Raw.
func formatCustomFieldValue(v favro.CardCustomFieldValue, f favro.CustomField) (string, bool) {
	switch f.Type {
	case "Text", "Link", "Date", "Date created":
		return decodeJSONString(v.Value)
	case "Number":
		return decodeJSONNumber(v.Value)
	case "Checkbox":
		return decodeJSONBool(v.Value)
	case "Single select", "Multiple select":
		return formatSelectValue(v.CustomFieldItemIDs, f.CustomFieldItems)
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
