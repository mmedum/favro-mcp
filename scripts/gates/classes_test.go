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
	out.mustSay(t, "docs/TOOLS.md#error-classes")
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

const sampleDoc = `# Tool reference

## Tools

Prose that mentions ` + "`invalid`" + ` in passing, and a table of its own:

| Tool | What it does |
| --- | --- |
| ` + "`favro_ping`" + ` | a row that is not a class |

### Error classes

| ` + "`early`" + ` | under a heading that only ends like the section's | must not be read |

## Error classes

` + "```markdown" + `
## A heading in an example
| ` + "`fenced`" + ` | a row in an example | must not be read |
` + "```" + `

| Class | When | What the caller does |
|---|---|---|
| ` + "`invalid`" + ` | bad arguments | change them |
| ` + "`not_found`" + ` | not there | stop looking |

Prose after the table mentioning ` + "`unverified`" + `, which is not a class.

### Retrying

| ` + "`leaked`" + ` | a row under a subheading | must not be read |

## Next

| ` + "`later`" + ` | a row in the next section | must not be read |
`

// The parser has to take the error-class table and nothing else: a
// class name in prose, in another table, in a fenced example, under a
// heading that only ends like the section's, or past the next heading
// would each quietly widen or narrow what the gate thinks is documented.
// A heading in a fence ends nothing. CRLF line endings read the same.
func TestDocumentedClassesTakesOnlyItsOwnTable(t *testing.T) {
	for name, doc := range map[string]string{
		"LF":   sampleDoc,
		"CRLF": strings.ReplaceAll(sampleDoc, "\n", "\r\n"),
	} {
		path := filepath.Join(t.TempDir(), "TOOLS.md")
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}

		got, rows, err := documentedClasses(path)
		if err != nil {
			t.Fatal(err)
		}
		if rows != 2 {
			t.Errorf("%s: read %d rows, want 2", name, rows)
		}
		if strings.Join(got, ",") != "invalid,not_found" {
			t.Errorf("%s: got %v; prose, fences, other tables and other sections must all be out of scope", name, got)
		}
	}
}

// The table is held to the vocabulary both ways: a declared class with
// no row fails, and a row naming no declared class fails.
func TestClassesHoldTheTableBothWays(t *testing.T) {
	declared := []string{"invalid", "not_found"}
	for _, tc := range []struct {
		name, old, new string
		want           []string
	}{
		{"the table as it stands", "", "", nil},
		{"a declared class with no row", "| `not_found` | not there | stop looking |\n", "", []string{
			`internal/render/class.go declares "not_found", and docs/TOOLS.md#error-classes does not name it`,
		}},
		{"a row naming no declared class", "| `invalid` |", "| `invented` | made up | nothing |\n| `invalid` |", []string{
			`docs/TOOLS.md#error-classes documents "invented", and internal/render/class.go declares no constant for it`,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := sampleDoc
			if tc.old != "" {
				if !strings.Contains(doc, tc.old) {
					t.Fatalf("the sample does not contain %q", tc.old)
				}
				doc = strings.Replace(doc, tc.old, tc.new, 1)
			}
			path := filepath.Join(t.TempDir(), "TOOLS.md")
			if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
				t.Fatal(err)
			}
			documented, _, err := documentedClasses(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := classesDisagree(declared, declared, documented, declared); !slices.Equal(got, tc.want) {
				t.Errorf("problems\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// A renamed or deleted section must fail rather than agree with an
// empty set — the failure this whole gate design exists to prevent.
func TestDocumentedClassesReportsAMissingSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "TOOLS.md")
	if err := os.WriteFile(path, []byte("# Nothing here\n\n### Error classes\n\n| `invalid` | a row | under the wrong heading |\n"), 0o600); err != nil {
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
