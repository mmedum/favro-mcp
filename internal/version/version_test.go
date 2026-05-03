package version

import (
	"strings"
	"testing"
)

// TestString_DefaultsArePresent verifies the package exposes non-empty
// metadata even when no ldflags overrides have been applied (e.g. `go test`).
func TestString_DefaultsArePresent(t *testing.T) {
	t.Parallel()

	got := String()
	if got == "" {
		t.Fatal("version.String() returned empty string")
	}
	if !strings.Contains(got, Tag) || !strings.Contains(got, Commit) {
		t.Fatalf("version.String() = %q; want it to contain Tag=%q and Commit=%q", got, Tag, Commit)
	}
}
