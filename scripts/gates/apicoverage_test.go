package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAPICoverageAgainstThisRepository is the gate doing its real job.
// A gate that only ever runs against a fixture is one nobody has
// watched succeed against the thing it guards.
func TestAPICoverageAgainstThisRepository(t *testing.T) {
	var out sink
	if err := apiCoverage(&out, nil); err != nil {
		t.Fatalf("the API surface and its verdicts disagree: %v", err)
	}
	out.mustSay(t, "endpoints in")
	out.mustSay(t, "implemented")
	out.mustSay(t, "client methods named and all present")
}

// The surface a fixture stands in for: two implemented endpoints, one
// written off, and a family decided together.
const sampleSurface = `{
  "source": "https://example.test/developer/",
  "fetched_at": "2026-09-14",
  "endpoints": [
    {"method": "GET", "path": "/api/v1/things", "section": "Things"},
    {"method": "POST", "path": "/api/v1/things", "section": "Things"},
    {"method": "DELETE", "path": "/api/v1/things/:id", "section": "Things"},
    {"method": "GET", "path": "/api/scim/v2/Users", "section": "SCIM"},
    {"method": "POST", "path": "/api/scim/v2/Users", "section": "SCIM"}
  ]
}`

const longReason = "A reason long enough to be a reason, saying what the endpoint does and why this server does not do it."

