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

// builtins are the host's own tools, which the model must not have. The
// task is to use THIS server; anything reachable another way makes the
// result a claim about the host instead.
var builtins = []string{
	"Bash", "Read", "Write", "Edit", "NotebookEdit", "Glob", "Grep",
	"WebFetch", "WebSearch", "Task", "Agent", "TodoWrite", "ToolSearch",
	"SlashCommand", "KillShell", "BashOutput",
}

// drive gives one task to a model and records what it did.
//
// The model gets this server's tools and nothing else. --strict-mcp-config
// keeps any of the developer's own MCP servers out of the run, and the
// disallow list keeps the model off the filesystem and the shell: an
// eval that lets a model reach for Bash is scoring Bash.
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
		// Every built-in, by name. --allowed-tools does NOT exclude
		// them: the first run of this harness listed six and the model
		// used Grep and Glob to read the maintainer's own notes about
		// Favro's quirks, which is not a surface this server exposes.
		// An eval that lets a model reach outside the tools is scoring
		// something else.
		"--disallowed-tools", strings.Join(builtins, ","),
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
