package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

// changelogPath is the changelog, from the module root.
const changelogPath = "CHANGELOG.md"

// releasableVersion is the shape a tag has to take to reach this
// command (D-85). It is the third of three places holding the same rule,
// and the only one that survives an edit to the workflow.
//
// The reason is not hygiene. This argument is a git ref name; git
// permits `$`, `(`, `)`, backtick, `;` and `{}` in one, and `${IFS}`
// supplies the space git forbids. The release job it runs in holds
// `id-token: write`, so a tag carrying a command substitution is a tag
// that can mint this repository's Sigstore identity and have a tampered
// artifact signed under it — after which every verification step README
// documents passes. The workflow passes the tag through the environment
// and its trigger admits none of those characters; this refuses them
// even if both change, since a version's own are digits and dots and its
// prerelease's letters, digits, dots and hyphens.
// https://git-scm.com/docs/git-check-ref-format
// https://docs.github.com/en/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks
var releasableVersion = regexp.MustCompile(`^v?` + semverPattern + `$`)

// releaseNotes prints one version's CHANGELOG section, which the release
// workflow passes to GoReleaser as the release-notes header. The
// published release then leads with the hand-written summary and keeps
// the generated pull-request list underneath.
//
// The heading itself is not printed: GoReleaser renders this above the
// release title, and a second heading there competes with it.
//
// A version with no section is an error rather than an empty string. A
// release whose changelog entry was never written should fail loudly at
// tag time, not publish with empty notes.
func releaseNotes(w io.Writer, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: gates release-notes VERSION")
	}
	if !releasableVersion.MatchString(args[0]) {
		return fmt.Errorf("%q is not a release version; expected vMAJOR.MINOR.PATCH, or vMAJOR.MINOR.PATCH-PRERELEASE "+
			"with PRERELEASE dot-separated identifiers of letters, digits and hyphens, each holding a letter or a digit", args[0])
	}
	data, err := os.ReadFile(changelogPath)
	if err != nil {
		return err
	}
	version := strings.TrimPrefix(args[0], "v")
	section := sectionFor(string(data), version)
	if section == "" {
		return fmt.Errorf("no CHANGELOG.md section for version %s", version)
	}
	_, err = fmt.Fprintln(w, promoteHeadings(section))
	return err
}

// promoteHeadings lifts each section heading one rank.
//
// GitHub renders the tag name as the page's h1, so a changelog's `###
// Added` lands as an h3 under it and skips h2 — which screen readers
// announce as a missing level. The changelog itself is right: `###` is
// correct under its own `## [1.2.3]`. Only the release page differs.
//
// Fences are read as the docs gate reads them, because a `###` inside a
// code block is content.
func promoteHeadings(section string) string {
	lines := strings.Split(section, "\n")
	var fence docsFence
	for i, line := range lines {
		if !fence.code(line) && docsHeadingLevel(line) >= 3 {
			lines[i] = line[1:]
		}
	}
	return strings.Join(lines, "\n")
}

// sectionFor is one version's body from a Keep a Changelog document,
// without the heading line, which carries the date, and without the
// blank lines that top and tail it.
func sectionFor(changelog, version string) string {
	doc := readChangelog(changelog)
	from, to := doc.section(version)
	return strings.Trim(strings.Join(doc.lines[from:to], "\n"), "\n")
}

// semverPattern is a version's numbers and a prerelease: dot-separated
// identifiers of letters, digits and hyphens, none empty
// (https://semver.org/#spec-item-9). semver admits an identifier of
// hyphens alone; this does not, since no release writes one.
const semverPattern = `\d+\.\d+\.\d+(?:-` + semverIdentifier + `(?:\.` + semverIdentifier + `)*)?`

// semverIdentifier is one prerelease identifier holding a letter or a digit.
const semverIdentifier = `[0-9A-Za-z-]*[0-9A-Za-z][0-9A-Za-z-]*`

var (
	// changelogVersion is a release's version, a prerelease suffix allowed.
	changelogVersion = regexp.MustCompile(`^` + semverPattern + `$`)
	// changelogFooter is a compare-link footer line: Unreleased's or a
	// version's, with or without a v, its URL written or not, the label in
	// any case, as a label matches (https://spec.commonmark.org/0.31.2/#matches).
	// Any other link definition sits in a section's prose.
	changelogFooter = regexp.MustCompile(`^\[(?i:unreleased|v?` + semverPattern + `)\]:(?:[ \t]|$)`)
)

// changelogDoc is a CHANGELOG read once, fenced code as an example: a
// heading or a link reference inside a fence is neither. Every reader of
// the CHANGELOG reads it through this, so the release note, the docs gate
// and staleness agree on where a section ends.
type changelogDoc struct {
	// lines are the file's lines, CRLF read as LF, and prose the same
	// lines with fenced code blanked.
	lines, prose []string
	// headings are the `## [label]` headings, in file order.
	headings []changelogHeading
}

// changelogHeading is a `## [label]` heading and the index of its line.
type changelogHeading struct {
	label string
	line  int
}

func readChangelog(text string) changelogDoc {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	c := changelogDoc{lines: strings.Split(text, "\n"), prose: docsProse(text)}
	for i, line := range c.prose {
		if label, ok := changelogLabel(line); ok {
			c.headings = append(c.headings, changelogHeading{label, i})
		}
	}
	return c
}

