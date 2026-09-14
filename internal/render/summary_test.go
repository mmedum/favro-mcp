package render

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// resource is shaped like the Favro wire types this renders: a few
// identifying scalars, a lot of fields that are usually unset, and a
// couple of collections whose contents are what structuredContent is
// for.
type resource struct {
	ID          string            `json:"card_id"`
	Name        string            `json:"name"`
	Sequential  int               `json:"sequential_id"`
	Archived    bool              `json:"archived"`
	Description string            `json:"description,omitempty"`
	TagIDs      []string          `json:"tag_ids,omitempty"`
	Fields      map[string]string `json:"custom_fields,omitempty"`
	Raw         json.RawMessage   `json:"raw,omitempty"`
	Created     time.Time         `json:"created,omitempty"`
	Hidden      string            `json:"-"`
	Nested      *resource         `json:"parent,omitempty"`
	unexported  string
}

type summarizing struct{ text string }

func (s summarizing) Summary() string { return s.text }

func TestSummaryPrefersSummarizer(t *testing.T) {
	t.Parallel()

	if got := Summary(summarizing{"12 items · last page"}); got != "12 items · last page" {
		t.Errorf("Summary(summarizing{\"12 items · last page\"}) = %v, want %v", got, "12 items · last page")
	}

	// An implementation that returns nothing falls through to the
	// outline rather than producing an empty content block — an empty
	// readable half is worse than a generic one.
	if got := Summary(summarizing{"   "}); got != "(empty result)" {
		t.Errorf("Summary(summarizing{\"   \"}) = %v, want %v", got, "(empty result)")
	}
}

func TestSummaryOutline(t *testing.T) {
	t.Parallel()

	got := Summary(resource{
		ID:         "abc",
		Name:       "a name with spaces",
		Sequential: 42,
		TagIDs:     []string{"t1", "t2", "t3"},
		Fields:     map[string]string{"a": "1"},
		Raw:        json.RawMessage(`{"x":1}`),
		Created:    time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC),
		Hidden:     "never rendered",
		unexported: "nor this",
	})

	if !strings.Contains(got, "card_id: abc") {
		t.Errorf("got does not contain %q", "card_id: abc")
	}
	if !strings.Contains(got, "name: a name with spaces") {
		t.Errorf("got does not contain %q", "name: a name with spaces")
	}
	if !strings.Contains(got, "sequential_id: 42") {
		t.Errorf("got does not contain %q", "sequential_id: 42")
	}
	if !strings.Contains(got, "tag_ids: 3 items") {
		t.Errorf("a collection is counted, not expanded: %q missing", "tag_ids: 3 items")
	}
	if !strings.Contains(got, "custom_fields: 1 entry") {
		t.Errorf("got does not contain %q", "custom_fields: 1 entry")
	}
	if !strings.Contains(got, "raw: 7 bytes") {
		t.Errorf("a byte slice is bytes, not a collection: %q missing", "raw: 7 bytes")
	}
	if !strings.Contains(got, "created: 2026-09-13T10:00:00Z") {
		t.Errorf("got does not contain %q", "created: 2026-09-13T10:00:00Z")
	}

	// Zero values are omitted: a card sets eight of thirty fields, and
	// twenty-two lines of "false" is how the readable half stops being
	// readable.
	if strings.Contains(got, "archived") {
		t.Errorf("got unexpectedly contains %q", "archived")
	}
	if strings.Contains(got, "description") {
		t.Errorf("got unexpectedly contains %q", "description")
	}
	// json:"-" and unexported fields are not ours to show.
	if strings.Contains(got, "never rendered") {
		t.Errorf("got unexpectedly contains %q", "never rendered")
	}
	if strings.Contains(got, "nor this") {
		t.Errorf("got unexpectedly contains %q", "nor this")
	}
}

func TestSummaryClipsAndBounds(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("word ", 200)
	got := Summary(resource{ID: "x", Description: long})
	if !strings.Contains(got, "…") {
		t.Errorf("got does not contain %q", "…")
	}
	for _, line := range strings.Split(got, "\n") {
		if len(line) > maxSummaryValue+40 {
			t.Errorf("a description is not a summary of a card: got %v, want at most %v", len(line), maxSummaryValue+40)
		}
	}

	// Newlines in a value collapse, so one field stays one line.
	got = Summary(resource{ID: "x", Description: "first\nsecond\nthird"})
	if !strings.Contains(got, "description: first second third") {
		t.Errorf("got does not contain %q", "description: first second third")
	}
	if len(strings.Split(got, "\n")) != 2 {
		t.Fatalf("len(strings.Split(got, \"\\n\")) = %d, want 2", len(strings.Split(got, "\n")))
	}
}

