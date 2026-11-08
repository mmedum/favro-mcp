package main

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"strings"
	"testing"
)

const sampleChangelog = `# Changelog

## [Unreleased]

### Added
- Something not yet released.

## [1.1.2] - 2026-08-27

### Fixed
- The thing that was wrong.
- The other thing.

## [1.1.1] - 2026-08-26

### Added
- An earlier entry.

[1.1.2]: https://example.test/compare/v1.1.1...v1.1.2
`

func TestChangelogSectionTakesOneVersion(t *testing.T) {
	got := sectionFor(sampleChangelog, "1.1.2")
	if !strings.Contains(got, "The thing that was wrong.") {
		t.Errorf("the section is missing its own entries: %q", got)
	}
	if strings.Contains(got, "An earlier entry") {
		t.Errorf("the section ran into the next version: %q", got)
	}
	if strings.Contains(got, "2026-08-27") {
		t.Errorf("the heading line should not be printed; goreleaser renders a title above it: %q", got)
	}
	if strings.HasPrefix(got, "\n") || strings.HasSuffix(got, "\n") {
		t.Errorf("the section should be trimmed, got %q", got)
	}
}

// The last version in a file is followed by the link references rather
// than by another heading, and those are not release notes.
func TestChangelogSectionStopsAtTheLinkReferences(t *testing.T) {
	got := sectionFor(sampleChangelog, "1.1.1")
	if strings.Contains(got, "https://example.test") {
		t.Errorf("the link reference block leaked into the notes: %q", got)
	}
	if !strings.Contains(got, "An earlier entry") {
		t.Errorf("the section is empty: %q", got)
	}
}

// A release whose entry was never written should fail at tag time rather
// than publish with empty notes.
func TestChangelogSectionIsEmptyForAnUnknownVersion(t *testing.T) {
	if got := sectionFor(sampleChangelog, "9.9.9"); got != "" {
		t.Errorf("an unwritten version should produce nothing, got %q", got)
	}
}

func TestReleaseNotesNeedsAVersion(t *testing.T) {
	t.Chdir(parityRepoRoot)
	var out sink
	if err := releaseNotes(&out, nil); err == nil {
		t.Error("no version argument should be an error")
	}
	const want = "no CHANGELOG.md section for version 99.0.0"
	if err := releaseNotes(&out, []string{"v99.0.0"}); err == nil || err.Error() != want {
		t.Errorf("a version with no section = %v, want %q", err, want)
	}
}

// A tag name reaches a `run:` block in the release job, and that job
// holds id-token: write. Git permits $ ( ) ` ; and {} in a ref name, so
// a tag can carry a command substitution, and ${IFS} supplies the space
// git forbids. The workflow passes the tag through the environment and
// its trigger admits none of those characters; this is the layer that
// holds if either changes.
func TestReleaseNotesRefusesATagThatIsNotAVersion(t *testing.T) {
	for _, tag := range []string{
		"v1.2.3-$(id)",
		"v1.2.3-`id`",
		"v1.2.3-a;id",
		"v1.2.3-${IFS}x",
		"v1.2.3-a b",
		"v1.2.3-a/../b",
		"v1.2.3-a|tee",
		"",
		// A prerelease identifier is never empty and holds a letter or a
		// digit (https://semver.org/#spec-item-9).
		"v1.2.3-",
		"v1.2.3-.",
		"v1.2.3--",
		"v1.2.3-rc..1",
		"v1.2.3-rc.",
		"v1.2.3-rc.-",
	} {
		var out sink
		err := releaseNotes(&out, []string{tag})
		if err == nil {
			t.Errorf("release-notes accepted %q as a version", tag)
			continue
		}
		want := fmt.Sprintf("%q is not a release version; expected vMAJOR.MINOR.PATCH, or vMAJOR.MINOR.PATCH-PRERELEASE "+
			"with PRERELEASE dot-separated identifiers of letters, digits and hyphens, each holding a letter or a digit", tag)
		if err.Error() != want {
			t.Errorf("release-notes rejected %q as %q, want %q", tag, err, want)
		}
	}
}

