package render

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Summarizer is implemented by an output type that knows how to say
// what it holds. The shared output shapes in internal/tools implement
// it, which covers the list, write and resolve tools — the three
// families where a generic field dump would bury the one thing the
// caller needs (the next page, the dry-run verdict, the candidates).
type Summarizer interface {
	Summary() string
}

// Limits on the generated text. `content` earns its place by being
// shorter than `structuredContent`, not by being the same information
// with different punctuation, so the outline is a digest: top-level
// fields, one line each, collections counted rather than expanded.
const (
	maxSummaryLines  = 24
	maxSummaryValue  = 120
	maxSummaryDepth  = 2
	summaryIndentStr = "  "
	// maxSummaryItems bounds how many of a collection's entries
	// InlineList names. Five is a glance; the rest are in
	// structuredContent, which is where a caller that wants all twenty
	// should be looking.
	maxSummaryItems = 5
)

// Summary renders v as the readable half of a tool result.
//
// A type that implements Summarizer says it itself. Everything else
// gets a bounded outline of its top-level fields: zero values omitted
// because a card carries thirty fields and sets eight, collections
// counted because the ids in them are exactly what structuredContent
// is for, and long strings clipped because a card description is not a
// summary of a card.
func Summary(v any) string {
	if s, ok := v.(Summarizer); ok {
		if text := strings.TrimSpace(s.Summary()); text != "" {
			return text
		}
	}
	rv := reflect.ValueOf(v)
	lines := outline(rv, 0)
	if len(lines) == 0 {
		return "(empty result)"
	}
	if len(lines) > maxSummaryLines {
		omitted := len(lines) - maxSummaryLines
		lines = append(lines[:maxSummaryLines:maxSummaryLines],
			fmt.Sprintf("… %d more %s", omitted, Plural(omitted, "field", "fields")))
	}
	return strings.Join(lines, "\n")
}

// outline renders one value as zero or more indented lines.
func outline(rv reflect.Value, depth int) []string {
	rv = deref(rv)
	if !rv.IsValid() {
		return nil
	}
	if rv.Kind() != reflect.Struct || isTime(rv.Type()) {
		if s := scalar(rv); s != "" {
			return []string{s}
		}
		return nil
	}

	var lines []string
	for _, f := range reflect.VisibleFields(rv.Type()) {
		lines = append(lines, outlineField(rv, f, depth)...)
	}
	return lines
}

// outlineField renders one struct field: nothing for a field that is
// not ours to show or is at its zero value, one line for a scalar, and
// a nested block for a struct until the depth cap turns it into "{…}".
func outlineField(rv reflect.Value, f reflect.StructField, depth int) []string {
	name, inner, ok := visibleField(rv, f)
	if !ok {
		return nil
	}

	indent := strings.Repeat(summaryIndentStr, depth)
	switch {
	case inner.Kind() != reflect.Struct || isTime(inner.Type()):
		return []string{indent + name + ": " + scalar(inner)}
	case depth >= maxSummaryDepth:
		return []string{indent + name + ": {…}"}
	default:
		nested := outline(inner, depth+1)
		if len(nested) == 0 {
			return nil
		}
		return append([]string{indent + name + ":"}, nested...)
	}
}

// visibleField is the one rule for whether a struct field belongs in
// any rendering, and what to call it: exported, not embedded, not at
// its zero value, not json:"-", and reachable through any pointers.
// Both renderers ask it, so a change to what is showable is one edit
// rather than two in two different orders.
func visibleField(rv reflect.Value, f reflect.StructField) (name string, inner reflect.Value, ok bool) {
	if f.Anonymous || !f.IsExported() {
		return "", reflect.Value{}, false
	}
	fv := rv.FieldByIndex(f.Index)
	if fv.IsZero() {
		return "", reflect.Value{}, false
	}
	inner = deref(fv)
	if !inner.IsValid() {
		return "", reflect.Value{}, false
	}
	name = fieldName(f)
	if name == "" {
		return "", reflect.Value{}, false
	}
	return name, inner, true
}

// scalar formats one non-struct value for a single line.
func scalar(rv reflect.Value) string {
	rv = deref(rv)
	if !rv.IsValid() {
		return ""
	}
	if isTime(rv.Type()) {
		if t, ok := rv.Interface().(time.Time); ok {
			return t.UTC().Format(time.RFC3339)
		}
	}
	if s, ok := number(rv); ok {
		return s
	}
	if s, ok := collection(rv); ok {
		return s
	}
	switch rv.Kind() {
	case reflect.String:
		return clip(rv.String())
	case reflect.Bool:
		return strconv.FormatBool(rv.Bool())
	case reflect.Struct:
		return "{…}"
	default:
		return clip(fmt.Sprint(rv.Interface()))
	}
}

// collection counts the kinds whose contents are what
// structuredContent is for. A card's twelve tag ids say "12 items"
// here and appear in full there; putting them in both is how the
// readable half stops being shorter than the machine one.
func collection(rv reflect.Value) (string, bool) {
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		// json.RawMessage and friends are bytes, not a collection of
		// anything the caller would count.
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return fmt.Sprintf("%d bytes", rv.Len()), true
		}
		n := rv.Len()
		return fmt.Sprintf("%d %s", n, Plural(n, "item", "items")), true
	case reflect.Map:
		n := rv.Len()
		return fmt.Sprintf("%d %s", n, Plural(n, "entry", "entries")), true
	default:
		return "", false
	}
}

