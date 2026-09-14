package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// ============================================================
// Pure projection helpers
// ============================================================

func TestProjectCardTags(t *testing.T) {
	t.Parallel()

	tags := []favro.Tag{
		{TagID: "t-1", Name: "frontend", Color: "blue"},
		{TagID: "t-2", Name: "backend"},
	}

	got := projectCardTags([]string{"t-1", "t-unknown", "t-2"}, tags)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	if got := got[0].Name; got != "frontend" {
		t.Errorf("got[0].Name = %v, want %v", got, "frontend")
	}
	if got := got[0].Color; got != "blue" {
		t.Errorf("got[0].Color = %v, want %v", got, "blue")
	}
	if got := got[1].TagID; got != "t-unknown" {
		t.Errorf("got[1].TagID = %v, want %v", got, "t-unknown")
	}
	if len(got[1].Name) != 0 {
		t.Errorf("unknown tag id must round-trip with empty name: got %v", got[1].Name)
	}
	if got := got[2].Name; got != "backend" {
		t.Errorf("got[2].Name = %v, want %v", got, "backend")
	}
}

func TestProjectCardAssignments(t *testing.T) {
	t.Parallel()

	users := []favro.User{
		{UserID: "u-1", Name: "Alice", Email: "alice@example.invalid"},
		{UserID: "u-2", Name: "Bob"},
	}
	assignments := []favro.CardAssignment{
		{UserID: "u-1", Completed: true},
		{UserID: "u-unknown"},
	}

	got := projectCardAssignments(assignments, users)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got := got[0].Name; got != "Alice" {
		t.Errorf("got[0].Name = %v, want %v", got, "Alice")
	}
	if got := got[0].Email; got != "alice@example.invalid" {
		t.Errorf("got[0].Email = %v, want %v", got, "alice@example.invalid")
	}
	if !got[0].Completed {
		t.Error("got[0].Completed = false, want true")
	}
	if got := got[1].UserID; got != "u-unknown" {
		t.Errorf("got[1].UserID = %v, want %v", got, "u-unknown")
	}
	if len(got[1].Name) != 0 {
		t.Errorf("got[1].Name = %v, want empty", got[1].Name)
	}
}

func TestProjectCollectionNames(t *testing.T) {
	t.Parallel()

	collections := []favro.Collection{
		{CollectionID: "c-1", Name: "Docs"},
		{CollectionID: "c-2", Name: "Engineering"},
	}

	got := projectCollectionNames([]string{"c-1", "c-unknown", "c-2"}, collections)
	if got := got; !reflect.DeepEqual(got, ([]string{"Docs", "Engineering"})) {
		t.Errorf("unknown collection ids must be silently dropped: got %v, want %v", got, []string{"Docs", "Engineering"})
	}
}

func TestFindWidgetAndColumn(t *testing.T) {
	t.Parallel()

	widgets := []favro.Widget{
		{WidgetCommonID: "w-1", Name: "Sprint"},
		{WidgetCommonID: "w-2", Name: "Roadmap"},
	}
	if got := findWidget(widgets, "w-1").Name; got != "Sprint" {
		t.Errorf("findWidget(widgets, \"w-1\").Name = %v, want %v", got, "Sprint")
	}
	if findWidget(widgets, "w-missing") != nil {
		t.Errorf("findWidget(widgets, \"w-missing\") = %v, want nil", findWidget(widgets, "w-missing"))
	}

	columns := []favro.Column{
		{ColumnID: "col-1", Name: "Doing"},
		{ColumnID: "col-2", Name: "Done"},
	}
	if got := findColumnName(columns, "col-2"); got != "Done" {
		t.Errorf("findColumnName(columns, \"col-2\") = %v, want %v", got, "Done")
	}
	if len(findColumnName(columns, "col-missing")) != 0 {
		t.Errorf("findColumnName(columns, \"col-missing\") = %v, want empty", findColumnName(columns, "col-missing"))
	}
}

// ============================================================
// formatCustomFieldValue — type-by-type contract.
// ============================================================