// changelogLabel is the label of a `## [label]` heading line.
func changelogLabel(line string) (string, bool) {
	if docsHeadingLevel(line) != 2 {
		return "", false
	}
	title := docsATXHeading.FindStringSubmatch(line)[1]
	label, _, ok := strings.Cut(strings.TrimPrefix(title, "["), "]")
	return label, ok && strings.HasPrefix(title, "[")
}

// section is the range of lines under `## [label]`, empty when there is
// no such heading. It ends at the next heading of its level or above and
// at the compare-link footer, which follows the oldest section with no
// heading in between.
func (c changelogDoc) section(label string) (from, to int) {
	i := slices.IndexFunc(c.headings, func(h changelogHeading) bool { return h.label == label })
	if i < 0 {
		return 0, 0
	}
	from = c.headings[i].line + 1
	for to = from; to < len(c.prose); to++ {
		if level := docsHeadingLevel(c.prose[to]); (level > 0 && level <= 2) || changelogFooter.MatchString(c.prose[to]) {
			break
		}
	}
	return from, to
}

// versions are the headings that name a release, in file order.
func (c changelogDoc) versions() []changelogHeading {
	var out []changelogHeading
	for _, h := range c.headings {
		if changelogVersion.MatchString(h.label) {
			out = append(out, h)
		}
	}
	return out
}

// changelogCompare orders two versions changelogVersion matches by semver
// precedence: by number, a prerelease before its release, and prerelease
// fields compared in turn, numbers below words (https://semver.org/#spec-item-11).
func changelogCompare(a, b string) int {
	coreA, preA, _ := strings.Cut(a, "-")
	coreB, preB, _ := strings.Cut(b, "-")
	if c := slices.CompareFunc(strings.Split(coreA, "."), strings.Split(coreB, "."), changelogCompareNumbers); c != 0 {
		return c
	}
	switch {
	case preA == preB:
		return 0
	case preA == "":
		return 1
	case preB == "":
		return -1
	}
	return slices.CompareFunc(strings.Split(preA, "."), strings.Split(preB, "."), func(x, y string) int {
		xNum, yNum := changelogDigits(x), changelogDigits(y)
		switch {
		case xNum && yNum:
			return changelogCompareNumbers(x, y)
		case xNum:
			return -1
		case yNum:
			return 1
		}
		return strings.Compare(x, y)
	})
}

// changelogCompareNumbers orders two runs of digits by the number they
// write, however long. Semver allows no leading zero.
func changelogCompareNumbers(x, y string) int {
	return cmp.Or(cmp.Compare(len(x), len(y)), strings.Compare(x, y))
}

// changelogDigits reports whether a prerelease field is a number.
func changelogDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// releaseTags is every tag of the repository at root, as `git tag` lists
// them.
func releaseTags(root string) ([]string, error) {
	out, err := git("-C", root, "tag")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// releasesNewestFirst is the release tags among tags, vX.Y.Z with a
// prerelease or not, newest first by semver. git's version sort puts
// v2.0.0-rc.1 after v2.0.0, so the order is semver's.
func releasesNewestFirst(tags []string) []string {
	var out []string
	for _, tag := range tags {
		if v, ok := strings.CutPrefix(tag, "v"); ok && changelogVersion.MatchString(v) {
			out = append(out, tag)
		}
	}
	slices.SortStableFunc(out, func(a, b string) int { return changelogCompare(b[1:], a[1:]) })
	return out
}

// lastTagOn is the newest release tag, vX.Y.Z by semver, that rev reaches
// in the repository at root, or "" when it reaches none: no tag yet, or
// only tags outside its history, as a fork that fetched upstream's has.
// Staleness, schema-diff and schema-ack all measure from it. Any other
// failure is an error: a tag that could not be read is not a release that
// does not exist.
func lastTagOn(root, rev string) (string, error) {
	tags, err := releaseTags(root)
	if err != nil {
		return "", err
	}
	return newestReleaseOn(root, rev, tags)
}

// newestReleaseOn is lastTagOn over tags already listed: the newest
// release among them whose commit rev reaches. merge-base exits 1 for a
// commit rev does not reach and otherwise for an error
// (https://git-scm.com/docs/git-merge-base, --is-ancestor).
func newestReleaseOn(root, rev string, tags []string) (string, error) {
	for _, tag := range releasesNewestFirst(tags) {
		_, err := git("-C", root, "merge-base", "--is-ancestor", "refs/tags/"+tag, rev)
		var exit *exec.ExitError
		switch {
		case err == nil:
			return tag, nil
		case !errors.As(err, &exit) || exit.ExitCode() != 1:
			return "", err
		}
	}
	return "", nil
}

// entries counts a section's "- " bullets at the margin, outside fenced
// code. Only such a bullet is an entry: an indented one belongs to the
// entry or the prose above it.
func (c changelogDoc) entries(label string) int {
	from, to := c.section(label)
	n := 0
	for _, line := range c.prose[from:to] {
		if strings.HasPrefix(line, "- ") {
			n++
		}
	}
	return n
}