// And it still accepts the shapes a real release uses. These reach the
// changelog lookup, which is where an unknown version legitimately
// fails — so the only thing asserted is that they get that far.
func TestReleaseNotesAcceptsRealVersionShapes(t *testing.T) {
	for _, tag := range []string{"v1.1.2", "1.1.2", "v2.0.0-rc1", "v2.0.0-beta.1", "v1.2.3-rc-1", "v1.2.3-rc.1", "v1.2.3-0.x-y"} {
		var out sink
		if err := releaseNotes(&out, []string{tag}); err != nil &&
			strings.Contains(err.Error(), "not a release version") {
			t.Errorf("release-notes rejected the legitimate version %q", tag)
		}
	}
}

// GitHub renders the tag name as the release page's h1, so an unaltered
// `### Added` lands as an h3 and skips a rank. The changelog itself is
// correct — `###` sits under its own `## [1.2.3]` — so the shift belongs
// here, on the way out, and nowhere else.
func TestReleaseNotesPromotesHeadingsOneRank(t *testing.T) {
	got := promoteHeadings("### Added\n- a thing\n\n### Fixed\n- another")
	if strings.Contains(got, "###") {
		t.Errorf("a heading was left at h3:\n%s", got)
	}
	for _, want := range []string{"## Added", "## Fixed"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// h2 must not become h1: that would collide with the tag name.
	if strings.Contains(promoteHeadings("## Already"), "\n# ") ||
		strings.HasPrefix(promoteHeadings("## Already"), "# ") {
		t.Error("an h2 was promoted to h1, which collides with the tag heading")
	}
}

// A `###` inside a fenced block is content, not a heading.
func TestReleaseNotesLeavesFencedContentAlone(t *testing.T) {
	in := "### Added\n- a thing\n\n```sh\n### not a heading\n```\n"
	got := promoteHeadings(in)
	if !strings.Contains(got, "## Added") {
		t.Error("the real heading was not promoted")
	}
	if !strings.Contains(got, "### not a heading") {
		t.Errorf("content inside a fence was rewritten:\n%s", got)
	}
}

// A section ends at the next heading of its level or above, or at the
// compare-link footer: Unreleased's or a version's definition, with or
// without a v, the label in any case. Any other link definition is prose.
func TestASectionEndsOnlyAtTheLinkFooter(t *testing.T) {
	const head = "## [1.0.0] - 2026-09-13\n\n- The thing.[^1]\n"
	for _, tc := range []struct {
		name, tail, want string
	}{
		{"a footnote", "\n[^1]: Only on Linux.\n", "- The thing.[^1]\n\n[^1]: Only on Linux."},
		{"a definition in the first column", "\n[1.0.0]: https://example.com/v1.0.0\n", "- The thing.[^1]"},
		{"a version's definition with its URL not yet written", "\n[1.0.0]:\n", "- The thing.[^1]"},
		{"a version's definition with a v", "\n[v1.0.0]: https://example.com/v1.0.0\n", "- The thing.[^1]"},
		{"a version's definition with a capital V", "\n[V1.0.0]: https://example.com/v1.0.0\n", "- The thing.[^1]"},
		{"a prerelease's definition with a v", "\n[v1.1.0-rc.1]: https://example.com/v1.1.0-rc.1\n", "- The thing.[^1]"},
		{"Unreleased's definition", "\n[Unreleased]: https://example.com/compare/v1.0.0...HEAD\n", "- The thing.[^1]"},
		{
			"Unreleased's definition in lower case", "\n[unreleased]: https://example.com/compare/v1.0.0...HEAD\n",
			"- The thing.[^1]",
		},
		{
			"Unreleased's definition in upper case", "\n[UNRELEASED]: https://example.com/compare/v1.0.0...HEAD\n",
			"- The thing.[^1]",
		},
		{
			"a definition continuing a list item", "  [1]: https://example.com/issue/1\n",
			"- The thing.[^1]\n  [1]: https://example.com/issue/1",
		},
		{
			"a version's definition continuing a list item", "  [v1.0.0]: https://example.com/v1.0.0\n",
			"- The thing.[^1]\n  [v1.0.0]: https://example.com/v1.0.0",
		},
		{
			"a definition of another label", "\n[issue]: https://example.com/issue/1\n",
			"- The thing.[^1]\n\n[issue]: https://example.com/issue/1",
		},
		{
			"a definition of a label that is no version", "\n[v1]: https://example.com/v1\n",
			"- The thing.[^1]\n\n[v1]: https://example.com/v1",
		},
		{
			"a definition of a label that only starts as Unreleased", "\n[unreleased-notes]: https://example.com/notes\n",
			"- The thing.[^1]\n\n[unreleased-notes]: https://example.com/notes",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sectionFor(head+tc.tail, "1.0.0"); got != tc.want {
				t.Errorf("section = %q, want %q", got, tc.want)
			}
		})
	}
}