func TestFormatCustomFieldValue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		field  favro.CustomField
		value  favro.CardCustomFieldValue
		users  []favro.User
		tags   []favro.Tag
		want   string
		wantOK bool
	}{
		{
			name:   "text",
			field:  favro.CustomField{Type: "Text"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`"hello"`)},
			want:   "hello",
			wantOK: true,
		},
		{
			name:   "link legacy string value",
			field:  favro.CustomField{Type: "Link"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`"https://example.invalid"`)},
			want:   "https://example.invalid",
			wantOK: true,
		},
		{
			name:   "link object with display text",
			field:  favro.CustomField{Type: "Link"},
			value:  favro.CardCustomFieldValue{Link: json.RawMessage(`{"url":"https://example.invalid","text":"docs"}`)},
			want:   "docs (https://example.invalid)",
			wantOK: true,
		},
		{
			name:   "link object without display text",
			field:  favro.CustomField{Type: "Link"},
			value:  favro.CardCustomFieldValue{Link: json.RawMessage(`{"url":"https://example.invalid"}`)},
			want:   "https://example.invalid",
			wantOK: true,
		},
		{
			name:   "number int",
			field:  favro.CustomField{Type: "Number"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`42`)},
			want:   "42",
			wantOK: true,
		},
		{
			name:   "number float",
			field:  favro.CustomField{Type: "Number"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`3.14`)},
			want:   "3.14",
			wantOK: true,
		},
		{
			// Documented shape: Number travels in `total`.
			name:   "number from total",
			field:  favro.CustomField{Type: "Number"},
			value:  favro.CardCustomFieldValue{Total: cfFloat(8)},
			want:   "8",
			wantOK: true,
		},
		{
			name:   "number zero in total is not treated as unset",
			field:  favro.CustomField{Type: "Number"},
			value:  favro.CardCustomFieldValue{Total: cfFloat(0)},
			want:   "0",
			wantOK: true,
		},
		{
			name:   "date",
			field:  favro.CustomField{Type: "Date"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`"2026-05-04T00:00:00Z"`)},
			want:   "2026-05-04T00:00:00Z",
			wantOK: true,
		},
		{
			name:   "date created",
			field:  favro.CustomField{Type: "Date created"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`"2026-05-04T12:00:00Z"`)},
			want:   "2026-05-04T12:00:00Z",
			wantOK: true,
		},
		{
			name:   "checkbox true",
			field:  favro.CustomField{Type: "Checkbox"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`true`)},
			want:   "true",
			wantOK: true,
		},
		{
			name:   "checkbox false",
			field:  favro.CustomField{Type: "Checkbox"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`false`)},
			want:   "false",
			wantOK: true,
		},
		{
			name: "single select known",
			field: favro.CustomField{
				Type: "Single select",
				CustomFieldItems: []favro.CustomFieldItem{
					{CustomFieldItemID: "item-1", Name: "Low"},
					{CustomFieldItemID: "item-2", Name: "High"},
				},
			},
			value:  favro.CardCustomFieldValue{CustomFieldItemIDs: []string{"item-2"}},
			want:   "High",
			wantOK: true,
		},
		{
			name: "single select unknown id",
			field: favro.CustomField{
				Type:             "Single select",
				CustomFieldItems: []favro.CustomFieldItem{{CustomFieldItemID: "item-1", Name: "Low"}},
			},
			value:  favro.CardCustomFieldValue{CustomFieldItemIDs: []string{"item-missing"}},
			want:   "",
			wantOK: false,
		},
		{
			name: "multi select",
			field: favro.CustomField{
				Type: "Multiple select",
				CustomFieldItems: []favro.CustomFieldItem{
					{CustomFieldItemID: "item-1", Name: "Red"},
					{CustomFieldItemID: "item-2", Name: "Green"},
					{CustomFieldItemID: "item-3", Name: "Blue"},
				},
			},
			value:  favro.CardCustomFieldValue{CustomFieldItemIDs: []string{"item-1", "item-3"}},
			want:   "Red, Blue",
			wantOK: true,
		},
		{
			name:   "members dereferenced via user list",
			field:  favro.CustomField{Type: "Members"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`["u-1","u-2"]`)},
			users:  []favro.User{{UserID: "u-1", Name: "Alice"}, {UserID: "u-2", Name: "Bob"}, {UserID: "u-3", Name: "Carol"}},
			want:   "Alice, Bob",
			wantOK: true,
		},
		{
			name:   "members empty array reports unset",
			field:  favro.CustomField{Type: "Members"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`[]`)},
			users:  []favro.User{{UserID: "u-1", Name: "Alice"}},
			want:   "",
			wantOK: false,
		},
		{
			name:   "members partial unknown id falls through known names",
			field:  favro.CustomField{Type: "Members"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`["u-1","u-missing"]`)},
			users:  []favro.User{{UserID: "u-1", Name: "Alice"}},
			want:   "Alice",
			wantOK: true,
		},
		{
			name: "status with color",
			field: favro.CustomField{
				Type: "Status",
				CustomFieldItems: []favro.CustomFieldItem{
					{CustomFieldItemID: "st-1", Name: "Doing", Color: "blue"},
				},
			},
			value:  favro.CardCustomFieldValue{CustomFieldItemIDs: []string{"st-1"}},
			want:   "Doing (blue)",
			wantOK: true,
		},
		{
			name: "status from documented value array",
			field: favro.CustomField{
				Type: "Status",
				CustomFieldItems: []favro.CustomFieldItem{
					{CustomFieldItemID: "st-1", Name: "Doing", Color: "blue"},
				},
			},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`["st-1"]`)},
			want:   "Doing (blue)",
			wantOK: true,
		},
		{
			name: "status without color falls back to plain name",
			field: favro.CustomField{
				Type: "Status",
				CustomFieldItems: []favro.CustomFieldItem{
					{CustomFieldItemID: "st-1", Name: "Doing"},
				},
			},
			value:  favro.CardCustomFieldValue{CustomFieldItemIDs: []string{"st-1"}},
			want:   "Doing",
			wantOK: true,
		},
		{
			// Documented shape: the rating lives in `total`, and the
			// scale is fixed at 0-5.
			name:   "rating from total",
			field:  favro.CustomField{Type: "Rating"},
			value:  favro.CardCustomFieldValue{Total: cfFloat(3)},
			want:   "3 / 5",
			wantOK: true,
		},
		{
			// Legacy shape: value carries the rating and total the max.
			name:   "rating split across value and total",
			field:  favro.CustomField{Type: "Rating"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`3`), Total: cfFloat(7)},
			want:   "3 / 7",
			wantOK: true,
		},
		{
			name:   "rating from bare value assumes the documented 0-5 scale",
			field:  favro.CustomField{Type: "Rating"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`4`)},
			want:   "4 / 5",
			wantOK: true,
		},
		{
			name:   "timeline both bounds",
			field:  favro.CustomField{Type: "Timeline"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`{"startDate":"2026-01-01T00:00:00Z","dueDate":"2026-02-01T00:00:00Z"}`)},
			want:   "2026-01-01T00:00:00Z → 2026-02-01T00:00:00Z",
			wantOK: true,
		},
		{
			name:   "timeline due only",
			field:  favro.CustomField{Type: "Timeline"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`{"dueDate":"2026-02-01T00:00:00Z"}`)},
			want:   "due 2026-02-01T00:00:00Z",
			wantOK: true,
		},
		{
			name:   "timeline start only",
			field:  favro.CustomField{Type: "Timeline"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`{"startDate":"2026-01-01T00:00:00Z"}`)},
			want:   "from 2026-01-01T00:00:00Z",
			wantOK: true,
		},
		{
			name:   "timeline empty object reports unset",
			field:  favro.CustomField{Type: "Timeline"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`{}`)},
			want:   "",
			wantOK: false,
		},
		{
			name:   "voting legacy bool",
			field:  favro.CustomField{Type: "Voting"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`true`)},
			want:   "true",
			wantOK: true,
		},
		{
			// Documented shape: the value is the array of userIds that
			// voted, so known voters resolve to names.
			name:   "vote resolves voter names",
			field:  favro.CustomField{Type: "Vote"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`["u-1","u-2"]`)},
			users:  []favro.User{{UserID: "u-1", Name: "Alice"}, {UserID: "u-2", Name: "Bob"}},
			want:   "Alice, Bob",
			wantOK: true,
		},
		{
			name:   "vote falls back to a count when voters are unknown",
			field:  favro.CustomField{Type: "Vote"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`["u-1","u-2"]`)},
			want:   "2 votes",
			wantOK: true,
		},
		{
			name:   "timeline from documented sibling object",
			field:  favro.CustomField{Type: "Timeline"},
			value:  favro.CardCustomFieldValue{Timeline: json.RawMessage(`{"startDate":"2026-01-01T00:00:00Z","dueDate":"2026-02-01T00:00:00Z"}`)},
			want:   "2026-01-01T00:00:00Z → 2026-02-01T00:00:00Z",
			wantOK: true,
		},
		{
			name:   "progress legacy bare number",
			field:  favro.CustomField{Type: "Progress"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`75`)},
			want:   "75%",
			wantOK: true,
		},
		{
			name:   "progress from documented percentage object",
			field:  favro.CustomField{Type: "Progress"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`{"percentage":40}`)},
			want:   "40%",
			wantOK: true,
		},
		{
			name:   "time renders summed milliseconds",
			field:  favro.CustomField{Type: "Time"},
			value:  favro.CardCustomFieldValue{Total: cfFloat(50400000)},
			want:   "14h 0m",
			wantOK: true,
		},
		{
			name:   "color",
			field:  favro.CustomField{Type: "Color"},
			value:  favro.CardCustomFieldValue{Color: "blue-300"},
			want:   "blue-300",
			wantOK: true,
		},
		{
			name:   "tags dereferenced via tag list",
			field:  favro.CustomField{Type: "Tags"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`["t-1","t-2"]`)},
			tags:   []favro.Tag{{TagID: "t-1", Name: "frontend"}, {TagID: "t-2", Name: "blocker"}},
			want:   "frontend, blocker",
			wantOK: true,
		},
		{
			name:   "sequential id number",
			field:  favro.CustomField{Type: "Sequential ID"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`123`)},
			want:   "123",
			wantOK: true,
		},
		{
			name:   "sequential id string",
			field:  favro.CustomField{Type: "Sequential ID"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`"BSC-42"`)},
			want:   "BSC-42",
			wantOK: true,
		},
		{
			name:   "relations count single",
			field:  favro.CustomField{Type: "Relations"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`["cc-1"]`)},
			want:   "1 related card",
			wantOK: true,
		},
		{
			name:   "relations count plural",
			field:  favro.CustomField{Type: "Relations"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`["cc-1","cc-2","cc-3"]`)},
			want:   "3 related cards",
			wantOK: true,
		},
		{
			name:   "unknown type",
			field:  favro.CustomField{Type: "Some Unknown Future Type"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`"x"`)},
			want:   "",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := formatCustomFieldValue(tc.value, tc.field, tc.users, tc.tags)
			if got := ok; got != tc.wantOK {
				t.Errorf("ok = %v, want %v", got, tc.wantOK)
			}
			if got := got; got != tc.want {
				t.Errorf("got = %v, want %v", got, tc.want)
			}
		})
	}
}

