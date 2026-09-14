package main

import (
	"strings"
	"testing"
)

// The hook runs the same implementations `make check` and CI run, rather
// than its own copies of them. Reading the step list is how that stays
// true without a vet pass over the whole module in a unit test.
func TestPrecommitRunsTheSharedChecks(t *testing.T) {
	steps := precommitSteps()
	if len(steps) < 3 {
		t.Fatalf("the hook runs %d steps; that is not the list", len(steps))
	}
	var names []string
	for _, s := range steps {
		names = append(names, s.name)
		if s.run == nil {
			t.Errorf("the %q step runs nothing", s.name)
		}
	}
	for _, want := range []string{"gofmt", "go vet", "leaks"} {
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
