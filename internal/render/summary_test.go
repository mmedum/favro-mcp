package render

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

	require.Equal(t, "12 items · last page", Summary(summarizing{"12 items · last page"}))

	// An implementation that returns nothing falls through to the
	// outline rather than producing an empty content block — an empty
	// readable half is worse than a generic one.
	require.Equal(t, "(empty result)", Summary(summarizing{"   "}))
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

	require.Contains(t, got, "card_id: abc")
	require.Contains(t, got, "name: a name with spaces")
	require.Contains(t, got, "sequential_id: 42")
	require.Contains(t, got, "tag_ids: 3 items", "a collection is counted, not expanded")
	require.Contains(t, got, "custom_fields: 1 entry")
	require.Contains(t, got, "raw: 7 bytes", "a byte slice is bytes, not a collection")
	require.Contains(t, got, "created: 2026-09-13T10:00:00Z")

	// Zero values are omitted: a card sets eight of thirty fields, and
	// twenty-two lines of "false" is how the readable half stops being
	// readable.
	require.NotContains(t, got, "archived")
	require.NotContains(t, got, "description")
	// json:"-" and unexported fields are not ours to show.
	require.NotContains(t, got, "never rendered")
	require.NotContains(t, got, "nor this")
}

func TestSummaryClipsAndBounds(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("word ", 200)
	got := Summary(resource{ID: "x", Description: long})
	require.Contains(t, got, "…")
	for _, line := range strings.Split(got, "\n") {
		require.LessOrEqual(t, len(line), maxSummaryValue+40,
			"a description is not a summary of a card")
	}

	// Newlines in a value collapse, so one field stays one line.
	got = Summary(resource{ID: "x", Description: "first\nsecond\nthird"})
	require.Contains(t, got, "description: first second third")
	require.Len(t, strings.Split(got, "\n"), 2)
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
	require.Len(t, lines, maxSummaryLines+1, "the cap plus the line that says what it cut")
	require.Equal(t, "… 2 more fields", lines[len(lines)-1])
	require.Contains(t, got, "A: set", "an untagged field falls back to its Go name")
}

func TestSummaryNestingStopsAtDepth(t *testing.T) {
	t.Parallel()

	deep := resource{ID: "l0", Nested: &resource{ID: "l1", Nested: &resource{ID: "l2", Nested: &resource{ID: "l3"}}}}
	got := Summary(deep)

	require.Contains(t, got, "card_id: l0")
	require.Contains(t, got, "card_id: l1")
	require.Contains(t, got, "{…}", "nesting stops rather than recursing into a whole object graph")
	require.NotContains(t, got, "l3")
}

func TestSummaryHandlesNonStructs(t *testing.T) {
	t.Parallel()

	require.Equal(t, "(empty result)", Summary(nil))
	require.Equal(t, "(empty result)", Summary(struct{}{}))
	require.Equal(t, "(empty result)", Summary((*resource)(nil)))
	require.Equal(t, "3 items", Summary([]string{"a", "b", "c"}))
	require.Equal(t, "hello", Summary("hello"))
	require.Equal(t, "card_id: p", Summary(&resource{ID: "p"}), "a pointer renders as what it points at")
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

	require.Equal(t, `card_id=abc name="a name with spaces" sequential_id=42`, got)
	require.NotContains(t, got, "tag_ids", "collections have no place on a one-line entry")
}

func TestInlineBounds(t *testing.T) {
	t.Parallel()

	// Every scalar set, so the field cap is what decides the length.
	got := Inline(resource{
		ID: "a", Name: "b", Sequential: 1, Archived: true,
		Description: "d", Created: time.Now(),
	})
	require.Len(t, strings.Split(got, " "), maxInlineFields)

	require.Equal(t, "(no scalar fields)", Inline(resource{TagIDs: []string{"t"}}))
	require.Empty(t, Inline(nil))
	require.Equal(t, "7", Inline(7))
}