// wide has more fields than the line cap allows, and no json tags — so
// it covers both the truncation and the fall-back-to-the-Go-name path.
type wide struct {
	A, B, C, D, E, F, G, H, I, J, K, L string
	M, N, O, P, Q, R, S, T, U, V, W, X string
	Y, Z                               string
}

// TestSummaryCapsItsLength is the bound on what reaches the model: a
// resource with more fields than the cap gets a count of the rest
// rather than all of them.
func TestSummaryCapsItsLength(t *testing.T) {
	t.Parallel()

	var w wide
	rv := reflect.ValueOf(&w).Elem()
	for i := range rv.NumField() {
		rv.Field(i).SetString("set")
	}

	got := Summary(w)
	lines := strings.Split(got, "\n")
	if len(lines) != maxSummaryLines+1 {
		t.Fatalf("the cap plus the line that says what it cut: got %d", len(lines))
	}
	if got := lines[len(lines)-1]; got != "… 2 more fields" {
		t.Errorf("lines[len(lines)-1] = %v, want %v", got, "… 2 more fields")
	}
	if !strings.Contains(got, "A: set") {
		t.Errorf("an untagged field falls back to its Go name: %q missing", "A: set")
	}
}

func TestSummaryNestingStopsAtDepth(t *testing.T) {
	t.Parallel()

	deep := resource{ID: "l0", Nested: &resource{ID: "l1", Nested: &resource{ID: "l2", Nested: &resource{ID: "l3"}}}}
	got := Summary(deep)

	if !strings.Contains(got, "card_id: l0") {
		t.Errorf("got does not contain %q", "card_id: l0")
	}
	if !strings.Contains(got, "card_id: l1") {
		t.Errorf("got does not contain %q", "card_id: l1")
	}
	if !strings.Contains(got, "{…}") {
		t.Errorf("nesting stops rather than recursing into a whole object graph: %q missing", "{…}")
	}
	if strings.Contains(got, "l3") {
		t.Errorf("got unexpectedly contains %q", "l3")
	}
}

func TestSummaryHandlesNonStructs(t *testing.T) {
	t.Parallel()

	if got := Summary(nil); got != "(empty result)" {
		t.Errorf("Summary(nil) = %v, want %v", got, "(empty result)")
	}
	if got := Summary(struct{}{}); got != "(empty result)" {
		t.Errorf("Summary(struct{}{}) = %v, want %v", got, "(empty result)")
	}
	if got := Summary((*resource)(nil)); got != "(empty result)" {
		t.Errorf("Summary((*resource)(nil)) = %v, want %v", got, "(empty result)")
	}
	if got := Summary([]string{"a", "b", "c"}); got != "3 items" {
		t.Errorf("Summary([]string{\"a\", \"b\", \"c\"}) = %v, want %v", got, "3 items")
	}
	if got := Summary("hello"); got != "hello" {
		t.Errorf("Summary(\"hello\") = %v, want %v", got, "hello")
	}
	if got := Summary(&resource{ID: "p"}); got != "card_id: p" {
		t.Errorf("a pointer renders as what it points at: got %v, want %v", got, "card_id: p")
	}
}

func TestInline(t *testing.T) {
	t.Parallel()

	got := Inline(resource{
		ID:         "abc",
		Name:       "a name with spaces",
		Sequential: 42,
		TagIDs:     []string{"t1"},
		Fields:     map[string]string{"a": "1"},
	})

	if got := got; got != `card_id=abc name="a name with spaces" sequential_id=42` {
		t.Errorf("got = %v, want %v", got, `card_id=abc name="a name with spaces" sequential_id=42`)
	}
	if strings.Contains(got, "tag_ids") {
		t.Errorf("collections have no place on a one-line entry: %q present", "tag_ids")
	}
}

func TestInlineBounds(t *testing.T) {
	t.Parallel()

	// Every scalar set, so the field cap is what decides the length.
	got := Inline(resource{
		ID: "a", Name: "b", Sequential: 1, Archived: true,
		Description: "d", Created: time.Now(),
	})
	if len(strings.Split(got, " ")) != maxInlineFields {
		t.Fatalf("len(strings.Split(got, \" \")) = %d, want maxInlineFields", len(strings.Split(got, " ")))
	}

	if got := Inline(resource{TagIDs: []string{"t"}}); got != "(no scalar fields)" {
		t.Errorf("Inline(resource{TagIDs: []string{\"t\"}}) = %v, want %v", got, "(no scalar fields)")
	}
	if len(Inline(nil)) != 0 {
		t.Errorf("Inline(nil) = %v, want empty", Inline(nil))
	}
	if got := Inline(7); got != "7" {
		t.Errorf("Inline(7) = %v, want %v", got, "7")
	}
}
