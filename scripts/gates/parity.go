package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var (
	// The prerequisites of the `check` target, which is what a person
	// runs and what the Makefile calls everything CI runs.
	checkTarget = regexp.MustCompile(`(?m)^check:\s*([^#\n]*)`)
	// A vet pass with a build tag, in either file.
	vetTags = regexp.MustCompile(`vet -tags=([a-z]+)`)
	// `NAME ?= value` or `NAME := value` at the top of the Makefile.
	makeAssign = regexp.MustCompile(`(?m)^([A-Z_][A-Z0-9_]*)\s*[:?]?=\s*(.*)$`)
	makeRef    = regexp.MustCompile(`\$\([A-Za-z_][A-Za-z0-9_]*\)`)
	// A gate invocation in a recipe line.
	gatesCall = regexp.MustCompile(`\./scripts/gates ([a-z-]+)`)
	// A module pinned to a version. The command is its last path
	// element — except when that element is a major-version suffix, as
	// in `gitleaks/v8@v8.30.1`, where taking it literally yields "v8".
	// That is not a false negative but something worse: "v8" appears in
	// the workflow inside the very version string this is trying to
	// match, so the comparison passed while comparing nothing.
	pinnedTool = regexp.MustCompile(`([A-Za-z0-9_-]+)(?:/v\d+)?@v\d+\.\d+\.\d+`)
)

// parity holds `make check` and the CI workflow to the same set of
// gates.
//
// The two lists live in different files and you are only ever editing
// one of them, so they drift silently. Three of the four sibling servers
// had them diverged, usually with the local list ahead — which means
// build-tagged files compiling only on a maintainer's laptop. A comment
// in a Makefile claiming "everything CI runs" is the sentence that stops
// the next person checking.
//
// The comparison is by command rather than by target name, because a
// target and the thing CI runs for it need not be spelled alike.
func parity(w io.Writer, _ []string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		return err
	}
	files, err := readWorkflows(root)
	if err != nil {
		return err
	}
	ci := ""
	for _, f := range files {
		if f.name == "ci.yml" {
			ci = f.data
		}
	}
	if ci == "" {
		return fmt.Errorf("no ci.yml among the workflows; the check is reading the wrong place")
	}

	declared := declaredGates()
	problems, vetPasses, err := parityCheck(string(makefile), ci, declared)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	_, err = fmt.Fprintf(w, "parity ok: %d gates in both, %d tagged vet passes in both\n",
		len(declared), vetPasses)
	return err
}

// declaredGates is every command the registry marks as running in both
// places, sorted.
func declaredGates() []string {
	var declared []string
	for name, c := range commands {
		if c.gate {
			declared = append(declared, name)
		}
	}
	slices.Sort(declared)
	return declared
}

// parityCheck is the rule itself, over the text of the two files rather
// than the files. A gate cannot be watched fail against this repository
// without first breaking this repository, so the reading is above and
// the rule is here, where a test can hand it a Makefile and a workflow
// that disagree.
//
// **Every prerequisite of `check`, not every gate.** The first version
// compared the gate registry against the workflow, which covered eight
// of the fifteen things `make check` runs: `vet`, `lint`, `vuln`,
// `licenses`, `secrets`, `tidy` and `fmt-check` were unheld, so deleting
// the govulncheck step from CI left this gate green while the Makefile
// went on calling `check` "everything CI runs". The list it reads now is
// the `check:` line itself, which is the same list the sentence claims.
func parityCheck(makefile, ci string, declared []string) (problems []string, vetPasses int, err error) {
	if len(declared) < 5 {
		return nil, 0, fmt.Errorf("only %d gates declared; the registry is not being read", len(declared))
	}

	m := checkTarget.FindStringSubmatch(makefile)
	if m == nil {
		return nil, 0, fmt.Errorf("no `check:` target in the Makefile; the check is reading the wrong file")
	}
	prereqs := strings.Fields(m[1])
	if len(prereqs) < 10 {
		return nil, 0, fmt.Errorf("the check target has %d prerequisites; that is not the whole list", len(prereqs))
	}

	// Both sides are read with commented-out lines removed. Matching the
	// raw text would mean a single `#` in front of a CI step — the way
	// somebody unblocks a red build — left this gate green, which is the
	// one divergence it exists to catch.
	ciCode := code(ci)
	ciRuns := runLines(ci)

	vars := makeVars(makefile)
	matched := 0
	for _, target := range prereqs {
		recipe := recipeOf(makefile, target, vars)
		if len(recipe) == 0 {
			problems = append(problems, fmt.Sprintf(
				"`make check` depends on %q and the Makefile gives it no recipe; it checks nothing", target))
			continue
		}
		for _, line := range recipe {
			sig := signature(line)
			if sig == "" {
				continue
			}
			// A gate is named by its invocation, which can only appear in
			// a `run:` step. Everything else may be satisfied by an
			// action that installs and runs the tool — `lint` is
			// golangci-lint-action, `secrets` is gitleaks-action — so
			// those are looked for anywhere in the workflow.
			haystack := ciCode
			if strings.HasPrefix(sig, "./scripts/gates ") {
				haystack = ciRuns
			}
			if !strings.Contains(haystack, sig) {
				problems = append(problems, fmt.Sprintf(
					"`make %s` runs %q and nothing in ci.yml does; `make check` claims to be what CI runs",
					target, sig))
				continue
			}
			matched++
		}
	}
	if matched < 8 {
		return nil, 0, fmt.Errorf("only %d recipe lines matched; the Makefile's shape has changed and "+
			"this check is reading almost nothing", matched)
	}

	// The registry's own claim, in the other direction: a command marked
	// as a gate has to be in both lists. Without this a gate could be
	// declared and wired nowhere.
	for _, name := range declared {
		if !strings.Contains(ciRuns, "./scripts/gates "+name) {
			problems = append(problems, fmt.Sprintf(
				"gate %q is declared in scripts/gates but no `run:` step in ci.yml runs it", name))
		}
		if !slices.Contains(prereqs, name) && !slices.Contains(prereqs, makeTargetFor(name)) {
			problems = append(problems, fmt.Sprintf(
				"gate %q is declared in scripts/gates but `make check` does not depend on it", name))
		}
	}

	// The specific divergence that started this rule elsewhere: a tagged
	// vet pass that only ever runs locally. Build tags are where a whole
	// package can stop compiling with CI perfectly green.
	local := tagSet(vetTags.FindAllStringSubmatch(code(makefile), -1))
	remote := tagSet(vetTags.FindAllStringSubmatch(ciCode, -1))
	for _, tag := range local {
		if !slices.Contains(remote, tag) {
			problems = append(problems, fmt.Sprintf(
				"`make vet` runs `-tags=%s` and no workflow does; those files compile only on a "+
					"maintainer's machine", tag))
		}
	}
	for _, tag := range remote {
		if !slices.Contains(local, tag) {
			problems = append(problems, fmt.Sprintf(
				"CI runs `vet -tags=%s` and `make vet` does not; a contributor cannot reproduce it", tag))
		}
	}
	return problems, len(local), nil
}

