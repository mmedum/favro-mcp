package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mmedum/favro-mcp/internal/livecover"
)

// liveCoverWaivedPath records the tools and options the live driver
// deliberately does not exercise, and why.
const liveCoverWaivedPath = "testdata/live-cover-waived.tsv"

// liveCoverFloors: a gate comparing two empty sets prints the same
// sentence as one comparing three hundred options.
const (
	minCoverTools      = 80
	minCoverOptions    = 300
	minCoverSteps      = 80
	minCoverWaiverText = 40
)

// liveCover fails on any tool or option the live driver never
// exercises.
//
// A claim that a driver covers the surface decays every time the
// surface grows, and silently: the standard records one repository
// where the same assertion ran at 14 of 28 tools, and another where a
// later phase added 67 options with no step. So the comparison is
// against the binary's own published schema rather than against a
// number somebody wrote down, and it is per option rather than per
// tool, because a tool called once with its required arguments leaves
// most of its surface untouched.
//
// The steps come from internal/livecover, which the driver runs — so
// the gate and the driver cannot disagree about what coverage means.
func liveCover(w io.Writer, _ []string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}

	schema, err := toolOptions(filepath.Join(root, "schemas.json"))
	if err != nil {
		return err
	}
	if len(schema) < minCoverTools {
		return fmt.Errorf("read %d tools from schemas.json, expected at least %d — run `make schemas`",
			len(schema), minCoverTools)
	}

	waived, err := readLiveWaivers(filepath.Join(root, filepath.FromSlash(liveCoverWaivedPath)))
	if err != nil {
		return err
	}

	steps := livecover.Steps()
	if len(steps) < minCoverSteps {
		return fmt.Errorf("the driver has %d steps, expected at least %d", len(steps), minCoverSteps)
	}

	cov := livecover.Measure(steps, schema, waived)
	if cov.Options < minCoverOptions {
		return fmt.Errorf("compared %d options, expected at least %d", cov.Options, minCoverOptions)
	}

	var problems []string
	for _, tool := range cov.MissingTools {
		problems = append(problems, fmt.Sprintf(
			"%s is registered and no step calls it — add one to internal/livecover, or waive it in %s",
			tool, liveCoverWaivedPath))
	}
	for _, opt := range cov.MissingOptions {
		problems = append(problems, fmt.Sprintf(
			"%s is a tool option no step sets — add one, or waive it in %s", opt, liveCoverWaivedPath))
	}
	problems = append(problems, staleLiveWaivers(waived, schema, steps)...)

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("the live driver does not cover the surface:\n  %s", strings.Join(problems, "\n  "))
	}

	_, err = fmt.Fprintf(w,
		"live-cover ok: %d steps over %d tools and %d options; %d covered, %d waived with a reason\n",
		len(steps), cov.Tools, cov.Options, cov.OptionsCovered, len(waived))
	return err
}

// staleLiveWaivers reports waivers for something that is covered, or
// that no longer exists. A waiver that waives nothing reads exactly
// like a decision.
func staleLiveWaivers(waived map[string]string, schema map[string][]string, steps []livecover.Step) []string {
	live := map[string]bool{}
	for tool, options := range schema {
		live[tool] = true
		for _, o := range options {
			live[tool+"."+o] = true
		}
	}
	covered := map[string]bool{}
	for _, s := range steps {
		covered[s.Tool] = true
		for a := range s.Args {
			covered[s.Tool+"."+a] = true
		}
	}

	var problems []string
	for key, reason := range waived {
		switch {
		case !live[key]:
			problems = append(problems, fmt.Sprintf(
				"%s: %s is waived and is not on the surface any more", liveCoverWaivedPath, key))
		case covered[key]:
			problems = append(problems, fmt.Sprintf(
				"%s: %s is waived and a step exercises it — drop the waiver", liveCoverWaivedPath, key))
		case len(reason) < minCoverWaiverText:
			problems = append(problems, fmt.Sprintf(
				"%s: %s is waived in %d characters; say why", liveCoverWaivedPath, key, len(reason)))
		}
	}
	return problems
}

// toolOptions reduces the committed schema dump to tool name -> input
// property names, which is all coverage is measured against.
func toolOptions(path string) (map[string][]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var dump struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &dump); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := make(map[string][]string, len(dump.Tools))
	for _, t := range dump.Tools {
		options := make([]string, 0, len(t.InputSchema.Properties))
		for name := range t.InputSchema.Properties {
			options = append(options, name)
		}
		sort.Strings(options)
		out[t.Name] = options
	}
	return out, nil
}

// readLiveWaivers parses KEY \t REASON, where KEY is a tool name or
// "tool.option".
func readLiveWaivers(path string) (map[string]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for i, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 {
			return nil, fmt.Errorf("%s:%d: got %d tab-separated fields, want 2 (KEY REASON)",
				liveCoverWaivedPath, i+1, len(fields))
		}
		out[strings.TrimSpace(fields[0])] = strings.TrimSpace(fields[1])
	}
	return out, nil
}