// ============================================================
// GetFullCard composition — end-to-end against a fixture server
// that responds for every Favro endpoint the resolver caches consult.
// ============================================================

// fullCardFixture wires a Resolver to an httptest server that
// responds for /cards, /widgets, /columns, /collections, /tags,
// /users, /customfields, /comments. Returns the resolver and a
// per-path call counter so tests can assert "only the cards we
// expected the call to be made".
type fullCardFixture struct {
	resolver *Resolver
	calls    map[string]*atomic.Int32
}

// fullCardFixtureOpts customises the fixture per test. Anything
// left zero-value uses an empty list response (so listAllX returns
// nothing without erroring).
type fullCardFixtureOpts struct {
	cards        []favro.Card
	widgets      []favro.Widget
	columns      []favro.Column
	collections  []favro.Collection
	tags         []favro.Tag
	users        []favro.User
	customFields []favro.CustomField
	comments     []favro.Comment
}

func newFullCardFixture(t *testing.T, opts fullCardFixtureOpts) *fullCardFixture {
	t.Helper()

	calls := map[string]*atomic.Int32{
		"/cards":        new(atomic.Int32),
		"/widgets":      new(atomic.Int32),
		"/columns":      new(atomic.Int32),
		"/collections":  new(atomic.Int32),
		"/tags":         new(atomic.Int32),
		"/users":        new(atomic.Int32),
		"/customfields": new(atomic.Int32),
		"/comments":     new(atomic.Int32),
	}

	encode := func(w http.ResponseWriter, page0 any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(page0)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/cards/"):
			calls["/cards"].Add(1)
			id := strings.TrimPrefix(r.URL.Path, "/cards/")
			for _, c := range opts.cards {
				if c.CardID == id {
					_ = json.NewEncoder(w).Encode(c)
					return
				}
			}
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
		case r.URL.Path == "/cards":
			calls["/cards"].Add(1)
			encode(w, favro.PageEnvelope[favro.Card]{Pages: 1, Entities: opts.cards})
		case r.URL.Path == "/widgets":
			calls["/widgets"].Add(1)
			encode(w, favro.PageEnvelope[favro.Widget]{Pages: 1, Entities: opts.widgets})
		case r.URL.Path == "/columns":
			calls["/columns"].Add(1)
			encode(w, favro.PageEnvelope[favro.Column]{Pages: 1, Entities: opts.columns})
		case r.URL.Path == "/collections":
			calls["/collections"].Add(1)
			encode(w, favro.PageEnvelope[favro.Collection]{Pages: 1, Entities: opts.collections})
		case r.URL.Path == "/tags":
			calls["/tags"].Add(1)
			encode(w, favro.PageEnvelope[favro.Tag]{Pages: 1, Entities: opts.tags})
		case r.URL.Path == "/users":
			calls["/users"].Add(1)
			encode(w, favro.PageEnvelope[favro.User]{Pages: 1, Entities: opts.users})
		case r.URL.Path == "/customfields":
			calls["/customfields"].Add(1)
			encode(w, favro.PageEnvelope[favro.CustomField]{Pages: 1, Entities: opts.customFields})
		case r.URL.Path == "/comments":
			calls["/comments"].Add(1)
			encode(w, favro.PageEnvelope[favro.Comment]{Pages: 1, Entities: opts.comments})
		default:
			t.Errorf("unexpected fixture path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})

	c := favroFixture(t, handler)
	return &fullCardFixture{resolver: NewResolver(c), calls: calls}
}

func TestGetFullCard_IdentityValidation(t *testing.T) {
	t.Parallel()

	fix := newFullCardFixture(t, fullCardFixtureOpts{})

	cases := []struct {
		name string
		id   FullCardIdentity
	}{
		{"none set", FullCardIdentity{}},
		{"card_id and card_common_id", FullCardIdentity{CardID: "x", CardCommonID: "y"}},
		{"all three", FullCardIdentity{CardID: "x", CardCommonID: "y", SequentialID: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := fix.resolver.GetFullCard(context.Background(), tc.id, false, 0)
			if !errors.Is(err, errFullCardIdentityRequired) {
				t.Fatalf("got %v, want errFullCardIdentityRequired", err)
			}
		})
	}
}

func TestGetFullCard_HappyPath_ByCardID(t *testing.T) {
	t.Parallel()

	fix := newFullCardFixture(t, fullCardFixtureOpts{
		cards: []favro.Card{{
			CardID:         "c-1",
			CardCommonID:   "cc-1",
			Name:           "Print visitor passes",
			WidgetCommonID: "w-1",
			ColumnID:       "col-2",
			Tags:           []string{"tag-1", "tag-missing"},
			Assignments: []favro.CardAssignment{
				{UserID: "u-1", Completed: true},
			},
			CustomFieldsValues: []favro.CardCustomFieldValue{
				{CustomFieldID: "cf-text", Value: json.RawMessage(`"high priority"`)},
				{CustomFieldID: "cf-select", CustomFieldItemIDs: []string{"item-2"}},
			},
		}},
		widgets: []favro.Widget{
			{WidgetCommonID: "w-1", Name: "Sprint Board", CollectionIDs: []string{"col-A", "col-B"}},
		},
		columns: []favro.Column{
			{ColumnID: "col-1", WidgetCommonID: "w-1", Name: "Doing"},
			{ColumnID: "col-2", WidgetCommonID: "w-1", Name: "Done"},
		},
		collections: []favro.Collection{
			{CollectionID: "col-A", Name: "Engineering"},
			{CollectionID: "col-B", Name: "Operations"},
		},
		tags: []favro.Tag{
			{TagID: "tag-1", Name: "frontend", Color: "blue"},
		},
		users: []favro.User{
			{UserID: "u-1", Name: "Alice", Email: "alice@example.invalid"},
		},
		customFields: []favro.CustomField{
			{CustomFieldID: "cf-text", Name: "Priority", Type: "Text"},
			{
				CustomFieldID: "cf-select", Name: "Status", Type: "Single select",
				CustomFieldItems: []favro.CustomFieldItem{
					{CustomFieldItemID: "item-1", Name: "Open"},
					{CustomFieldItemID: "item-2", Name: "Closed"},
				},
			},
		},
		comments: []favro.Comment{
			{CommentID: "cmt-1", CardCommonID: "cc-1", UserID: "u-1", Body: "first comment"},
		},
	})

	got, err := fix.resolver.GetFullCard(context.Background(), FullCardIdentity{CardID: "c-1"}, true, 0)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	if got := got.Name; got != "Print visitor passes" {
		t.Errorf("got.Name = %v, want %v", got, "Print visitor passes")
	}
	if got := got.WidgetName; got != "Sprint Board" {
		t.Errorf("got.WidgetName = %v, want %v", got, "Sprint Board")
	}
	if got := got.ColumnName; got != "Done" {
		t.Errorf("got.ColumnName = %v, want %v", got, "Done")
	}
	if got := got.CollectionNames; !reflect.DeepEqual(got, ([]string{"Engineering", "Operations"})) {
		t.Errorf("got.CollectionNames = %v, want %v", got, []string{"Engineering", "Operations"})
	}

	if len(got.ResolvedTags) != 2 {
		t.Fatalf("len(got.ResolvedTags) = %d, want 2", len(got.ResolvedTags))
	}
	if got := got.ResolvedTags[0].Name; got != "frontend" {
		t.Errorf("got.ResolvedTags[0].Name = %v, want %v", got, "frontend")
	}
	if len(got.ResolvedTags[1].Name) != 0 {
		t.Errorf("missing tag id must round-trip with empty name: got %v", got.ResolvedTags[1].Name)
	}

	if len(got.ResolvedAssignments) != 1 {
		t.Fatalf("len(got.ResolvedAssignments) = %d, want 1", len(got.ResolvedAssignments))
	}
	if got := got.ResolvedAssignments[0].Name; got != "Alice" {
		t.Errorf("got.ResolvedAssignments[0].Name = %v, want %v", got, "Alice")
	}
	if !got.ResolvedAssignments[0].Completed {
		t.Error("got.ResolvedAssignments[0].Completed = false, want true")
	}

	if len(got.ResolvedCustomFields) != 2 {
		t.Fatalf("len(got.ResolvedCustomFields) = %d, want 2", len(got.ResolvedCustomFields))
	}
	if got := got.ResolvedCustomFields[0].Name; got != "Priority" {
		t.Errorf("got.ResolvedCustomFields[0].Name = %v, want %v", got, "Priority")
	}
	if got := got.ResolvedCustomFields[0].DisplayValue; got != "high priority" {
		t.Errorf("got.ResolvedCustomFields[0].DisplayValue = %v, want %v", got, "high priority")
	}
	if !got.ResolvedCustomFields[0].Dereferenced {
		t.Error("got.ResolvedCustomFields[0].Dereferenced = false, want true")
	}
	if got := got.ResolvedCustomFields[1].Name; got != "Status" {
		t.Errorf("got.ResolvedCustomFields[1].Name = %v, want %v", got, "Status")
	}
	if got := got.ResolvedCustomFields[1].DisplayValue; got != "Closed" {
		t.Errorf("got.ResolvedCustomFields[1].DisplayValue = %v, want %v", got, "Closed")
	}
	if !got.ResolvedCustomFields[1].Dereferenced {
		t.Error("got.ResolvedCustomFields[1].Dereferenced = false, want true")
	}

	if len(got.Comments) != 1 {
		t.Fatalf("len(got.Comments) = %d, want 1", len(got.Comments))
	}
	if got := got.Comments[0].Body; got != "first comment" {
		t.Errorf("got.Comments[0].Body = %v, want %v", got, "first comment")
	}
}

func TestGetFullCard_HappyPath_BySequentialID(t *testing.T) {
	t.Parallel()

	fix := newFullCardFixture(t, fullCardFixtureOpts{
		cards: []favro.Card{{
			CardID:             "c-1",
			CardCommonID:       "cc-1",
			Name:               "Visitor flow",
			SequentialID:       42,
			SequentialIDPrefix: "VP",
		}},
	})

	got, err := fix.resolver.GetFullCard(context.Background(), FullCardIdentity{SequentialID: 42}, false, 0)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.Name; got != "Visitor flow" {
		t.Errorf("got.Name = %v, want %v", got, "Visitor flow")
	}
	if got := got.SequentialID; got != 42 {
		t.Errorf("got.SequentialID = %v, want %v", got, 42)
	}
	if len(got.Comments) != 0 {
		t.Errorf("include_comments=false must skip the /comments call: got %v", got.Comments)
	}
	if got := fix.calls["/comments"].Load(); got != 0 {
		t.Errorf("fix.calls[\"/comments\"].Load() = %v, want %v", got, 0)
	}
}

func TestGetFullCard_NotFoundOnEmptyListResult(t *testing.T) {
	t.Parallel()

	// Fixture intentionally returns no cards — list-by-cardCommonID
	// finds nothing.
	fix := newFullCardFixture(t, fullCardFixtureOpts{})

	_, err := fix.resolver.GetFullCard(context.Background(), FullCardIdentity{CardCommonID: "cc-missing"}, false, 0)
	if !errors.Is(err, errFullCardNotFound) {
		t.Fatalf("got %v, want errFullCardNotFound", err)
	}
}

func TestGetFullCard_ExcludesCommentsByDefault(t *testing.T) {
	t.Parallel()

	fix := newFullCardFixture(t, fullCardFixtureOpts{
		cards: []favro.Card{{CardID: "c-1", CardCommonID: "cc-1", Name: "x"}},
		comments: []favro.Comment{
			{CommentID: "cmt-1", CardCommonID: "cc-1", UserID: "u-1", Body: "should not appear"},
		},
	})

	got, err := fix.resolver.GetFullCard(context.Background(), FullCardIdentity{CardID: "c-1"}, false, 0)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got.Comments) != 0 {
		t.Errorf("got.Comments = %v, want empty", got.Comments)
	}
	if got := fix.calls["/comments"].Load(); got != 0 {
		t.Errorf("include_comments=false must short-circuit before any /comments call: got %v, want %v", got, 0)
	}
}

func TestGetFullCard_CommentLimitTrimsResult(t *testing.T) {
	t.Parallel()

	cards := []favro.Card{{CardID: "c-1", CardCommonID: "cc-1", Name: "x"}}
	comments := make([]favro.Comment, 5)
	for i := range comments {
		comments[i] = favro.Comment{CommentID: "cmt", CardCommonID: "cc-1", UserID: "u-1", Body: "msg"}
	}
	fix := newFullCardFixture(t, fullCardFixtureOpts{cards: cards, comments: comments})

	got, err := fix.resolver.GetFullCard(context.Background(), FullCardIdentity{CardID: "c-1"}, true, 2)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got.Comments) != 2 {
		t.Fatalf("comment_limit must trim the page result: got %d", len(got.Comments))
	}
}