// The one CHANGELOG reader: CRLF read as LF, a heading or a footer in a
// fence is an example, a tab after ## starts a heading, and a bullet in a
// fence is no entry.
func TestReadChangelog(t *testing.T) {
	doc := readChangelog(strings.ReplaceAll("# Changelog\n\n## [Unreleased]\n\n- a\n### [8.8.8]\n\n~~~markdown\n- in a fence\n## [9.9.9]\n"+
		"[9.9.9]: https://example.com/x\n~~~\n\n- b\n\n## [2.0.0-rc.1] - 2026-01-01\n\n- c\n##\tLater\n- d\n\n"+
		"[Unreleased]: https://example.com/u\n[2.0.0-rc.1]:\n", "\n", "\r\n"))
	if want := []changelogHeading{{"Unreleased", 2}, {"2.0.0-rc.1", 15}}; !slices.Equal(doc.headings, want) {
		t.Errorf("headings = %v, want %v", doc.headings, want)
	}
	if want := []changelogHeading{{"2.0.0-rc.1", 15}}; !slices.Equal(doc.versions(), want) {
		t.Errorf("versions = %v, want %v", doc.versions(), want)
	}
	if from, to := readChangelog("## [1.0.0]\n- a\n# Notes\n- b\n").section("1.0.0"); from != 1 || to != 2 {
		t.Errorf("a section ends at an h1 too: lines [%d, %d), want [1, 2)", from, to)
	}
	// A fence closes only at a line of its own character, as many or more,
	// and a # with no blank after it starts no heading.
	inner := readChangelog("##\t[1.0.0]\n````\n```bash\n## [9.9.9]\n```\n````\n#hashtag\n####### seven\n- a\n")
	if want := []changelogHeading{{"1.0.0", 0}}; !slices.Equal(inner.versions(), want) {
		t.Errorf("versions = %v, want %v: a tab after ## starts a heading", inner.versions(), want)
	}
	if from, to := inner.section("1.0.0"); from != 1 || to != 10 {
		t.Errorf("section 1.0.0 = lines [%d, %d), want [1, 10)", from, to)
	}
	for label, want := range map[string]string{
		"Unreleased": "\n- a\n### [8.8.8]\n\n~~~markdown\n- in a fence\n## [9.9.9]\n[9.9.9]: https://example.com/x\n~~~\n\n- b\n",
		"2.0.0-rc.1": "\n- c",
		"9.9.9":      "",
	} {
		if from, to := doc.section(label); strings.Join(doc.lines[from:to], "\n") != want {
			t.Errorf("section %s = %q, want %q", label, strings.Join(doc.lines[from:to], "\n"), want)
		}
	}
	if got := doc.entries("Unreleased"); got != 2 {
		t.Errorf("entries(Unreleased) = %d, want 2: a bullet in a fence is an example", got)
	}
}

// The release note is the section whole: a heading in a fence cuts
// nothing, and only the headings outside it are lifted.
func TestReleaseNotesReadTheFence(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, changelogPath, "## [Unreleased]\n\n## [1.0.0]\n\n### Added\n\n- a\n\n~~~bash\n## not a heading\n### nor this\n~~~\n\n- b\n\n[1.0.0]: https://example.com\n")
	t.Chdir(dir)
	var out bytes.Buffer
	if err := releaseNotes(&out, []string{"v1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if want := "## Added\n\n- a\n\n~~~bash\n## not a heading\n### nor this\n~~~\n\n- b\n"; out.String() != want {
		t.Errorf("notes = %q, want %q", out.String(), want)
	}
}

