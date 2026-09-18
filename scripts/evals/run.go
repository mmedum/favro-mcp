//go:build evals

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func environ() []string { return os.Environ() }

// drive gives one task to a model and records what it did.
//
// The model gets this server's tools and nothing else, and the way that
// is spelled matters more than it looks. The first version of this
// harness named the host's built-ins in a disallow list, which is a
// denylist and fails open: it listed six, and the model used Grep and
// Glob — both absent — to read the maintainer's own notes about Favro's
// quirks mid-task. Naming more of them was the wrong repair. A
// hand-typed list of a surface somebody else ships is exactly what hard
// rule 14 says not to build, and re-auditing it against each CLI
// release is a job nobody will do; a later read found Monitor still
// missing, which runs a shell command under a name that is not Bash.
//
// So the built-in surface is turned off at the source instead:
// --tools "" disables all of them, and the only tools that survive are
// the MCP ones --allowed-tools names. --setting-sources= drops the
// developer's own settings, hooks, skills and pre-approved permission
// rules, which would otherwise decide what a built-in may do, and
// --strict-mcp-config keeps their other MCP servers out. What is left
// is this server's tools, which is the thing being scored.
//
// This is also the security boundary, not just a scoring one. The run
// reads a real organization, so card text written by anyone with access
// to it reaches the model, and the process tree holds a live Favro
// credential. A built-in that runs a command is an exfiltration path
// for injected card content; there must not be one.
//
// The model's server is started WITHOUT FAVRO_ENABLE_DESTRUCTIVE, so it
// cannot delete anything. Cleanup is the harness's job, on its own
// session, which is the only place the delete tools exist.
func drive(ctx context.Context, cfgPath, prompt, model string, budget float64) *Run {
	r := &Run{}
	args := []string{
		"-p", prompt,
		"--output-format", "stream-json", "--verbose",
		"--mcp-config", cfgPath,
		"--strict-mcp-config",
		"--allowed-tools", "mcp__favro__*",
		// "" is the CLI's own "disable every built-in". Derived from
		// the flag rather than from a list of names this repository
		// would have to keep in step with a surface it does not own.
		"--tools", "",
		// No user, project or local settings: the maintainer's
		// permission rules, hooks and skills are not part of what is
		// being scored, and they decide what a built-in may do.
		"--setting-sources", "",
		"--model", model,
		"--max-budget-usd", fmt.Sprintf("%.2f", budget),
		"--permission-mode", "acceptEdits",
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		r.Err = err
		return r
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		r.Err = err
		return r
	}

	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		readEvent(r, sc.Bytes())
	}
	if err := cmd.Wait(); err != nil && r.Err == nil {
		r.Err = fmt.Errorf("claude: %w (%s)", err, clip(stderr.String(), 300))
	}
	return r
}

// readEvent folds one stream-json line into the run: the tool calls the
// model made, its final answer, and what the run cost.
func readEvent(r *Run, line []byte) {
	var ev struct {
		Type    string  `json:"type"`
		Subtype string  `json:"subtype"`
		Result  string  `json:"result"`
		TotalC  float64 `json:"total_cost_usd"`
		Message struct {
			Content []struct {
				Type  string         `json:"type"`
				Name  string         `json:"name"`
				Input map[string]any `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &ev); err != nil {
		return
	}
	switch ev.Type {
	case "assistant":
		for _, c := range ev.Message.Content {
			if c.Type == "tool_use" {
				r.Calls = append(r.Calls, Call{Tool: bareTool(c.Name), Args: c.Input})
			}
		}
	case "result":
		if ev.Result != "" {
			r.Answer = ev.Result
		}
		if ev.TotalC > 0 {
			r.CostUSD = ev.TotalC
		}
		if ev.Subtype != "" && ev.Subtype != "success" && r.Err == nil {
			r.Err = fmt.Errorf("run ended as %q", ev.Subtype)
		}
	}
}

// bareTool strips the MCP prefix, so a task scores `favro_create_card`
// rather than the host's wire name for it.
func bareTool(name string) string {
	if i := strings.LastIndex(name, "__"); i >= 0 {
		return name[i+2:]
	}
	return name
}
