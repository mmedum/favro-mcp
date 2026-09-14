package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A block reported by several test binaries counts once. Without the
// de-duplication a package linked into three binaries reads as three
// times its real statement count, and the floor becomes an average
// wearing a floor's name.
func TestReadProfileCountsEachBlockOnce(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "cov.out")
	body := strings.Join([]string{
		"mode: atomic",
		"example.test/m/internal/a/f.go:1.1,2.2 4 1",
		"example.test/m/internal/a/f.go:1.1,2.2 4 0",
		"example.test/m/internal/a/g.go:1.1,2.2 6 0",
		"example.test/m/internal/b/h.go:1.1,2.2 2 3",
	}, "\n") + "\n"
	if err := os.WriteFile(profile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := readProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.percent("example.test/m/internal/a/"); got != 40 {
		t.Errorf("percent = %.1f, want 40 (4 of 10 statements, the repeated block counted once)", got)
	}
	if got := s.percent("example.test/m/internal/b/"); got != 100 {
		t.Errorf("percent = %.1f, want 100", got)
	}
	if got := s.percent("example.test/m/internal/nosuch/"); got != 0 {
		t.Errorf("a package with no blocks is 0, got %.1f", got)
	}
}

// A profile from a run that covered nothing must not read as a clean
// one.
func TestReadProfileRefusesAnEmptyRun(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "cov.out")
	if err := os.WriteFile(profile, []byte("mode: atomic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readProfile(profile); err == nil {
		t.Error("a profile with no blocks should fail; it is indistinguishable from a passing one otherwise")
	}
	if _, err := readProfile(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("a missing profile should fail")
	}
}

// An exemption naming a package the list does not contain reads as a
// considered exemption and exempts nothing.
func TestEveryExemptionNamesARealPackage(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	module, err := moduleName()
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := internalPackages(module, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) < 4 {
		t.Fatalf("go list returned %d packages; this check is not reading them", len(pkgs))
	}
	for pkg, why := range exemptFromFloor {
		if !slices.Contains(pkgs, pkg) {
			t.Errorf("exemptFromFloor has %q (%s) and no such package exists", pkg, why)
		}
	}
}
