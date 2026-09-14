package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// apiFieldsWaivedPath records the documented fields this server
// deliberately does not model, and why.
const apiFieldsWaivedPath = "testdata/api-fields-waived.tsv"

// apiFieldsFloors: a gate that compared two empty sets would print the
// same sentence as one that compared fifteen resources.
const (
	minFieldResources = 12
	minFieldsChecked  = 100
	minWaiverReason   = 40
)

// sectionTypes maps a reference section to the wire type that models
// it. Hand-written, and held: every section with a field table must
// appear here, and every type named must exist.
var sectionTypes = map[string]string{
	"Users":             "User",
	"Organizations":     "Organization",
	"Collections":       "Collection",
	"Widgets":           "Widget",
	"Columns":           "Column",
	"Cards":             "Card",
	"Activities":        "Activity",
	"Tags":              "Tag",
	"Tasks":             "Task",
	"Tasklists":         "Tasklist",
	"Comments":          "Comment",
	"Attachments":       "CardAttachment",
	"Custom fields":     "CustomField",
	"Groups":            "Group",
	"Outgoing webhooks": "Webhook",
}

// waiver is one documented field this server does not model.
type waiver struct {
	section string
	field   string
	reason  string
	line    int
}

// apiFields holds the documented field lists against the wire types.
//
// It answers one question `api-coverage` cannot: an endpoint can be
// implemented while the type behind it quietly ignores half of what
// Favro sends. A field added to a Favro resource is invisible to a
// client that decodes into a struct without it — encoding/json drops
// what it does not recognise, which is what makes reads tolerant and
// also what makes this silent.
//
// Runs offline, against whatever `gates api-diff` last fetched.
func apiFields(w io.Writer, _ []string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}

	surface, err := readSurface(filepath.Join(root, filepath.FromSlash(apiSurfacePath)))
	if err != nil {
		return err
	}
	if len(surface.Resources) < minFieldResources {
		return fmt.Errorf("%s holds %d resource field lists, expected at least %d — refetch it with `make api-diff`",
			apiSurfacePath, len(surface.Resources), minFieldResources)
	}

	modelled, err := wireFields(filepath.Join(root, "internal", "favro"))
	if err != nil {
		return err
	}

	waived, err := readWaivers(filepath.Join(root, filepath.FromSlash(apiFieldsWaivedPath)))
	if err != nil {
		return err
	}

	problems, checked := checkFields(surface.Resources, modelled, waived)
	if checked < minFieldsChecked {
		return fmt.Errorf("compared only %d documented fields, expected at least %d", checked, minFieldsChecked)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("the documented fields and the wire types disagree:\n  %s", strings.Join(problems, "\n  "))
	}

	_, err = fmt.Fprintf(w, "api-fields ok: %d documented fields across %d resources, each modelled or waived with a reason (%d waived)\n",
		checked, len(surface.Resources), len(waived))
	return err
}

// checkFields compares each documented field against the type's tags.
func checkFields(resources []apiResource, modelled map[string]map[string]bool, waived []waiver) ([]string, int) {
	var problems []string
	checked := 0

	waivedBy := map[string]waiver{}
	for _, wv := range waived {
		waivedBy[wv.section+"\x00"+wv.field] = wv
	}
	used := map[string]bool{}

	for _, r := range resources {
		typeName, ok := sectionTypes[r.Section]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"the reference documents a %q resource and sectionTypes names no wire type for it", r.Section))
			continue
		}
		tags, ok := modelled[typeName]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"sectionTypes maps %q to favro.%s, which is not a struct in internal/favro", r.Section, typeName))
			continue
		}
		for _, field := range r.Fields {
			checked++
			if tags[field] {
				continue
			}
			key := r.Section + "\x00" + field
			if wv, ok := waivedBy[key]; ok {
				used[key] = true
				if len(wv.reason) < minWaiverReason {
					problems = append(problems, fmt.Sprintf("%s:%d: %s.%s is waived in %d characters; say why",
						apiFieldsWaivedPath, wv.line, r.Section, field, len(wv.reason)))
				}
				continue
			}
			problems = append(problems, fmt.Sprintf(
				"Favro documents %s.%s and favro.%s has no json tag for it — model it, or waive it with a reason in %s",
				r.Section, field, typeName, apiFieldsWaivedPath))
		}
	}

	for _, wv := range waived {
		if !used[wv.section+"\x00"+wv.field] {
			problems = append(problems, fmt.Sprintf(
				"%s:%d: %s.%s is waived and is either modelled or no longer documented — a waiver that waives nothing reads like a decision",
				apiFieldsWaivedPath, wv.line, wv.section, wv.field))
		}
	}
	return problems, checked
}

// wireFields returns, per struct, the json tag names it carries.
func wireFields(dir string) (map[string]map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				names := map[string]bool{}
				for _, f := range st.Fields.List {
					if f.Tag == nil {
						continue
					}
					tag := reflect.StructTag(strings.Trim(f.Tag.Value, "`"))
					name, _, _ := strings.Cut(tag.Get("json"), ",")
					if name != "" && name != "-" {
						names[name] = true
					}
				}
				out[ts.Name.Name] = names
			}
		}
	}
	return out, nil
}

// readWaivers parses the waiver file: SECTION, FIELD, REASON.
func readWaivers(path string) ([]waiver, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []waiver
	for i, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s:%d: got %d tab-separated fields, want 3 (SECTION FIELD REASON)",
				apiFieldsWaivedPath, i+1, len(fields))
		}
		out = append(out, waiver{
			section: strings.TrimSpace(fields[0]),
			field:   strings.TrimSpace(fields[1]),
			reason:  strings.TrimSpace(fields[2]),
			line:    i + 1,
		})
	}
	return out, nil
}
