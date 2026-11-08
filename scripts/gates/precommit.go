package main

import (
	"fmt"
	"io"
	"strings"
)

// precommit is what the git hook runs: the checks fast enough for every
// commit that catch the things which are expensive to undo.
//
// The leak and secret scans are here rather than only in CI because a
// leak reaches the history at commit time, and one deleted from the tip
// is still in the log. Point git at the hook with `make hooks`.
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
// rather than restating it, so the hook cannot drift from them.
func precommitSteps() []step {
	return []step{
		{"gofmt", func(w io.Writer) error { return fmtCheck(w, nil) }},
		{"go vet", func(io.Writer) error { return runCmd("go", "vet", "./...") }},
		{"leaks", func(w io.Writer) error { return leaks(w, nil) }},
		{"gitleaks", func(io.Writer) error {
			mk, err := readMakefile()
			if err != nil {
				return err
			}
			scan, err := precommitGitleaks(mk)
			if err != nil {
				return err
			}
			return runCmd(scan[0], scan[1:]...)
		}},
	}
}

// precommitGitleaks is the hook's secret scan: the gitleaks module@version
// and flags the Makefile's GITLEAKS and GITLEAKS_FLAGS name, the ones
// `make secrets` runs, over the staged changes. `make secrets` scans
// commits, so a credential not yet committed is caught here or not before
// it is in the history. gitleaks v8.19.0 deprecated `protect` for
// `git --staged`
// (https://github.com/gitleaks/gitleaks/blob/v8.30.1/README.md#commands,
// https://github.com/gitleaks/gitleaks/blob/v8.30.1/cmd/git.go#L17).
func precommitGitleaks(mk *makefile) ([]string, error) {
	module := mk.expand("$(GITLEAKS)")
	if !strings.Contains(module, "@") {
		return nil, fmt.Errorf("%s: GITLEAKS names no module@version, so the hook has no gitleaks to scan with", makefilePath)
	}
	if _, ok := mk.vars["GITLEAKS_FLAGS"]; !ok {
		return nil, fmt.Errorf("%s: GITLEAKS_FLAGS is not set, so the hook has no flags to scan with", makefilePath)
	}
	flags := shellTexts(shellSplit(mk.expand("$(GITLEAKS_FLAGS)")))
	return append(append([]string{"go", "run", module, "git", "--staged"}, flags...), "."), nil
}
