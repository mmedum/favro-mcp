package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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
// even if both change.
// https://git-scm.com/docs/git-check-ref-format
// https://docs.github.com/en/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks
var releasableVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[A-Za-z0-9.]+)?$`)

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
		return fmt.Errorf("%q is not a release version; expected vMAJOR.MINOR.PATCH with an "+
			"optional alphanumeric pre-release suffix", args[0])
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(root, changelogPath))
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
	code := docsCodeLines(lines)
	for i, line := range lines {
		if !code[i] && strings.HasPrefix(line, "###") {
			lines[i] = line[1:]
		}
	}
	return strings.Join(lines, "\n")
}

// sectionFor is one version's body from a Keep a Changelog document,
// without the heading line, which carries the date, and without the
// blank lines that top and tail it.
func sectionFor(changelog, version string) string {
	lines := strings.Split(changelog, "\n")
	from, to := changelogSection(lines, version)
	return strings.Trim(strings.Join(lines[from:to], "\n"), "\n")
}

// changelogSection is the range of lines under `## [version]`, empty
// when there is no such heading. It stops at the next heading and at the
// link reference block, which follows the oldest section under no
// heading of its own.
func changelogSection(lines []string, version string) (from, to int) {
	want := "## [" + version + "]"
	start := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, want) })
	if start < 0 {
		return 0, 0
	}
	to = start + 1
	for to < len(lines) && !strings.HasPrefix(lines[to], "## ") && !isLinkDefinition(lines[to]) {
		to++
	}
	return start + 1, to
}

// isLinkDefinition reports whether a line is a Markdown link definition,
// as a changelog's compare-link footer is made of. It is the docs gate's
// rule, held to the first column: an indented one continues a list item.
func isLinkDefinition(line string) bool {
	return !strings.HasPrefix(line, " ") && docsMDLinkDef.MatchString(line)
}
