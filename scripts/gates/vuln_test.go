package main

import (
	"errors"
	"io"
	"slices"
	"testing"
)

// The toolchain directive wins over the go line, as the go command and
// setup-go read it, and a go line without a patch is not a release.
func TestVulnToolchainReadsGoMod(t *testing.T) {
	for _, tc := range []struct {
		name, gomod, want string
		couldNotRun       bool
	}{
		{"the go line", "module x\n\ngo 1.27.4\n", "go1.27.4", false},
		{"the toolchain directive", "module x\n\ngo 1.27.0\n\ntoolchain go1.27.4\n", "go1.27.4", false},
		{"a language version only", "module x\n\ngo 1.27\n", "", false},
		{"no go line", "module x\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := vulnToolchain(tc.gomod)
			if got != tc.want {
				t.Errorf("vulnToolchain = %q, want %q", got, tc.want)
			}
			if tc.want == "" && err == nil {
				t.Fatal("no error for a go.mod that names no release")
			}
			if errors.As(err, new(couldNotRun)) != tc.couldNotRun {
				t.Errorf("err = %v, want couldNotRun %v", err, tc.couldNotRun)
			}
		})
	}
}

// The command takes govulncheck as the Makefile pins it, so the version
// stays where the pins gate reads it.
func TestVulnTakesThePinnedModule(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"govulncheck"},
		{"golang.org/x/vuln/cmd/govulncheck@v1.8.0", "extra"},
		{"-modfile=x/y@v1"},
	} {
		if err := vuln(io.Discard, args); !errors.As(err, new(couldNotRun)) {
			t.Errorf("vuln(%q) = %v, want a usage error", args, err)
		}
	}
}

// The pins gate reads the module the vuln gate runs as a `go run` of it,
// so the Makefile's pin and CI's are held to one version.
func TestPinsReadsTheModuleTheVulnGateRuns(t *testing.T) {
	const module = "golang.org/x/vuln/cmd/govulncheck@v1.8.0"
	got := pinsGoRuns("go run ./scripts/gates vuln " + module)
	if !slices.Contains(got, pinsGoRun{"run", module}) {
		t.Errorf("pinsGoRuns = %v, want a go run of %s", got, module)
	}
	if got := pinsGoRuns("go run ./scripts/gates docs " + module); len(got) != 0 {
		t.Errorf("pinsGoRuns of another gate = %v, want none", got)
	}
}
