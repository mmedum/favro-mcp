//go:build evals

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// The sandbox's names. Everything a run touches is something it made,
// which is what keeps a transcript free of the organization's own data:
// the redactor removes ids and addresses, not names, so the only safe
// names are ones we chose.
const (
	sandboxPrefix = "favro-mcp evals"
	doneColumn    = "Done"
	topicWord     = "deployment"
	topicTitle    = "Deployment checklist for the release runbook"
	targetTitle   = "Eval target card"
	extraTitle    = "Unrelated note about invoicing"
	newCardTitle  = "Card the model was asked to create"
	commentBody   = "checked by the eval harness"
	missingTag    = "no-such-tag-please-refuse"
)

type harness struct {
	admin *session
	box   Sandbox
}

func (h *harness) Call(tool string, args map[string]any) (map[string]any, error) {
	return h.admin.Call(tool, args)
}
func (h *harness) Sandbox() Sandbox { return h.box }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "evals:", err)
		os.Exit(1)
	}
}

func run() error {
	bin := flag.String("bin", "./bin/favro-mcp", "the built server to drive")
	model := flag.String("model", "claude-sonnet-5", "the model to drive")
	budget := flag.Float64("budget", 0.50, "max spend per task, USD")
	only := flag.String("only", "", "run only this task")
	keep := flag.Bool("keep", false, "leave the sandbox behind for inspection")
	flag.Parse()

	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("the claude CLI is not on PATH, and it is what drives the model: %w", err)
	}
	if _, err := os.Stat(*bin); err != nil {
		return fmt.Errorf("%s: %w (run `make build` first)", *bin, err)
	}

	// The harness's own session, with the delete tools, for building
	// and tearing down the sandbox and for reading end states back.
	admin, err := start(*bin, "FAVRO_ENABLE_DESTRUCTIVE=true")
	if err != nil {
		return fmt.Errorf("starting the harness session: %w", err)
	}
	defer admin.stop()

	h := &harness{admin: admin}
	// Registered BEFORE the build, not after: the first run of this
	// failed halfway through and left a real collection behind in the
	// organization. teardown is a no-op until there is something to
	// remove, and every id is recorded the moment it exists.
	if !*keep {
		defer h.teardown()
	} else {
		defer func() { fmt.Printf("sandbox kept: collection %s\n", h.box.CollectionID) }()
	}
	fmt.Println("building the sandbox…")
	if err := h.build(); err != nil {
		return fmt.Errorf("building the sandbox: %w", err)
	}

	cfg, cleanup, err := modelConfig(*bin)
	if err != nil {
		return err
	}
	defer cleanup()

	vals := map[string]string{
		"board":      h.box.BoardName,
		"card":       targetTitle,
		"newCard":    newCardTitle,
		"column":     doneColumn,
		"comment":    commentBody,
		"tag":        missingTag,
		"topic":      topicWord,
		"topicTitle": topicTitle,
	}

	var results []Result
	for _, task := range Tasks {
		if *only != "" && task.Name != *only {
			continue
		}
		prompt, err := substitute(task.Prompt, vals)
		if err != nil {
			return fmt.Errorf("%s: %w", task.Name, err)
		}
		fmt.Printf("\n▸ %s\n", task.Name)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		r := drive(ctx, cfg, prompt, *model, *budget)
		cancel()
		r.Task, r.Vals = task.Name, vals

		results = append(results, score(h, task, r))
	}
	if len(results) == 0 {
		return fmt.Errorf("no task matched %q", *only)
	}
	return report(results)
}

// build creates the collection, the board, a Done column and the cards
// the tasks work on.
func (h *harness) build() error {
	stamp := time.Now().UTC().Format("20060102-150405")
	name := fmt.Sprintf("%s %s", sandboxPrefix, stamp)

	coll, err := h.Call("favro_create_collection", map[string]any{"name": name})
	if err != nil {
		return err
	}
	h.box.CollectionID, _ = created(coll)["collectionId"].(string)
	if h.box.CollectionID == "" {
		return fmt.Errorf("created a collection with no id")
	}

	h.box.BoardName = name + " board"
	w, err := h.Call("favro_create_widget", map[string]any{
		"collection_id": h.box.CollectionID,
		"name":          h.box.BoardName,
		// Favro rejects a widget with no type — "type is expected as
		// one of backlog, board" — though the tool schema has it
		// optional. Section 2.1: the reference and the live API differ.
		"type": "board",
	})
	if err != nil {
		return err
	}
	h.box.WidgetCommonID, _ = created(w)["widgetCommonId"].(string)
	if h.box.WidgetCommonID == "" {
		return fmt.Errorf("created a board with no widgetCommonId")
	}

	col, err := h.Call("favro_create_column", map[string]any{
		"widget_common_id": h.box.WidgetCommonID,
		"name":             doneColumn,
	})
	if err != nil {
		return err
	}
	h.box.DoneColumnID, _ = created(col)["columnId"].(string)
	h.box.DoneColumnName = doneColumn

	for _, title := range []string{targetTitle, topicTitle, extraTitle} {
		if _, err := h.Call("favro_create_card", map[string]any{
			"name":             title,
			"widget_common_id": h.box.WidgetCommonID,
		}); err != nil {
			return fmt.Errorf("seeding %q: %w", title, err)
		}
	}
	fmt.Printf("  board %q, 3 cards, column %q\n", h.box.BoardName, doneColumn)
	return nil
}

// created unwraps what a write tool returns. Every mutating tool in
// this server answers with {dry_run, result, …} rather than the bare
// resource, so reading the id off the top level silently yields "" —
// which is how the first run created a collection and then could not
// name the thing it had just made.
func created(out map[string]any) map[string]any {
	if inner, ok := out["result"].(map[string]any); ok {
		return inner
	}
	return out
}

// teardown removes everything the run made. Best effort and loud: a
// sandbox left behind is a real collection in somebody's organization.
func (h *harness) teardown() {
	if h.box.CollectionID == "" {
		return
	}
	fmt.Println("\nremoving the sandbox…")
	if h.box.WidgetCommonID != "" {
		if _, err := h.Call("favro_delete_widget", map[string]any{
			"widget_common_id": h.box.WidgetCommonID,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "  could not delete the board: %v\n", err)
		}
	}
	if _, err := h.Call("favro_delete_collection", map[string]any{
		"collection_id": h.box.CollectionID,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "  COULD NOT DELETE collection %s — remove it by hand: %v\n",
			h.box.CollectionID, err)
		return
	}
	fmt.Println("  gone")
}

// modelConfig writes the .mcp.json the model's session uses. It does
// NOT set FAVRO_ENABLE_DESTRUCTIVE: the model cannot delete anything,
// and cleanup stays with the harness.
func modelConfig(bin string) (string, func(), error) {
	abs, err := filepath.Abs(bin)
	if err != nil {
		return "", nil, err
	}
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"favro": map[string]any{"command": abs},
		},
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", nil, err
	}
	f, err := os.CreateTemp("", "favro-evals-*.json")
	if err != nil {
		return "", nil, err
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		return "", nil, err
	}
	return f.Name(), func() { _ = os.Remove(f.Name()) }, nil
}
