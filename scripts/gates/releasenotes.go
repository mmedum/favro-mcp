package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

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
	_, err = fmt.Fprintln(w, section)
	return err
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
