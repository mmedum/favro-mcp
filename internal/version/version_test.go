package version

import (
	"runtime/debug"
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

// The linker-stamped path, the `go install` path, and the neither path.
// Tag and Commit are package variables set at link time, so the only
// way to test a build this binary is not is to pass the inputs in —
// which is what stampFrom exists for.
func TestStampFrom(t *testing.T) {
	t.Parallel()

	buildInfo := func(version string, settings ...debug.BuildSetting) *debug.BuildInfo {
		bi := &debug.BuildInfo{Settings: settings}
		bi.Main.Version = version
		bi.Main.Path = "github.com/mmedum/favro-mcp"
		return bi
	}

	tests := []struct {
		name       string
		tag        string
		commit     string
		info       *debug.BuildInfo
		wantTag    string
		wantCommit string
		wantSource string
	}{
		{
			name: "ldflags win when the linker spoke",
			tag:  "v1.2.3", commit: "abc1234",
			info:       buildInfo("v9.9.9"),
			wantTag:    "v1.2.3",
			wantCommit: "abc1234",
			wantSource: "ldflags",
		},
		{
			// `go install` applies no ldflags, which is the defect the
			// shared standard's §10 names first: without this fallback
			// a binary installed the documented way reports
			// "dev (unknown)" and no bug report from one can be tied to
			// a build.
			name: "go install falls back to the module version",
			tag:  "dev", commit: "unknown",
			info: buildInfo("v1.1.2",
				debug.BuildSetting{Key: "vcs.revision", Value: "0123456789abcdef"},
			),
			wantTag:    "v1.1.2",
			wantCommit: "0123456",
			wantSource: "buildinfo",
		},
		{
			name: "a dirty working tree is said so",
			tag:  "dev", commit: "unknown",
			info: buildInfo("(devel)",
				debug.BuildSetting{Key: "vcs.revision", Value: "0123456789abcdef"},
				debug.BuildSetting{Key: "vcs.modified", Value: "true"},
			),
			// "(devel)" is no more informative than "dev"; the
			// placeholder stays rather than being traded for another.
			wantTag:    "dev",
			wantCommit: "0123456-dirty",
			wantSource: "buildinfo",
		},
		{
			name: "no build info at all",
			tag:  "dev", commit: "unknown",
			info: nil,
			// Nothing to fall back to, and saying so is the point:
			// "unstamped" is the difference between a report naming a
			// build and one naming nothing.
			wantTag:    "dev",
			wantCommit: "unknown",
			wantSource: "unstamped",
		},
		{
			name: "build info with neither a version nor a revision",
			tag:  "dev", commit: "unknown",
			info:       buildInfo("(devel)"),
			wantTag:    "dev",
			wantCommit: "unknown",
			wantSource: "unstamped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stampFrom(tt.tag, tt.commit, tt.info)
			if got.tag != tt.wantTag {
				t.Errorf("tag = %q; want %q", got.tag, tt.wantTag)
			}
			if got.commit != tt.wantCommit {
				t.Errorf("commit = %q; want %q", got.commit, tt.wantCommit)
			}
			if got.source != tt.wantSource {
				t.Errorf("source = %q; want %q", got.source, tt.wantSource)
			}
		})
	}
}

func TestModuleFrom(t *testing.T) {
	t.Parallel()

	if got := moduleFrom(nil); got != "unknown" {
		t.Errorf("moduleFrom(nil) = %q; want %q", got, "unknown")
	}
	bi := &debug.BuildInfo{}
	bi.Main.Path = "github.com/mmedum/favro-mcp/"
	if got := moduleFrom(bi); got != "github.com/mmedum/favro-mcp" {
		t.Errorf("moduleFrom(…) = %q; want the path without its trailing slash", got)
	}
}

// Source and Module are the exported wrappers. They read the real build,
// so the only thing worth asserting is that they answer at all — an
// empty string in a bug report is worse than "unknown".
func TestSourceAndModuleAlwaysAnswer(t *testing.T) {
	t.Parallel()

	if Source() == "" {
		t.Error("Source() is empty")
	}
	if Module() == "" {
		t.Error("Module() is empty")
	}
}
