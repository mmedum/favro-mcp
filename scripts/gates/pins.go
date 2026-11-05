package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var (
	// usesLine stays on one line: a match that began on a blank line
	// above would hand stepBlock indent 0, and the step would borrow a
	// later step's version.
	usesLine    = regexp.MustCompile(`(?m)^[ \t]*-?[ \t]*uses:[ \t]*([^\s#]+)`)
	fullSHA     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	exactSemver = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

	// versionLine matches any input naming a tool's version, derived
	// from the shape of the key rather than from a list of keys. A list
	// goes stale silently: a key that can never match reads as coverage
	// and provides none.
	//
	// `go-version-file` ends in `file`, so it is not matched, and it is
	// a pin by reference anyway; a bare `go-version` would be caught,
	// which is what we would want.
	versionLine = regexp.MustCompile(`(?mi)^\s*([A-Za-z0-9_-]*(?:version|release)):\s*["']?([^"'\s#]+)["']?`)

	// A Makefile variable's value that pins a Go module: module@version,
	// and no variable reference.
	makePin = regexp.MustCompile(`^([^\s@$]+/[^\s@$]+)@([^\s@$]+)$`)
	// A module a workflow runs at a version: module@version.
	moduleAt = regexp.MustCompile(`([A-Za-z0-9._~-]+(?:/[A-Za-z0-9._~-]+)+)@([^\s"'#]+)`)
	// majorSuffix is a module path's major-version element, as in
	// gitleaks/v8.
	majorSuffix = regexp.MustCompile(`^v\d+$`)
)

// actionPins are the Makefile's modules CI installs through an action
// rather than runs at module@version: the action, and the `with:` input
// or `env:` variable on its step that names the version.
var actionPins = map[string]struct {
	action string
	key    *regexp.Regexp
}{
	"github.com/golangci/golangci-lint/v2/cmd/golangci-lint": {"golangci/golangci-lint-action", stepKey("version")},
	"github.com/zricethezav/gitleaks/v8":                     {"gitleaks/gitleaks-action", stepKey("GITLEAKS_VERSION")},
}

// stepKey matches key's value on a line of a step, under `with:` or
// `env:` alike.
func stepKey(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*["']?([^"'\s#]+)`)
}

// minMakefilePins is the fewest module pins the Makefile may hold, well
// under what it holds: a reader that matched none would agree with CI
// about nothing.
const minMakefilePins = 2

// workflowFile is one file under .github/workflows. code is the same
// text with whole-line comments removed, computed once here rather than
// three times per file by the rules that read it.
type workflowFile struct {
	name string
	data string
	code string
}

// readWorkflows reads every workflow once and holds the floor itself
// rather than leaving each rule to remember one: zero files and zero
// findings are the same output.
func readWorkflows(root string) ([]workflowFile, error) {
	dir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var files []workflowFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := filepath.Ext(e.Name()); ext != ".yml" && ext != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		files = append(files, workflowFile{name: e.Name(), data: string(data), code: code(string(data))})
	}
	if len(files) < 3 {
		return nil, fmt.Errorf("only %d workflows read; the check is not reading %s", len(files), dir)
	}
	return files, nil
}

// pins holds every action to a full commit SHA and every tool it
// installs to one exact version.
//
// GitHub's guidance is to pin each action to a full-length commit SHA,
// so a backdoor pushed to the action's repository does not reach this
// one. The tool half matters just as much and reads as complete when it
// is not: an action that fetches `latest` is a pinned wrapper around an
// unpinned dependency, which is how a green build stops being
// reproducible tomorrow.
// https://docs.github.com/en/actions/reference/security/secure-use#using-third-party-actions
func pins(w io.Writer, _ []string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	return pinsAt(root, w)
}

