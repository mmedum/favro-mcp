//go:build evals

package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Result is one task, scored on both halves.
//
// Both, because either alone is misleading. A model can leave the right
// end state having guessed a name, been refused, and got there on the
// second try — a pass by outcome and a failure by every rule this
// server is built on. And a clean trace proves nothing if the work is
// not there.
type Result struct {
	Task         string
	EndState     error
	Trace        error
	Calls        int
	MaxCalls     int
	CostUSD      float64
	Answer       string
	Unverifiable string
	Ran          error
}

func (r Result) ok() bool {
	return r.Ran == nil && r.EndState == nil && r.Trace == nil && r.Calls <= r.MaxCalls
}

// args renders a call's arguments compactly, longest-lived keys first,
// so a trace reads as a sequence rather than a wall of JSON.
func args(m map[string]any) string {
	if len(m) == 0 {
		return "()"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, clip(fmt.Sprint(m[k]), 34)))
	}
	return "(" + strings.Join(parts, " ") + ")"
}

func score(h Harness, task Task, run *Run) Result {
	res := Result{
		Task:         task.Name,
		Calls:        len(run.Calls),
		MaxCalls:     task.MaxCalls,
		CostUSD:      run.CostUSD,
		Answer:       run.Answer,
		Unverifiable: task.Unverifiable,
		Ran:          run.Err,
	}
	if run.Err != nil {
		return res
	}
	if task.EndState != nil {
		res.EndState = task.EndState(h, run)
	}
	if task.Trace != nil {
		res.Trace = task.Trace(run)
	}

	mark := "ok"
	if !res.ok() {
		mark = "FAIL"
	}
	fmt.Printf("  %-4s %d call(s), $%.3f\n", mark, res.Calls, res.CostUSD)
	if res.EndState != nil {
		fmt.Printf("       end state: %v\n", res.EndState)
	}
	if res.Trace != nil {
		fmt.Printf("       trace: %v\n", res.Trace)
	}
	if res.Calls > res.MaxCalls {
		fmt.Printf("       took %d calls, budget %d — it got there by flailing\n", res.Calls, res.MaxCalls)
	}
	// The trace is the finding. A failure says the surface is hard to
	// use; only the sequence says HOW, and reading it is the whole
	// point of running this rather than a unit test.
	if !res.ok() {
		for i, c := range run.Calls {
			fmt.Printf("       %2d. %s %s\n", i+1, c.Tool, args(c.Args))
		}
		if run.Answer != "" {
			fmt.Printf("       answered: %s\n", clip(run.Answer, 160))
		}
	}
	return res
}

// report prints the run and decides the exit code.
//
// Unverifiable halves are printed rather than hidden: a task that says
// which half went unchecked is worth more than one that quietly checks
// nothing, and that sentence is the only warning a reader gets.
func report(results []Result) error {
	fmt.Printf("\n%s\n", strings.Repeat("─", 60))
	passed, spent := 0, 0.0
	for _, r := range results {
		spent += r.CostUSD
		if r.ok() {
			passed++
		}
	}
	for _, r := range results {
		mark := "ok  "
		if !r.ok() {
			mark = "FAIL"
		}
		fmt.Printf("%s %-24s %d/%d calls  $%.3f\n", mark, r.Task, r.Calls, r.MaxCalls, r.CostUSD)
		if r.Unverifiable != "" {
			fmt.Printf("     unchecked: %s\n", r.Unverifiable)
		}
	}
	fmt.Printf("\n%d of %d passed, $%.2f spent\n", passed, len(results), spent)

	if passed < len(results) {
		fmt.Fprintln(os.Stderr, "\nA failure here is a claim about the SURFACE, not about the model: "+
			"a tool that works and cannot be used is what this exists to find. Read the trace before "+
			"changing a description.")
		return fmt.Errorf("%d of %d tasks failed", len(results)-passed, len(results))
	}
	return nil
}
