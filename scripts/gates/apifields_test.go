package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAPIFieldsAgainstThisRepository is the gate against the thing it
// guards, which is the run that matters.
func TestAPIFieldsAgainstThisRepository(t *testing.T) {
	var out sink
	if err := apiFields(&out, nil); err != nil {
		t.Fatalf("the documented fields and the wire types disagree: %v", err)
	}
	out.mustSay(t, "documented fields across")
	out.mustSay(t, "modelled or waived")
}

// TestEverySectionWithFieldsHasAType holds the one hand-written list in
// this gate. A resource the reference documents and sectionTypes does
// not name is a resource whose fields nothing compares.
func TestEverySectionWithFieldsHasAType(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	surface, err := readSurface(filepath.Join(root, filepath.FromSlash(apiSurfacePath)))
	if err != nil {
		t.Fatal(err)
	}
	if len(surface.Resources) < minFieldResources {
		t.Fatalf("read %d resources, expected at least %d", len(surface.Resources), minFieldResources)
	}

	modelled, err := wireFields(filepath.Join(root, "internal", "favro"))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range surface.Resources {
		name, ok := sectionTypes[r.Section]
		if !ok {
			t.Errorf("the reference documents %q and sectionTypes names no type for it", r.Section)
			continue
		}
		if _, ok := modelled[name]; !ok {
			t.Errorf("sectionTypes maps %q to favro.%s, which is not a struct in internal/favro", r.Section, name)
		}
	}
	for section := range sectionTypes {
		found := false
		for _, r := range surface.Resources {
			if r.Section == section {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("sectionTypes names %q, which the reference no longer documents with a field table", section)
		}
	}
}

const fieldFixture = `{"source":"x","fetched_at":"2026-09-14","endpoints":[],"resources":[
  {"section":"Tags","fields":["tagId","name","mystery"]}
]}`

func fieldInputs(t *testing.T, waiversTSV string) ([]apiResource, map[string]map[string]bool, []waiver) {
	t.Helper()

	dir := t.TempDir()
	sp := filepath.Join(dir, "surface.json")
	if err := os.WriteFile(sp, []byte(fieldFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	surface, err := readSurface(sp)
	if err != nil {
		t.Fatal(err)
	}
	wp := filepath.Join(dir, "waived.tsv")
	if err := os.WriteFile(wp, []byte(waiversTSV), 0o600); err != nil {
		t.Fatal(err)
	}
	waived, err := readWaivers(wp)
	if err != nil {
		t.Fatal(err)
	}
	modelled := map[string]map[string]bool{"Tag": {"tagId": true, "name": true}}
	return surface.Resources, modelled, waived
}

const goodReason = "A reason long enough to say what the field is and why nothing here parses it."

// A documented field with neither a tag nor a waiver is the gap this
// gate exists to find.
func TestCheckFieldsReportsAnUnmodelledField(t *testing.T) {
	resources, modelled, waived := fieldInputs(t, "")
	problems, checked := checkFields(resources, modelled, waived)
	if checked != 3 {
		t.Errorf("checked %d fields, want 3", checked)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "mystery") {
		t.Errorf("got %v, want the unmodelled field reported", problems)
	}
}

func TestCheckFieldsAcceptsAWaiver(t *testing.T) {
	resources, modelled, waived := fieldInputs(t, "Tags\tmystery\t"+goodReason+"\n")
	if problems, _ := checkFields(resources, modelled, waived); len(problems) != 0 {
		t.Errorf("a waived field should pass: %v", problems)
	}
}

func TestCheckFieldsRejectsAThinWaiver(t *testing.T) {
	resources, modelled, waived := fieldInputs(t, "Tags\tmystery\tlater\n")
	problems, _ := checkFields(resources, modelled, waived)
	if len(problems) != 1 || !strings.Contains(problems[0], "say why") {
		t.Errorf("got %v, want the thin reason reported", problems)
	}
}

// The half that rots: a waiver for a field that is modelled, or that
// Favro no longer documents, reads like a decision and is residue.
func TestCheckFieldsReportsAStaleWaiver(t *testing.T) {
	resources, modelled, waived := fieldInputs(t,
		"Tags\tmystery\t"+goodReason+"\nTags\tname\t"+goodReason+"\n")
	problems, _ := checkFields(resources, modelled, waived)
	if len(problems) != 1 || !strings.Contains(problems[0], "waives nothing") {
		t.Errorf("got %v, want the stale waiver reported", problems)
	}
}

// TestParseResourcesNeedsRealTables covers the floor: a page with no
// field tables must fail rather than agree with an empty set.
func TestParseResourcesNeedsRealTables(t *testing.T) {
	if _, err := parseResources([]byte("<html><h1 id=\"a\">A</h1></html>")); err == nil {
		t.Error("a page with no field tables should not parse as a set of resources")
	}
}
