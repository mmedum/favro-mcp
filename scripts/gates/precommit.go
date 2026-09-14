package main

import (
	"fmt"
	"io"
)

// precommit is what the git hook runs: the checks fast enough for every
// commit that catch the things which are expensive to undo.
//
// The leak scan is here rather than only in CI because a leak reaches
// the history at commit time, and one deleted from the tip is still in
// the log. Point git at the hook with `make hooks`.
func precommit(w io.Writer, _ []string) error {
	for _, s := range precommitSteps() {
		_, _ = fmt.Fprintf(w, "-- %s\n", s.name)
		if err := s.run(w); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
	}
	return nil
}

// step is one thing the hook runs.
type step struct {
	name string
	run  func(io.Writer) error
}

// precommitSteps is the list, separated from the loop so a test can read
// it without running a vet pass over the whole module.
//
// Each entry calls the same implementation `make check` and CI call,
// rather than restating it: the gofmt rule had three spellings in two
// languages before this, and nothing held them together.
func precommitSteps() []step {
	return []step{
		{"gofmt", func(w io.Writer) error { return fmtCheck(w, nil) }},
		{"go vet", func(io.Writer) error { return runCmd("go", "vet", "./...") }},
		{"leaks", func(w io.Writer) error { return leaks(w, nil) }},
	}
}
