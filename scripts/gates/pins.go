package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	usesLine    = regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*([^\s#]+)`)
	fullSHA     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	exactSemver = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

	// versionLine matches any input naming a tool's version, derived
	// from the shape of the key rather than from a list of keys. A list
	// goes stale silently: a sibling's named two keys that could never
	// match, which read as coverage and provided none.
	//
	// `go-version-file` ends in `file`, so it is not matched, and it is
	// a pin by reference anyway; a bare `go-version` would be caught,
	// which is what we would want.
	versionLine = regexp.MustCompile(`(?mi)^\s*([A-Za-z0-9_-]*(?:version|release)):\s*["']?([^"'\s#]+)["']?`)

	// The Makefile's pinned tool invocations: `go run <module>@<version>`.
	makeToolPin = regexp.MustCompile(`([A-Za-z0-9./_\-]+)@(v\d+\.\d+\.\d+[0-9A-Za-z.\-]*)`)
)

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
// GitHub's own guidance is that a full-length commit SHA is the only
// immutable way to reference an action. The tool half matters just as
// much and reads as complete when it is not: an action that fetches
// `latest` is a pinned wrapper around an unpinned dependency, which is
// how a green build stops being reproducible tomorrow.
func pins(w io.Writer, _ []string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	files, err := readWorkflows(root)
	if err != nil {
		return err
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
	if err := scannersAgree(root, files); err != nil {
		problems = append(problems, err.Error())
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	_, err = fmt.Fprintf(w, "pins ok: %d actions by SHA, %d tool versions exact, %d workflows pin the shell\n",
		actions, versions, len(files))
	return err
}

// scannersAgree keeps the local secret scan and CI's on one gitleaks.
// Two versions means a commit can pass `make secrets` and fail the job,
// on rules that exist in one build and not the other — and the person
// who hits it has no way to reproduce the failure locally.
func scannersAgree(root string, files []workflowFile) error {
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		return fmt.Errorf("read Makefile: %w", err)
	}
	local := ""
	for _, m := range makeToolPin.FindAllStringSubmatch(code(string(makefile)), -1) {
		if strings.Contains(m[1], "gitleaks") {
			local = m[2]
		}
	}
	if local == "" {
		return fmt.Errorf("no pinned gitleaks module in the Makefile; the check is reading the wrong file")
	}
	ci := ""
	for _, f := range files {
		for _, m := range versionLine.FindAllStringSubmatch(f.code, -1) {
			if strings.EqualFold(m[1], "GITLEAKS_VERSION") {
				ci = m[2]
			}
		}
	}
	if ci == "" {
		return fmt.Errorf("no GITLEAKS_VERSION in any workflow; the check is reading the wrong place")
	}
	if strings.TrimPrefix(local, "v") != strings.TrimPrefix(ci, "v") {
		return fmt.Errorf("gitleaks is %s in the Makefile and %s in CI; a commit can pass one and fail the other", local, ci)
	}
	return nil
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
