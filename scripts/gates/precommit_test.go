package main

import (
	"slices"
	"strings"
	"testing"
)

// The hook runs the same implementations `make check` and CI run, rather
// than its own copies of them. Reading the step list is how that stays
// true without a vet pass over the whole module in a unit test.
func TestPrecommitRunsTheSharedChecks(t *testing.T) {
	steps := precommitSteps()
	if len(steps) < 4 {
		t.Fatalf("the hook runs %d steps; that is not the list", len(steps))
	}
	var names []string
	for _, s := range steps {
		names = append(names, s.name)
		if s.run == nil {
			t.Errorf("the %q step runs nothing", s.name)
		}
	}
	for _, want := range []string{"gofmt", "go vet", "leaks", "gitleaks"} {
		if !strings.Contains(strings.Join(names, " "), want) {
			t.Errorf("the hook does not run %q; it is %v", want, names)
		}
	}

	// The leak scan in particular: a leak reaches the history at commit
	// time, and one deleted from the tip is still in the log — which is
	// the whole reason there is a hook rather than only a CI job.
	var out sink
	for _, s := range steps {
		if s.name != "leaks" {
			continue
		}
		if err := s.run(&out); err != nil {
			t.Fatalf("the leak step should pass on a clean tree: %v", err)
		}
		out.mustSay(t, "files scanned")
	}
}

// The hook scans the staged changes with the gitleaks module@version and
// the flags the Makefile's GITLEAKS and GITLEAKS_FLAGS name, so the
// version has one home and the pins gate holds it there.
func TestTheHookScansWithTheMakefilesGitleaks(t *testing.T) {
	const noVersion = "Makefile: GITLEAKS names no module@version, so the hook has no gitleaks to scan with"
	const noFlags = "Makefile: GITLEAKS_FLAGS is not set, so the hook has no flags to scan with"
	for _, tc := range []struct {
		name, makefile string
		want           []string
		err            string
	}{
		{
			"the Makefile's module and flags",
			"GL = github.com/zricethezav/gitleaks/v8@v8.1.0\nGITLEAKS ?= $(GL)\nGITLEAKS_FLAGS = --config x.toml --redact # why\n",
			[]string{"go", "run", "github.com/zricethezav/gitleaks/v8@v8.1.0", "git", "--staged", "--config", "x.toml", "--redact", "."},
			"",
		},
		{"no GITLEAKS", "GO ?= go\nGITLEAKS_FLAGS = --redact\n", nil, noVersion},
		{"GITLEAKS with no version", "GITLEAKS ?= github.com/zricethezav/gitleaks/v8\nGITLEAKS_FLAGS = --redact\n", nil, noVersion},
		{"no GITLEAKS_FLAGS", "GITLEAKS ?= github.com/zricethezav/gitleaks/v8@v8.1.0\n", nil, noFlags},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := precommitGitleaks(parseMakefile(tc.makefile))
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			if !slices.Equal(got, tc.want) || msg != tc.err {
				t.Errorf("precommitGitleaks() = %q, %q; want %q, %q", got, msg, tc.want, tc.err)
			}
		})
	}
}
