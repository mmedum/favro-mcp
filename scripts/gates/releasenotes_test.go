package main

import (
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
	got := changelogSection(sampleChangelog, "1.1.2")
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
	got := changelogSection(sampleChangelog, "1.1.1")
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
	if got := changelogSection(sampleChangelog, "9.9.9"); got != "" {
		t.Errorf("an unwritten version should produce nothing, got %q", got)
	}
}

func TestReleaseNotesNeedsAVersion(t *testing.T) {
	var out sink
	if err := releaseNotes(&out, nil); err == nil {
		t.Error("no version argument should be an error")
	}
	if err := releaseNotes(&out, []string{"v99.0.0"}); err == nil {
		t.Error("a version with no section should fail loudly")
	}
}

// A tag name reaches a `run:` block in the release job, and that job
// holds id-token: write. Git permits $ ( ) ` ; and {} in a ref name, so
// a tag can carry a command substitution, and ${IFS} supplies the space
// git forbids. The workflow passes the tag through the environment now
// and its trigger no longer admits those characters; this is the layer
// that holds if either is changed back.
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
	} {
		var out sink
		err := releaseNotes(&out, []string{tag})
		if err == nil {
			t.Errorf("release-notes accepted %q as a version", tag)
			continue
		}
		if !strings.Contains(err.Error(), "not a release version") {
			t.Errorf("release-notes rejected %q for the wrong reason: %v", tag, err)
		}
	}
}

// And it still accepts the shapes a real release uses. These reach the
// changelog lookup, which is where an unknown version legitimately
// fails — so the only thing asserted is that they get that far.
func TestReleaseNotesAcceptsRealVersionShapes(t *testing.T) {
	for _, tag := range []string{"v1.1.2", "1.1.2", "v2.0.0-rc1", "v2.0.0-beta.1"} {
		var out sink
		if err := releaseNotes(&out, []string{tag}); err != nil &&
			strings.Contains(err.Error(), "not a release version") {
			t.Errorf("release-notes rejected the legitimate version %q", tag)
		}
	}
}