// makeVars reads the Makefile's simple variable assignments, so a recipe
// spelled `$(GO) run $(GITLEAKS)` can be compared with a workflow that
// spells the same thing literally.
func makeVars(makefile string) map[string]string {
	vars := map[string]string{}
	for _, m := range makeAssign.FindAllStringSubmatch(code(makefile), -1) {
		vars[m[1]] = strings.TrimSpace(m[2])
	}
	return vars
}

// expand substitutes $(NAME) until nothing changes, which is enough for
// assignments that refer to each other one level deep.
func expand(line string, vars map[string]string) string {
	for range 5 {
		next := makeRef.ReplaceAllStringFunc(line, func(ref string) string {
			name := ref[2 : len(ref)-1]
			if v, ok := vars[name]; ok {
				return v
			}
			return ref
		})
		if next == line {
			break
		}
		line = next
	}
	return line
}

// recipeOf returns a target's recipe lines, variables expanded. A recipe
// line is one that begins with a tab, which is the only thing make is
// strict about.
func recipeOf(makefile, target string, vars map[string]string) []string {
	lines := strings.Split(makefile, "\n")
	var recipe []string
	for i, line := range lines {
		if !strings.HasPrefix(line, target+":") {
			continue
		}
		for _, l := range lines[i+1:] {
			if strings.HasPrefix(l, "\t") {
				body := strings.TrimLeft(strings.TrimPrefix(l, "\t"), "@-")
				if body != "" {
					recipe = append(recipe, expand(body, vars))
				}
				continue
			}
			if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "#") {
				continue
			}
			break
		}
		break
	}
	return recipe
}

// signature reduces a recipe line to the shortest string that identifies
// what it runs, so the same job spelled differently in a workflow still
// matches: a gate by its subcommand, a pinned tool by its command name,
// anything else by its first three words.
//
// Deriving this rather than keeping a table of target-to-step names is
// the point. A table is a third list to maintain, and the two it sits
// between are exactly the lists this gate exists because nobody
// maintains.
func signature(line string) string {
	if m := gatesCall.FindStringSubmatch(line); m != nil {
		return "./scripts/gates " + m[1]
	}
	if m := pinnedTool.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	if len(fields) > 3 {
		fields = fields[:3]
	}
	return strings.Join(fields, " ")
}

// runLines keeps the lines of a workflow that actually run something: a
// `run:` line, and the body of a `run: |` block, which is every line
// indented past the `run:` that opened it. Requiring the command on the
// `run:` line itself fails closed — nothing slips through — but the
// first multi-line step somebody writes would report a gate CI plainly
// runs as missing.
func runLines(ci string) string {
	var kept []string
	body := -1
	for _, l := range strings.Split(code(ci), "\n") {
		indent := indentOf(l)
		if body >= 0 {
			if strings.TrimSpace(l) == "" || indent > body {
				kept = append(kept, l)
				continue
			}
			body = -1
		}
		i := strings.Index(l, "run:")
		if i < 0 {
			continue
		}
		kept = append(kept, l)
		switch strings.TrimSpace(l[i+len("run:"):]) {
		case "|", "|-", "|+", ">", ">-", ">+":
			body = indent
		}
	}
	return strings.Join(kept, "\n")
}

// makeTargetFor maps a gate to the Makefile target that runs it, for the
// ones whose names differ. Kept deliberately short: a long map here
// would mean the targets and the gates have stopped resembling each
// other, which is its own problem.
func makeTargetFor(gate string) string {
	if gate == "coverage" {
		return "cover"
	}
	return gate
}

func tagSet(matches [][]string) []string {
	var tags []string
	for _, m := range matches {
		if !slices.Contains(tags, m[1]) {
			tags = append(tags, m[1])
		}
	}
	slices.Sort(tags)
	return tags
}
