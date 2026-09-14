package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// releasableVersion is the shape a tag has to take to reach this
// command. It is the third of three places holding the same rule, and
// the only one that survives an edit to the workflow.
//
// The reason is not hygiene. This argument is a git ref name; git
// permits `$`, `(`, `)`, backtick, `;` and `{}` in one, and `${IFS}`
// supplies the space git forbids. The release job it runs in holds
// `id-token: write`, so a tag carrying a command substitution is a tag
// that can mint this repository's Sigstore identity and have a tampered
// artifact signed under it — after which every verification step README
// documents passes. The workflow passes the tag through the environment
// now and its trigger no longer admits those characters; this refuses
// them even if both are changed back.
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
// tag time, not publish with empty notes — which is what happened for
// every release before the section was wired in at all.
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
	data, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		return err
	}
	version := strings.TrimPrefix(args[0], "v")
	section := changelogSection(string(data), version)
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
// Fences are tracked because a `###` inside a code block is content.
func promoteHeadings(section string) string {
	lines := strings.Split(section, "\n")
	inFence := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if !inFence && strings.HasPrefix(line, "###") {
			lines[i] = line[1:]
		}
	}
	return strings.Join(lines, "\n")
}

// changelogSection extracts one version's body from a Keep a Changelog
// document. It stops at the next heading of any level and at the link
// reference block, which sits at the bottom under no heading at all.
func changelogSection(changelog, version string) string {
	_, rest, ok := strings.Cut(changelog, "## ["+version+"]")
	if !ok {
		return ""
	}
	var body []string
	for line := range strings.Lines(rest) {
		trimmed := strings.TrimRight(line, "\n")
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]: http") {
			break
		}
		body = append(body, trimmed)
	}
	// The first line is the remainder of the heading line itself — the
	// date — and the blank lines around the body are the document's
	// formatting rather than its content.
	if len(body) > 0 {
		body = body[1:]
	}
	return strings.Trim(strings.Join(body, "\n"), "\n")
}
