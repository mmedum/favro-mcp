package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestClassesAgainstThisRepository is the gate doing its real job. It
// has to pass here, because a gate that only ever runs against a
// fixture is a gate nobody has watched succeed against the thing it
// guards.
func TestClassesAgainstThisRepository(t *testing.T) {
	var out sink
	if err := classes(&out, nil); err != nil {
		t.Fatalf("the error vocabulary and its documentation disagree: %v", err)
	}
	out.mustSay(t, "in the vocabulary")
	out.mustSay(t, "§6.2")
}

const sampleClassSource = `package render

type Class string

const (
	ClassInvalid  Class = "invalid"
	ClassNotFound Class = "not_found"
)

// A const block of something else must not be read as a vocabulary.
const (
	maxThings int = 3
)

var Classes = []Class{ClassInvalid, ClassNotFound}

func Classify(err error) Class {
	if err == nil {
		return ClassInvalid
	}
	return ClassNotFound
}
`

func TestDeclaredClassesReadsValuesNotIdentifiers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "class.go")
	if err := os.WriteFile(path, []byte(sampleClassSource), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := declaredClasses(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"invalid", "not_found"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v — the wire value is what a caller sees in [class], not the Go identifier", got, want)
	}
}

const sampleDoc = `## 6. Addressing and error classes

### 6.1 How the model addresses things

Prose that mentions ` + "`invalid`" + ` in passing, and a table of its own:

| Thing | Note |
|---|---|
| a row | that is not a class |

### 6.2 Error classes

| Class | When | What the caller does about it |
|---|---|---|
| ` + "`invalid`" + ` | bad arguments | change them |
| ` + "`not_found`" + ` | not there | stop looking |

Prose after the table mentioning ` + "`unverified`" + `, which is not a class.

### 6.3 Something else

| ` + "`leaked`" + ` | a class in the next section | must not be read |
`

// The parser has to take the table in §6.2 and nothing else: a class
// name in prose, in §6.1's unrelated table, or in §6.3 would each
// quietly widen or narrow what the gate thinks is documented.
func TestDocumentedClassesTakesOnlyItsOwnTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "architecture.md")
	if err := os.WriteFile(path, []byte(sampleDoc), 0o600); err != nil {
		t.Fatal(err)
	}

	got, rows, err := documentedClasses(path)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Errorf("read %d rows, want 2", rows)
	}
	if strings.Join(got, ",") != "invalid,not_found" {
		t.Errorf("got %v; prose, §6.1's table and §6.3 must all be out of scope", got)
	}
}

// A renamed or deleted section must fail rather than agree with an
// empty set — the failure this whole gate design exists to prevent.
func TestDocumentedClassesReportsAMissingSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "architecture.md")
	if err := os.WriteFile(path, []byte("# Nothing here\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, rows, err := documentedClasses(path)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 0 || len(got) != 0 {
		t.Errorf("got %v rows=%d, want nothing found", got, rows)
	}
}

// TestListedClassesResolvesTheSlice covers the check that replaced a
// unit test in internal/render: the const block and the Classes slice
// are two lists nothing in Go makes agree.
func TestListedClassesResolvesTheSlice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "class.go")
	if err := os.WriteFile(path, []byte(sampleClassSource), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := listedClasses(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "invalid,not_found" {
		t.Errorf("got %v, want the slice resolved back to wire values", got)
	}
}

// A slice member with no constant behind it has to surface as itself,
// not vanish into a shorter list — the caller compares the two sets and
// needs something to name.
func TestListedClassesSurfacesAnUnknownMember(t *testing.T) {
	src := strings.Replace(sampleClassSource,
		"var Classes = []Class{ClassInvalid, ClassNotFound}",
		"var Classes = []Class{ClassInvalid, ClassNotFound, ClassInvented}", 1)
	path := filepath.Join(t.TempDir(), "class.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := listedClasses(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got, "ClassInvented") {
		t.Errorf("got %v, want the unknown identifier reported", got)
	}
}

func TestCamel(t *testing.T) {
	cases := map[string]string{
		"invalid":      "Invalid",
		"not_found":    "NotFound",
		"rate_limited": "RateLimited",
		"":             "",
	}
	for in, want := range cases {
		if got := camel(in); got != want {
			t.Errorf("camel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestIsClassDeclaration covers the distinction the gate's first run
// got wrong: the const block and the Classes slice are the vocabulary
// declaring itself, and Classify — in the same file — is the code that
// emits it.
func TestIsClassDeclaration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "class.go")
	if err := os.WriteFile(path, []byte(sampleClassSource), 0o600); err != nil {
		t.Fatal(err)
	}

	file, fsetErr := parseGoFile(path)
	if fsetErr != nil {
		t.Fatal(fsetErr)
	}

	var declarations, other int
	for _, decl := range file.Decls {
		if isClassDeclaration(decl) {
			declarations++
		} else {
			other++
		}
	}
	if declarations != 2 {
		t.Errorf("found %d declarations, want the const block and the Classes slice", declarations)
	}
	if other < 3 {
		t.Errorf("found %d other declarations, want the type, the unrelated const block and Classify", other)
	}
}
