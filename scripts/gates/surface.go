package main

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// toolSurface is one schema dump, read for the comparison: every item of
// every list, keyed by itemKey, every other key but the build stamp, and
// each tool's inputs, which are the contract (CONTRIBUTING, Versioning):
// renaming or removing an input breaks a caller, and so does requiring
// one.
type toolSurface struct {
	items  map[string]map[string]any
	rest   map[string]any
	inputs map[string]toolInputs
}

// toolInputs is one tool's input fields, and which of them are required.
type toolInputs struct {
	fields, required []string
}

// stampKeys are the dump's build stamp. They change with the build and
// are not part of the surface; what an SDK bump changes on the wire shows
// up inside the tools.
var stampKeys = []string{"server", "sdk", "version"}

// readToolSurface reads one dump.
func readToolSurface(data []byte) (toolSurface, error) {
	d, err := parseSchemas(data)
	if err != nil {
		return toolSurface{}, err
	}
	items, rest, err := dumpItems(data)
	if err != nil {
		return toolSurface{}, err
	}
	s := toolSurface{items: items, rest: rest, inputs: map[string]toolInputs{}}
	for _, t := range d.Tools {
		s.inputs[t.Name] = toolInputs{fields: slices.Sorted(maps.Keys(t.InputSchema.Properties)), required: t.InputSchema.Required}
	}
	return s, nil
}

// dumpItems reads every item of every list in a dump, keyed by itemKey,
// and every other key but the build stamp. A list with an item that has
// no identity is compared as one value, since a position is no identity.
func dumpItems(data []byte) (map[string]map[string]any, map[string]any, error) {
	var whole map[string]any
	if err := json.Unmarshal(data, &whole); err != nil {
		return nil, nil, err
	}
	for _, k := range stampKeys {
		delete(whole, k)
	}
	items := map[string]map[string]any{}
	rest := map[string]any{}
	for key, v := range whole {
		list, isList := v.([]any)
		keyed := map[string]map[string]any{}
		for _, item := range list {
			obj, _ := item.(map[string]any)
			id := itemKey(key, obj)
			if id == "" {
				keyed = nil
				break
			}
			if _, twice := keyed[id]; twice {
				return nil, nil, fmt.Errorf("the dump names %q twice", id)
			}
			keyed[id] = obj
		}
		if !isList || keyed == nil {
			rest[key] = v
			continue
		}
		for id, obj := range keyed {
			if _, twice := items[id]; twice {
				return nil, nil, fmt.Errorf("the dump names %q twice", id)
			}
			items[id] = obj
		}
	}
	return items, rest, nil
}

// itemKey is how an item of a dumped list is named in the comparison: a
// tool by its name, a resource by the template or URI a client asks for,
// and an item of any other list by the list and its name. Empty when the
// item has none.
func itemKey(list string, item map[string]any) string {
	id := ""
	for _, k := range []string{"uriTemplate", "uri", "name"} {
		if v, ok := item[k].(string); ok && v != "" {
			id = v
			break
		}
	}
	switch {
	case id == "":
		return ""
	case list == "tools":
		return id
	case list == "resourceTemplates" || list == "resources":
		return "resource " + id
	}
	return list + " " + id
}

// itemName is what a footer names an item by, from its key in the
// surface. A tool's key is its name; every other key is a word for its
// list, a space and the name, as itemKey builds it, and a tool's name
// has no space.
func itemName(key string) string {
	if _, name, ok := strings.Cut(key, " "); ok {
		return name
	}
	return key
}

// A change is one difference between two dumps: the item it is to, which
// is what a footer names, and how the report shows it. The item is a
// tool's name, a resource's template or URI, a list item's name, or a
// top-level key such as instructions. field is, on the break of a tool
// still there, the field it breaks, such as "input card_id", and on a
// removed item its whole key, so a removed prompt and a tool of one name
// differ.
type change struct {
	item, text, field string
}