// number formats the numeric kinds, which are a third of reflect.Kind
// and none of the interesting part of scalar.
func number(rv reflect.Value) (string, bool) {
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10), true
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(rv.Float(), 'g', -1, 64), true
	default:
		return "", false
	}
}

// fieldName prefers the json tag, so the readable half and the
// machine half name the same field and a caller can move between them.
func fieldName(f reflect.StructField) string {
	tag, ok := f.Tag.Lookup("json")
	if !ok {
		return f.Name
	}
	name, _, _ := strings.Cut(tag, ",")
	switch name {
	case "-":
		return ""
	case "":
		return f.Name
	default:
		return name
	}
}

// clip renders a value as one line's worth: runs of whitespace become
// single spaces, and the result stops at maxSummaryValue.
//
// It collapses and measures in one pass, and stops as soon as it has
// enough. The obvious spelling — strings.Join(strings.Fields(s), " ")
// and then slice — normalises the whole input first, and the inputs
// here are card descriptions: on an 84 KB body that was 320 KB of
// garbage per call to produce 120 bytes, twice per call for the
// description editors, which return the old body and the new one.
// Stopping on a rune boundary also keeps the output valid UTF-8, which
// slicing at a byte offset did not.
func clip(s string) string {
	var b strings.Builder
	b.Grow(min(len(s), maxSummaryValue) + len(ellipsis))
	pendingSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			// Leading whitespace is dropped, and trailing whitespace
			// never gets written because a pending space is only
			// emitted when a non-space follows it.
			pendingSpace = b.Len() > 0
			continue
		}
		if b.Len()+runeWidth(r, pendingSpace) > maxSummaryValue {
			return b.String() + ellipsis
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// runeWidth is how many bytes writing r would add, counting the space
// that would have to precede it.
func runeWidth(r rune, pendingSpace bool) int {
	n := utf8.RuneLen(r)
	if pendingSpace {
		n++
	}
	return n
}

// ellipsis marks a value that was cut.
const ellipsis = "…"

func deref(rv reflect.Value) reflect.Value {
	for rv.IsValid() && (rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface) {
		if rv.IsNil() {
			return reflect.Value{}
		}
		rv = rv.Elem()
	}
	return rv
}

func isTime(t reflect.Type) bool { return t == reflect.TypeOf(time.Time{}) }

// Plural is the obvious helper, exported because internal/tools
// builds its own summary headers ("52 items", "7 candidates") and
// already imports this package.
func Plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// InlineList renders the first few entries of a collection, one
// per line and indented, with a count of whatever it left out.
//
// It is here rather than in the caller because the bound and the
// wording of the elision are the same decision for every collection,
// and they were written twice before this existed.
func InlineList[T any](items []T) string {
	var b strings.Builder
	for i, item := range items {
		if i == maxSummaryItems {
			fmt.Fprintf(&b, "\n… %d more", len(items)-maxSummaryItems)
			break
		}
		b.WriteString("\n" + summaryIndentStr + Inline(item))
	}
	return b.String()
}

// maxInlineFields bounds one Inline line. Five is what fits a terminal
// row beside a label and is enough for the identifying fields of every
// Favro resource: an id, a name, and whatever distinguishes it.
const maxInlineFields = 5

// Inline renders v as a single "key=value key=value" line, for a
// collection's entries — the place where a per-item outline would turn
// a twenty-item page into a hundred lines and the JSON would have been
// shorter.
func Inline(v any) string {
	rv := deref(reflect.ValueOf(v))
	if !rv.IsValid() {
		return ""
	}
	if rv.Kind() != reflect.Struct || isTime(rv.Type()) {
		return scalar(rv)
	}

	var parts []string
	for _, f := range reflect.VisibleFields(rv.Type()) {
		if len(parts) == maxInlineFields {
			break
		}
		if name, value, ok := inlineField(rv, f); ok {
			parts = append(parts, name+"="+quoteIfNeeded(value))
		}
	}
	if len(parts) == 0 {
		return "(no scalar fields)"
	}
	return strings.Join(parts, " ")
}

// inlineField reports whether f belongs on a one-line entry, and what
// it reads as. Collections are out: on one line, "3 items" for a field
// nobody asked about is noise.
func inlineField(rv reflect.Value, f reflect.StructField) (name, value string, ok bool) {
	name, inner, ok := visibleField(rv, f)
	if !ok {
		return "", "", false
	}
	switch inner.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return "", "", false
	case reflect.Struct:
		if !isTime(inner.Type()) {
			return "", "", false
		}
	}
	return name, scalar(inner), true
}

// quoteIfNeeded quotes a value only when leaving it bare would make the
// line ambiguous — a name with a space in it reads as two fields
// otherwise. Numbers and ids stay bare, which is what makes the line
// scannable.
func quoteIfNeeded(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\"") {
		return strconv.Quote(s)
	}
	return s
}