func TestGetFullCard_SkipsResolversWhenCardHasNoIDs(t *testing.T) {
	t.Parallel()

	// Card has no tags / assignments / customFieldsValues / widgetCommonId,
	// so the resolver MUST NOT touch /tags, /users, /widgets, /columns,
	// /collections, /customfields. Only the GetCard call should fire.
	fix := newFullCardFixture(t, fullCardFixtureOpts{
		cards: []favro.Card{{CardID: "c-1", CardCommonID: "cc-1", Name: "minimal"}},
	})

	_, err := fix.resolver.GetFullCard(context.Background(), FullCardIdentity{CardID: "c-1"}, false, 0)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	if got := fix.calls["/cards"].Load(); got != 1 {
		t.Errorf("exactly one /cards call (the GetCard fetch): got %v, want %v", got, 1)
	}
	if got := fix.calls["/tags"].Load(); got != 0 {
		t.Errorf("fix.calls[\"/tags\"].Load() = %v, want %v", got, 0)
	}
	if got := fix.calls["/users"].Load(); got != 0 {
		t.Errorf("fix.calls[\"/users\"].Load() = %v, want %v", got, 0)
	}
	if got := fix.calls["/widgets"].Load(); got != 0 {
		t.Errorf("fix.calls[\"/widgets\"].Load() = %v, want %v", got, 0)
	}
	if got := fix.calls["/columns"].Load(); got != 0 {
		t.Errorf("fix.calls[\"/columns\"].Load() = %v, want %v", got, 0)
	}
	if got := fix.calls["/collections"].Load(); got != 0 {
		t.Errorf("fix.calls[\"/collections\"].Load() = %v, want %v", got, 0)
	}
	if got := fix.calls["/customfields"].Load(); got != 0 {
		t.Errorf("fix.calls[\"/customfields\"].Load() = %v, want %v", got, 0)
	}
	if got := fix.calls["/comments"].Load(); got != 0 {
		t.Errorf("fix.calls[\"/comments\"].Load() = %v, want %v", got, 0)
	}
}

// cfFloat returns a pointer to n. CardCustomFieldValue.Total is a
// pointer so an explicit 0 stays distinguishable from an unset field.
func cfFloat(n float64) *float64 { return &n }