// writeFixture lays out a surface and a coverage file in a temp dir and
// returns the checkCoverage inputs.
func writeFixture(t *testing.T, surfaceJSON, coverageTSV string) ([]apiEndpoint, []coverageRow) {
	t.Helper()

	dir := t.TempDir()
	sp := filepath.Join(dir, "surface.json")
	cp := filepath.Join(dir, "coverage.tsv")
	if err := os.WriteFile(sp, []byte(surfaceJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cp, []byte(coverageTSV), 0o600); err != nil {
		t.Fatal(err)
	}
	surface, err := readSurface(sp)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := readCoverage(cp)
	if err != nil {
		t.Fatal(err)
	}
	return surface.Endpoints, rows
}

func TestCheckCoverageAcceptsAWholeSet(t *testing.T) {
	tsv := "GET\t/api/v1/things\timplemented\tListThings\n" +
		"POST\t/api/v1/things\timplemented\tCreateThing\n" +
		"DELETE\t/api/v1/things/:id\tout\t" + longReason + "\n" +
		"*\t/api/scim/*\tout\t" + longReason + "\n"
	endpoints, rows := writeFixture(t, sampleSurface, tsv)

	methods := map[string]bool{"ListThings": true, "CreateThing": true}
	if problems := checkCoverage(endpoints, rows, methods); len(problems) != 0 {
		t.Errorf("a complete set should pass: %v", problems)
	}
}

// An endpoint nobody decided about is the obvious half of the gate.
func TestCheckCoverageReportsAnUndecidedEndpoint(t *testing.T) {
	tsv := "GET\t/api/v1/things\timplemented\tListThings\n" +
		"*\t/api/scim/*\tout\t" + longReason + "\n"
	endpoints, rows := writeFixture(t, sampleSurface, tsv)

	problems := checkCoverage(endpoints, rows, map[string]bool{"ListThings": true})
	if len(problems) != 2 {
		t.Fatalf("got %d problems, want one per undecided endpoint: %v", len(problems), problems)
	}
	for _, want := range []string{"POST /api/v1/things", "DELETE /api/v1/things/:id"} {
		if !strings.Contains(strings.Join(problems, "\n"), want) {
			t.Errorf("problems do not name %s: %v", want, problems)
		}
	}
}

// The half that actually rots: a verdict for an endpoint that is no
// longer documented reads exactly like a considered decision.
func TestCheckCoverageReportsAVerdictWithNoEndpoint(t *testing.T) {
	tsv := "GET\t/api/v1/things\timplemented\tListThings\n" +
		"POST\t/api/v1/things\timplemented\tCreateThing\n" +
		"DELETE\t/api/v1/things/:id\tout\t" + longReason + "\n" +
		"GET\t/api/v1/removed\tout\t" + longReason + "\n" +
		"*\t/api/scim/*\tout\t" + longReason + "\n"
	endpoints, rows := writeFixture(t, sampleSurface, tsv)

	problems := checkCoverage(endpoints, rows, map[string]bool{"ListThings": true, "CreateThing": true})
	if len(problems) != 1 || !strings.Contains(problems[0], "/api/v1/removed") {
		t.Errorf("got %v, want the stale verdict reported", problems)
	}
}

// An "implemented" verdict is only worth something if the method it
// names exists.
func TestCheckCoverageReportsAMissingMethod(t *testing.T) {
	tsv := "GET\t/api/v1/things\timplemented\tListThings,ListThingsRenamed\n" +
		"POST\t/api/v1/things\timplemented\tCreateThing\n" +
		"DELETE\t/api/v1/things/:id\tout\t" + longReason + "\n" +
		"*\t/api/scim/*\tout\t" + longReason + "\n"
	endpoints, rows := writeFixture(t, sampleSurface, tsv)

	problems := checkCoverage(endpoints, rows, map[string]bool{"ListThings": true, "CreateThing": true})
	if len(problems) != 1 || !strings.Contains(problems[0], "ListThingsRenamed") {
		t.Errorf("got %v, want the renamed method reported", problems)
	}
}

// "not yet" is not a reason.
func TestCheckCoverageReportsAThinReason(t *testing.T) {
	tsv := "GET\t/api/v1/things\timplemented\tListThings\n" +
		"POST\t/api/v1/things\timplemented\tCreateThing\n" +
		"DELETE\t/api/v1/things/:id\tout\tnot yet\n" +
		"*\t/api/scim/*\tout\t" + longReason + "\n"
	endpoints, rows := writeFixture(t, sampleSurface, tsv)

	problems := checkCoverage(endpoints, rows, map[string]bool{"ListThings": true, "CreateThing": true})
	if len(problems) != 1 || !strings.Contains(problems[0], "say why") {
		t.Errorf("got %v, want the thin reason reported", problems)
	}
}

// Two rows claiming one endpoint means nobody can tell which decision
// applies.
func TestCheckCoverageReportsOverlappingRows(t *testing.T) {
	tsv := "GET\t/api/v1/things\timplemented\tListThings\n" +
		"*\t/api/v1/*\tout\t" + longReason + "\n" +
		"*\t/api/scim/*\tout\t" + longReason + "\n"
	endpoints, rows := writeFixture(t, sampleSurface, tsv)

	problems := checkCoverage(endpoints, rows, map[string]bool{"ListThings": true})
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "one endpoint, one decision") {
		t.Errorf("got %v, want the overlap reported", problems)
	}
}

func TestReadCoverageRejectsAMalformedRow(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "coverage.tsv")
	if err := os.WriteFile(p, []byte("# fine\nGET\t/api/v1/things\timplemented\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCoverage(p); err == nil {
		t.Error("a three-field row should not parse")
	}
}

// TestParseReferenceNeedsARealPage covers the floors: a page that is
// not the reference must fail loudly rather than yield a short list
// that then agrees with a short set of verdicts.
func TestParseReferenceNeedsARealPage(t *testing.T) {
	if _, _, err := parseReference([]byte("<html><body>nothing here</body></html>")); err == nil {
		t.Error("a page with no endpoints should not parse as a surface")
	}

	var page strings.Builder
	for i := range apiMinSections {
		page.WriteString("<h1 id=\"s" + string(rune('a'+i)) + "\">Section</h1>")
	}
	page.WriteString(`<p><code>GET https://favro.com/api/v1/things</code></p>`)
	if _, _, err := parseReference([]byte(page.String())); err == nil {
		t.Error("one endpoint is not the reference surface; the floor should reject it")
	}
}