// surfaceChanges is what differs between two surfaces. breaking is what
// breaks a caller: an item gone, or a tool's input gone or newly
// required. changed is every other difference, an output, an annotation
// or a _meta mark included, named by the part that moved.
type surfaceChanges struct {
	added, changed, breaking []change
}

func (c surfaceChanges) empty() bool {
	return len(c.added) == 0 && len(c.changed) == 0 && len(c.breaking) == 0
}

// compareSurfaces is what changed from was to now. A tool that broke
// also lists every other change to it, its schemas' included: the break
// names only the fields it broke.
func compareSurfaces(was, now toolSurface) surfaceChanges {
	var c surfaceChanges
	var removed []change
	for _, key := range sortedUnion(was.items, now.items) {
		before, inWas := was.items[key]
		after, inNow := now.items[key]
		name := itemName(key)
		switch {
		case !inNow:
			removed = append(removed, change{item: name, text: key + " removed", field: key})
		case !inWas:
			c.added = append(c.added, change{item: name, text: key})
		default:
			if parts := changedParts(before, after); len(parts) > 0 {
				c.changed = append(c.changed, change{item: name, text: key + " (" + strings.Join(parts, ", ") + ")"})
			}
		}
	}
	for _, part := range changedParts(was.rest, now.rest) {
		top, _, _ := strings.Cut(part, ".")
		c.changed = append(c.changed, change{item: top, text: part})
	}
	c.breaking = slices.Concat(removed, inputBreaks(was.inputs, now.inputs))
	return c
}

// inputBreaks is each input a tool kept by name lost, and each it newly
// requires, whether the field is new or was optional. A removed tool is
// the caller's to report.
func inputBreaks(was, now map[string]toolInputs) []change {
	var out []change
	broke := func(name, field, how string) {
		out = append(out, change{name, name + ": input field " + field + " " + how, "input " + field})
	}
	for _, name := range slices.Sorted(maps.Keys(was)) {
		kept, ok := now[name]
		if !ok {
			continue
		}
		for _, f := range was[name].fields {
			if !slices.Contains(kept.fields, f) {
				broke(name, f, "removed")
			}
		}
		for _, f := range slices.Sorted(slices.Values(kept.required)) {
			if !slices.Contains(was[name].required, f) {
				broke(name, f, "newly required")
			}
		}
	}
	return out
}

// write prints the changes to w, under a header naming what they are
// against.
func (c surfaceChanges) write(w io.Writer, against string) {
	_, _ = fmt.Fprintf(w, "against %s: %d added, %d changed, %d breaking\n", against, len(c.added), len(c.changed), len(c.breaking))
	for _, n := range c.added {
		_, _ = fmt.Fprintf(w, "  + %s\n", n.text)
	}
	for _, n := range c.changed {
		_, _ = fmt.Fprintf(w, "  ~ %s\n", n.text)
	}
	for _, b := range c.breaking {
		_, _ = fmt.Fprintf(w, "  ! %s  BREAKING\n", b.text)
	}
}

// changedParts names what differs between two dumped objects, one level
// deep, so a flipped hint reads as annotations.destructiveHint. Values
// are compared decoded, so a reordered key is the same schema.
func changedParts(o, n map[string]any) []string {
	var parts []string
	for _, k := range sortedUnion(o, n) {
		if reflect.DeepEqual(o[k], n[k]) {
			continue
		}
		om, oMap := o[k].(map[string]any)
		nm, nMap := n[k].(map[string]any)
		if !oMap || !nMap {
			parts = append(parts, k)
			continue
		}
		for _, sub := range sortedUnion(om, nm) {
			if !reflect.DeepEqual(om[sub], nm[sub]) {
				parts = append(parts, k+"."+sub)
			}
		}
	}
	return parts
}

// sortedUnion is every key of a and b, sorted.
func sortedUnion[V any](a, b map[string]V) []string {
	keys := slices.AppendSeq(slices.Collect(maps.Keys(a)), maps.Keys(b))
	slices.Sort(keys)
	return slices.Compact(keys)
}