// pinsAt runs the pins gate over the checkout at root.
func pinsAt(root string, w io.Writer) error {
	files, err := readWorkflows(root)
	if err != nil {
		return err
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		return fmt.Errorf("read Makefile: %w", err)
	}

	var problems []string
	actions, versions := 0, 0
	for _, f := range files {
		for _, m := range usesLine.FindAllStringSubmatch(f.code, -1) {
			ref := m[1]
			if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "docker://") {
				continue // a local action carries no version of its own
			}
			actions++
			_, rev, ok := strings.Cut(ref, "@")
			if !ok || !fullSHA.MatchString(rev) {
				problems = append(problems, fmt.Sprintf(
					"%s: %s is not pinned to a full commit SHA (a tag is mutable)", f.name, ref))
			}
		}
		for _, m := range versionLine.FindAllStringSubmatch(f.code, -1) {
			versions++
			if !exactSemver.MatchString(m[2]) {
				problems = append(problems, fmt.Sprintf(
					"%s: %s: %q is not exactly one version — a range, a bare major or `latest` "+
						"lets the tool change under a pinned action", f.name, m[1], m[2]))
			}
		}
		if !pinsShell(f.data) {
			problems = append(problems, fmt.Sprintf(
				"%s: no workflow-level `defaults: run: shell: bash`; without it the Windows "+
					"runner parses a `run` line as PowerShell", f.name))
		}
	}

	// A checker that finds nothing passes for the wrong reason, and
	// either count can go to zero on its own.
	if actions < 5 {
		return fmt.Errorf("only %d action references found; the check is not reading the workflows", actions)
	}
	if versions < 2 {
		return fmt.Errorf("only %d tool versions found; the input names have probably changed", versions)
	}
	local := makefilePins(string(makefile))
	if len(local) < minMakefilePins {
		return fmt.Errorf("only %d module@version pins read from the Makefile, fewer than %d; the check is not reading them",
			len(local), minMakefilePins)
	}
	problems = append(problems, makefileAgrees(code(string(makefile)), local, files)...)
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	_, err = fmt.Fprintf(w, "pins ok: %d actions by SHA, %d tool versions exact, %d workflows pin the shell, "+
		"%d Makefile pins match the workflows\n", actions, versions, len(files), len(local))
	return err
}

// makefilePin is a Makefile variable set to a Go module at one version.
type makefilePin struct{ variable, module, version string }

// makefilePins is every `VAR ?= module@version` assignment, by variable,
// at the value make gives it: the last assignment, as makeVars reads.
func makefilePins(makefile string) []makefilePin {
	var out []makefilePin
	for variable, value := range makeVars(makefile) {
		value, _, _ = strings.Cut(value, "#")
		if m := makePin.FindStringSubmatch(strings.TrimSpace(value)); m != nil {
			out = append(out, makefilePin{variable, m[1], m[2]})
		}
	}
	slices.SortFunc(out, func(a, b makefilePin) int { return strings.Compare(a.variable, b.variable) })
	return out
}

// makefileAgrees holds each tool the Makefile pins to every version a
// workflow pins it at. Two versions means `make check` passes on a tool
// CI never runs, or fails on one it has moved past; a commit can pass
// one and fail the other, with no way to reproduce the failure locally.
// A pin no workflow makes is a tool only `make check` runs.
func makefileAgrees(mk string, local []makefilePin, files []workflowFile) []string {
	var problems []string
	for _, p := range local {
		tool := moduleTool(p.module)
		if !strings.Contains(mk, "$("+p.variable+")") {
			problems = append(problems, fmt.Sprintf("the Makefile sets %s and never runs $(%s)", p.variable, p.variable))
		}
		pinned := 0
		for _, f := range files {
			for _, v := range workflowPins(f.code, p.module) {
				pinned++
				if strings.TrimPrefix(v, "v") != strings.TrimPrefix(p.version, "v") {
					problems = append(problems, fmt.Sprintf("%s is %s in the Makefile's %s and %s in %s; "+
						"make check runs one and CI the other", tool, p.version, p.variable, v, f.name))
				}
			}
		}
		if pinned == 0 {
			problems = append(problems, fmt.Sprintf("no workflow pins %s, which the Makefile runs at $(%s)", tool, p.variable))
		}
	}
	return problems
}

