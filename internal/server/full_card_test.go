package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

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
	require.Len(t, got, 3)
	require.Equal(t, "frontend", got[0].Name)
	require.Equal(t, "blue", got[0].Color)
	require.Equal(t, "t-unknown", got[1].TagID)
	require.Empty(t, got[1].Name, "unknown tag id must round-trip with empty name")
	require.Equal(t, "backend", got[2].Name)
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
	require.Len(t, got, 2)
	require.Equal(t, "Alice", got[0].Name)
	require.Equal(t, "alice@example.invalid", got[0].Email)
	require.True(t, got[0].Completed)
	require.Equal(t, "u-unknown", got[1].UserID)
	require.Empty(t, got[1].Name)
}

func TestProjectCollectionNames(t *testing.T) {
	t.Parallel()

	collections := []favro.Collection{
		{CollectionID: "c-1", Name: "Docs"},
		{CollectionID: "c-2", Name: "Engineering"},
	}

	got := projectCollectionNames([]string{"c-1", "c-unknown", "c-2"}, collections)
	require.Equal(t, []string{"Docs", "Engineering"}, got, "unknown collection ids must be silently dropped")
}

func TestFindWidgetAndColumn(t *testing.T) {
	t.Parallel()

	widgets := []favro.Widget{
		{WidgetCommonID: "w-1", Name: "Sprint"},
		{WidgetCommonID: "w-2", Name: "Roadmap"},
	}
	require.Equal(t, "Sprint", findWidget(widgets, "w-1").Name)
	require.Nil(t, findWidget(widgets, "w-missing"))

	columns := []favro.Column{
		{ColumnID: "col-1", Name: "Doing"},
		{ColumnID: "col-2", Name: "Done"},
	}
	require.Equal(t, "Done", findColumnName(columns, "col-2"))
	require.Empty(t, findColumnName(columns, "col-missing"))
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
			name:   "link",
			field:  favro.CustomField{Type: "Link"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`"https://example.invalid"`)},
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
			name:   "rating with total",
			field:  favro.CustomField{Type: "Rating"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`3`), Total: 5},
			want:   "3 / 5",
			wantOK: true,
		},
		{
			name:   "rating without total falls back to bare value",
			field:  favro.CustomField{Type: "Rating"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`4`)},
			want:   "4",
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
			name:   "voting bool",
			field:  favro.CustomField{Type: "Voting"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`true`)},
			want:   "true",
			wantOK: true,
		},
		{
			name:   "progress",
			field:  favro.CustomField{Type: "Progress"},
			value:  favro.CardCustomFieldValue{Value: json.RawMessage(`75`)},
			want:   "75%",
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
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.want, got)
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
			require.ErrorIs(t, err, errFullCardIdentityRequired)
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
	require.NoError(t, err)

	require.Equal(t, "Print visitor passes", got.Name)
	require.Equal(t, "Sprint Board", got.WidgetName)
	require.Equal(t, "Done", got.ColumnName)
	require.Equal(t, []string{"Engineering", "Operations"}, got.CollectionNames)

	require.Len(t, got.ResolvedTags, 2)
	require.Equal(t, "frontend", got.ResolvedTags[0].Name)
	require.Empty(t, got.ResolvedTags[1].Name, "missing tag id must round-trip with empty name")

	require.Len(t, got.ResolvedAssignments, 1)
	require.Equal(t, "Alice", got.ResolvedAssignments[0].Name)
	require.True(t, got.ResolvedAssignments[0].Completed)

	require.Len(t, got.ResolvedCustomFields, 2)
	require.Equal(t, "Priority", got.ResolvedCustomFields[0].Name)
	require.Equal(t, "high priority", got.ResolvedCustomFields[0].DisplayValue)
	require.True(t, got.ResolvedCustomFields[0].Dereferenced)
	require.Equal(t, "Status", got.ResolvedCustomFields[1].Name)
	require.Equal(t, "Closed", got.ResolvedCustomFields[1].DisplayValue)
	require.True(t, got.ResolvedCustomFields[1].Dereferenced)

	require.Len(t, got.Comments, 1)
	require.Equal(t, "first comment", got.Comments[0].Body)
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
	require.NoError(t, err)
	require.Equal(t, "Visitor flow", got.Name)
	require.Equal(t, 42, got.SequentialID)
	require.Empty(t, got.Comments, "include_comments=false must skip the /comments call")
	require.EqualValues(t, 0, fix.calls["/comments"].Load())
}

func TestGetFullCard_NotFoundOnEmptyListResult(t *testing.T) {
	t.Parallel()

	// Fixture intentionally returns no cards — list-by-cardCommonID
	// finds nothing.
	fix := newFullCardFixture(t, fullCardFixtureOpts{})

	_, err := fix.resolver.GetFullCard(context.Background(), FullCardIdentity{CardCommonID: "cc-missing"}, false, 0)
	require.ErrorIs(t, err, errFullCardNotFound)
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
	require.NoError(t, err)
	require.Empty(t, got.Comments)
	require.EqualValues(t, 0, fix.calls["/comments"].Load(),
		"include_comments=false must short-circuit before any /comments call")
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
	require.NoError(t, err)
	require.Len(t, got.Comments, 2, "comment_limit must trim the page result")
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
	require.NoError(t, err)

	require.EqualValues(t, 1, fix.calls["/cards"].Load(), "exactly one /cards call (the GetCard fetch)")
	require.EqualValues(t, 0, fix.calls["/tags"].Load())
	require.EqualValues(t, 0, fix.calls["/users"].Load())
	require.EqualValues(t, 0, fix.calls["/widgets"].Load())
	require.EqualValues(t, 0, fix.calls["/columns"].Load())
	require.EqualValues(t, 0, fix.calls["/collections"].Load())
	require.EqualValues(t, 0, fix.calls["/customfields"].Load())
	require.EqualValues(t, 0, fix.calls["/comments"].Load())
}