func TestChangelogCompareOrdersAsSemver(t *testing.T) {
	chain := []string{
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2",
		"1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.9.0", "1.10.0", "10.0.0",
	}
	for i := range chain {
		for j := range chain {
			if got, want := changelogCompare(chain[i], chain[j]), cmp.Compare(i, j); got != want {
				t.Errorf("changelogCompare(%s, %s) = %d, want %d", chain[i], chain[j], got, want)
			}
		}
	}
}

// Release tags are ordered by semver precedence, a prerelease before its
// release and numbers by value, and a tag that names no release is none.
func TestReleasesNewestFirst(t *testing.T) {
	for _, tc := range []struct{ tags, want []string }{
		{[]string{"v1.9.0", "v2.0.0-rc.1"}, []string{"v2.0.0-rc.1", "v1.9.0"}},
		{[]string{"v2.0.0-rc.1", "v2.0.0", "v1.10.0"}, []string{"v2.0.0", "v2.0.0-rc.1", "v1.10.0"}},
		{[]string{"v1.9.0", "v1.10.0", "latest", "v11", "2.0.0"}, []string{"v1.10.0", "v1.9.0"}},
		{[]string{"v2.0.0-rc.2", "v2.0.0-rc.10"}, []string{"v2.0.0-rc.10", "v2.0.0-rc.2"}},
		// A prerelease identifier may hold a hyphen (https://semver.org/#spec-item-9).
		{[]string{"v1.9.0", "v2.0.0-rc-1"}, []string{"v2.0.0-rc-1", "v1.9.0"}},
		{[]string{"latest", "v1.2.3--"}, nil},
	} {
		if got := releasesNewestFirst(tc.tags); !slices.Equal(got, tc.want) {
			t.Errorf("releasesNewestFirst(%q) = %q, want %q", tc.tags, got, tc.want)
		}
	}
}

// The release schema-diff, schema-ack and staleness measure from is the
// newest vX.Y.Z tag by semver that the revision reaches: not a nearer tag
// that names no release, not a newer release on another line, and not the
// release a merge reaches first.
func TestLastTagIsOneTheRevisionReaches(t *testing.T) {
	initRepo(t)
	empty := func(message string) { mustGit(t, "commit", "--quiet", "--allow-empty", "-m", message) }
	empty("Start")
	mustGit(t, "branch", "before-any-release")
	empty("Release")
	mustGit(t, "tag", "v1.0.0")
	mustGit(t, "switch", "--quiet", "-c", "maint")
	empty("Fix")
	mustGit(t, "tag", "v1.0.1")
	mustGit(t, "switch", "--quiet", "main")
	empty("Next")
	mustGit(t, "tag", "v2.0.0")
	// main moves on before the maintenance release merges in, so the merge
	// is nearer v1.0.1 than v2.0.0.
	for _, m := range []string{"More", "And more", "And still more"} {
		empty(m)
	}
	mustGit(t, "switch", "--quiet", "-c", "merged")
	mustGit(t, "merge", "--quiet", "--no-ff", "-m", "Merge maint", "maint")
	// git's version sort and the tags' names both put v3.0.0-rc.1 after
	// v3.0.0; semver puts it before.
	mustGit(t, "switch", "--quiet", "-c", "next", "main")
	empty("Candidate")
	mustGit(t, "tag", "v3.0.0-rc.1")
	mustGit(t, "branch", "candidate")
	empty("Final")
	mustGit(t, "tag", "v3.0.0")
	mustGit(t, "switch", "--quiet", "main")
	empty("Weekly")
	mustGit(t, "tag", "weekly")
	for rev, want := range map[string]string{
		"main":               "v2.0.0",
		"maint":              "v1.0.1",
		"merged":             "v2.0.0",
		"candidate":          "v3.0.0-rc.1",
		"next":               "v3.0.0",
		"before-any-release": "",
	} {
		if got, err := lastTagOn(".", rev); err != nil || got != want {
			t.Errorf("lastTagOn(%s) = %q, %v; want %q", rev, got, err, want)
		}
	}
}