// moduleTool is the name a module's tool goes by: its last path element,
// or the one before a major-version suffix, so gitleaks/v8 is gitleaks.
func moduleTool(module string) string {
	if base := path.Base(module); !majorSuffix.MatchString(base) {
		return base
	}
	return path.Base(path.Dir(module))
}

// workflowPins is every version a workflow pins module at: each
// module@version it runs, and the version key of each step that
// installs module through its action.
func workflowPins(workflow, module string) []string {
	var out []string
	for _, m := range moduleAt.FindAllStringSubmatch(workflow, -1) {
		if m[1] == module {
			out = append(out, m[2])
		}
	}
	via, ok := actionPins[module]
	if !ok {
		return out
	}
	for _, loc := range usesLine.FindAllStringSubmatchIndex(workflow, -1) {
		if ref, _, _ := strings.Cut(workflow[loc[2]:loc[3]], "@"); ref != via.action {
			continue
		}
		if m := via.key.FindStringSubmatch(stepBlock(workflow, loc[0])); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// stepBlock is the step whose `uses:` line starts at start: that line and
// every line after it indented deeper, up to the next step.
func stepBlock(data string, start int) string {
	lines := strings.Split(data[start:], "\n")
	base := indentOf(lines[0])
	var out []string
	for i, ln := range lines {
		if i > 0 && strings.TrimSpace(ln) != "" {
			in := indentOf(ln)
			if in < base || (in == base && strings.HasPrefix(strings.TrimSpace(ln), "- ")) {
				break
			}
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// pinsShell reports whether a workflow sets bash as the default shell
// for the whole file: `defaults:` at column zero, `run:` under it, and
// `shell: bash` nested *under* that.
//
// Depth is the whole point. Asking only whether both keys appear says
// yes to a `shell: bash` sitting *beside* `run:` — which is
// `defaults.shell`, not a key GitHub has, so it pins nothing while
// reading to a maintainer as though it does.
func pinsShell(data string) bool {
	lines := strings.Split(data, "\n")
	for i, line := range lines {
		if indentOf(line) != 0 || withoutComment(line) != "defaults:" {
			continue
		}
		runIndent := -1
		for _, l := range lines[i+1:] {
			key := withoutComment(l)
			if key == "" {
				continue
			}
			if indent := indentOf(l); indent == 0 || indent <= runIndent {
				break // the defaults block, or the run block inside it, ended
			} else if key == "run:" {
				runIndent = indent
			} else if runIndent >= 0 && isShellBash(key) {
				return true
			}
		}
		return false // YAML allows one `defaults:` per mapping, so there is no second chance
	}
	return false
}

// indentOf counts the leading whitespace, which is the only measure of
// depth this needs. A space and a tab each count as one.
func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " \t")) }

// withoutComment trims a trailing `#` comment and the surrounding space,
// so a block annotated the way this repository annotates things still
// reads as pinned.
func withoutComment(line string) string {
	if i := strings.Index(line, "#"); i >= 0 {
		line = line[:i]
	}
	return strings.TrimSpace(line)
}

// isShellBash accepts `shell: bash` however YAML lets it be written,
// quotes included.
func isShellBash(entry string) bool {
	name, value, ok := strings.Cut(entry, ":")
	if !ok || strings.TrimSpace(name) != "shell" {
		return false
	}
	return strings.Trim(strings.TrimSpace(value), `"'`) == "bash"
}

// code drops whole-line comments, which the Makefile and the workflows
// both spell with a leading `#`. Only whole lines: a `#` further along
// can be inside a string, and a trailing comment does not stop the
// command in front of it from running.
//
// Commenting a step out is the one divergence these gates exist to
// catch, and matching raw text would let a single `#` hide it.
func code(text string) string {
	var kept []string
	for _, l := range strings.Split(text, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n")
}
