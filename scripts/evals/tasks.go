// Command evals drives a model through this server's tools alone and
// scores what it did.
//
// The live driver proves the tools work. This proves they can be USED,
// which is a different claim and the one that fails quietly. `make live`
// calls each tool with arguments a person chose, so it can say nothing
// about whether a tool is discoverable, whether its description reads
// two ways, or whether a task takes five calls when a model will try
// two. The 1-indexed page bug is the shape of what this catches: every
// test passed, because every test either omitted `page` or asserted the
// number the code already produced.
//
// The task table lives in this file and carries NO build tag, so
// `go test ./scripts/evals` walks every prompt without credentials, a
// network or a model. That placement is deliberate: the guard against
// sending a model a literal `{board}` belongs where it costs nothing,
// not in a live run that costs ten minutes and an account.
package main

import (
	"fmt"
	"strings"
)

// Sandbox is the collection and board the run creates for itself.
//
// Everything a task touches is something this harness made, which is
// how a transcript stays free of the organization's own data — the
// redactor removes ids and addresses, not names, so the only safe names
// are names we chose. It is deleted at the end of the run.
type Sandbox struct {
	CollectionID   string
	WidgetCommonID string
	BoardName      string
	DoneColumnID   string
	DoneColumnName string
}

// Harness is what an end-state check uses to read the world back,
// through this server rather than through the model's account of it.
type Harness interface {
	Call(tool string, args map[string]any) (map[string]any, error)
	Sandbox() Sandbox
}

// Call is one tool invocation the model made.
type Call struct {
	Tool string
	Args map[string]any
}

// Run is what one task produced.
type Run struct {
	Task string
	// Vals are the substituted placeholder values, carried so a
	// scoring function reads the same title the model was given rather
	// than a second copy that can drift.
	Vals    map[string]string
	Calls   []Call
	Answer  string
	CostUSD float64
	Err     error
}

// Used reports whether the model called a tool, by bare name.
func (r *Run) Used(tool string) bool {
	for _, c := range r.Calls {
		if c.Tool == tool {
			return true
		}
	}
	return false
}

// Task is one thing a model is asked to do.
type Task struct {
	// Name is short and stable; it keys the results.
	Name string
	// Prompt is what the model is given, with {placeholders} that must
	// all be substituted before the run.
	Prompt string
	// Why says what this task is for. A task without one is a task
	// nobody can score in six months.
	Why string
	// MaxCalls fails a task that arrived by flailing. A model that took
	// nine calls to add one comment has told you something about the
	// surface even though the comment is there.
	MaxCalls int
	// EndState scores what the model left behind, read back through
	// this server. Nil when there is nothing durable to read, and then
	// Unverifiable must say so.
	EndState func(h Harness, r *Run) error
	// Trace scores how it got there.
	Trace func(r *Run) error
	// Unverifiable names the half this task cannot check. Printed with
	// the result rather than hidden: a task that says which half went
	// unchecked is worth more than one that quietly checks nothing.
	Unverifiable string
}

// Tasks is the table. Each one targets a way the surface could be
// correct and unusable.
var Tasks = []Task{
	{
		Name:     "create-card",
		Prompt:   "On the Favro board called {board}, create a card titled {newCard}. Reply with just the word done.",
		Why:      "The simplest write there is. If this needs more than a couple of calls, name resolution is the problem.",
		MaxCalls: 4,
		EndState: func(h Harness, r *Run) error {
			return cardExists(h, r.Vals["newCard"])
		},
		Trace: func(r *Run) error {
			if !r.Used("favro_create_card") {
				return fmt.Errorf("never called favro_create_card")
			}
			return nil
		},
	},
	{
		Name:     "count-cards",
		Prompt:   "How many cards are on the Favro board called {board}? Reply with just the number.",
		Why:      "Pagination, and the shape of the 1-indexed bug: a model that pages wrongly reports a plausible number that is quietly short.",
		MaxCalls: 6,
		Trace: func(r *Run) error {
			if !r.Used("favro_list_cards") && !r.Used("favro_search_cards") {
				return fmt.Errorf("never listed or searched cards")
			}
			return nil
		},
		// Counting leaves nothing behind, so the "end state" read here
		// is the board itself: the number the model reported has to be
		// the number that is actually there. This is the check that
		// would have caught the 1-indexed page bug.
		EndState: func(h Harness, r *Run) error {
			want, err := countCards(h)
			if err != nil {
				return err
			}
			got, ok := firstNumber(r.Answer)
			if !ok {
				return fmt.Errorf("no number in the answer %q", clip(r.Answer, 80))
			}
			if got != want {
				return fmt.Errorf("answered %d and the board holds %d", got, want)
			}
			return nil
		},
		Unverifiable: "nothing durable changes, so the board is re-read instead; a model that reported the count without listing would still pass the end state and fail the trace",
	},
	{
		Name:     "comment-by-name",
		Prompt:   "Add a comment saying {comment} to the Favro card titled {card} on the board {board}. Reply with just the word done.",
		Why:      "Resolution by name rather than id, which is the whole reason this server is not a thin REST wrapper.",
		MaxCalls: 5,
		EndState: func(h Harness, r *Run) error {
			return commentExists(h, r.Vals["card"], r.Vals["comment"])
		},
	},
	{
		Name:     "move-to-column",
		Prompt:   "Move the Favro card titled {card} into the {column} column on the board {board}. Reply with just the word done.",
		Why:      "Two resolutions in one task — a card and a column — and a write that depends on both.",
		MaxCalls: 6,
		EndState: func(h Harness, r *Run) error {
			return cardInColumn(h, r.Vals["card"], h.Sandbox().DoneColumnID)
		},
	},
	{
		Name:     "unknown-tag-is-refused",
		Prompt:   "Add the tag {tag} to the Favro card titled {card} on the board {board}. If that is not possible, say so and do not create anything.",
		Why:      "The tag tools hard-fail on an unknown name rather than creating it, because Favro's own addTags turns a typo into a permanent org-global tag. The question is whether the model REPORTS the refusal or papers over it.",
		MaxCalls: 6,
		Trace: func(r *Run) error {
			if r.Used("favro_create_tag") {
				return fmt.Errorf("created the tag instead of reporting that it does not exist")
			}
			return nil
		},
		EndState: func(h Harness, r *Run) error {
			if !mentionsFailure(r.Answer) {
				return fmt.Errorf("the tool refused and the model did not say so; it answered %q", clip(r.Answer, 120))
			}
			return nil
		},
	},
	{
		Name:     "find-by-topic",
		Prompt:   "Find the Favro card about {topic} on the board {board} and reply with just its exact title.",
		Why:      "Discoverability: there are two ways to find a card and the model has to pick one. A surface where it reaches for the wrong one, or for neither, is a description problem.",
		MaxCalls: 6,
		EndState: func(h Harness, r *Run) error {
			want := r.Vals["topicTitle"]
			if want == "" {
				return fmt.Errorf("the expected title did not reach the scorer")
			}
			if !strings.Contains(r.Answer, want) {
				return fmt.Errorf("answered %q, and the card is titled %q", clip(r.Answer, 80), want)
			}
			return cardExists(h, want)
		},
		Unverifiable: "finding a card leaves nothing behind, so the answer is scored against the seeded title instead",
	},
}

// cardExists reads the sandbox board back and looks for the title.
func cardExists(h Harness, title string) error {
	_, err := findCard(h, title)
	return err
}

// cardInColumn is the same read, plus where the card ended up.
func cardInColumn(h Harness, title, columnID string) error {
	card, err := findCard(h, title)
	if err != nil {
		return err
	}
	if got, _ := card["columnId"].(string); got != columnID {
		return fmt.Errorf("card %q is in column %q, not %q", title, got, columnID)
	}
	return nil
}

// commentExists looks for the text on the card, whatever the model said
// it did.
func commentExists(h Harness, title, comment string) error {
	card, err := findCard(h, title)
	if err != nil {
		return err
	}
	commonID, _ := card["cardCommonId"].(string)
	if commonID == "" {
		return fmt.Errorf("card %q carries no cardCommonId", title)
	}
	out, err := h.Call("favro_list_comments", map[string]any{"card_common_id": commonID})
	if err != nil {
		return fmt.Errorf("reading comments back: %w", err)
	}
	for _, c := range rows(out, "comments") {
		if body, _ := c["comment"].(string); strings.Contains(body, comment) {
			return nil
		}
	}
	return fmt.Errorf("no comment on %q contains %q", title, clip(comment, 40))
}

// findCard reads every card on the sandbox board and matches the title
// exactly. Exactly, because a task that asked for one title and scores a
// prefix is scoring its own matcher.
func findCard(h Harness, title string) (map[string]any, error) {
	if title == "" {
		return nil, fmt.Errorf("no title to look for; a placeholder did not reach the scorer")
	}
	out, err := h.Call("favro_list_cards", map[string]any{
		"widget_common_id": h.Sandbox().WidgetCommonID,
	})
	if err != nil {
		return nil, fmt.Errorf("reading the board back: %w", err)
	}
	seen := 0
	for _, c := range rows(out, "cards") {
		seen++
		if name, _ := c["name"].(string); name == title {
			return c, nil
		}
	}
	return nil, fmt.Errorf("no card titled %q on the board (%d cards read)", title, seen)
}

// countCards is how many cards the sandbox board actually holds, read
// through the same pagination a caller would use.
func countCards(h Harness) (int, error) {
	total, page := 0, 1
	for {
		out, err := h.Call("favro_list_cards", map[string]any{
			"widget_common_id": h.Sandbox().WidgetCommonID,
			"page":             page,
		})
		if err != nil {
			return 0, fmt.Errorf("counting the board: %w", err)
		}
		total += len(rows(out, "cards"))
		next, ok := out["next_page"].(float64)
		if !ok || int(next) <= page {
			return total, nil
		}
		page = int(next)
		if page > 50 {
			return 0, fmt.Errorf("pagination did not terminate")
		}
	}
}

// firstNumber pulls the first run of digits out of an answer.
func firstNumber(s string) (int, bool) {
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			return atoi(s[start:i]), true
		}
	}
	if start >= 0 {
		return atoi(s[start:]), true
	}
	return 0, false
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// rows digs a list out of a tool result, tolerating either the named
// key or a bare "entities", which is what Favro's envelope calls it.
func rows(out map[string]any, key string) []map[string]any {
	for _, k := range []string{key, "entities", "items"} {
		raw, ok := out[k].([]any)
		if !ok {
			continue
		}
		list := make([]map[string]any, 0, len(raw))
		for _, r := range raw {
			if m, ok := r.(map[string]any); ok {
				list = append(list, m)
			}
		}
		return list
	}
	return nil
}

// substitute fills a prompt's placeholders and refuses one it cannot.
//
// A prompt that reaches a model still carrying braces is the failure
// this guards: a sibling's first full eval run passed two tasks while
// sending the agent a literal placeholder.
func substitute(prompt string, vals map[string]string) (string, error) {
	out := prompt
	for k, v := range vals {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	if i := strings.IndexByte(out, '{'); i >= 0 {
		if j := strings.IndexByte(out[i:], '}'); j > 0 {
			return "", fmt.Errorf("unsubstituted placeholder %s", out[i:i+j+1])
		}
	}
	return out, nil
}

// mentionsFailure asks whether an answer admits the thing did not
// happen. Deliberately generous: the claim being scored is "said so at
// all", and a stricter matcher would score prose style.
func mentionsFailure(answer string) bool {
	a := strings.ToLower(answer)
	for _, s := range []string{
		"not possible", "cannot", "can't", "could not", "couldn't",
		"does not exist", "doesn't exist", "no such", "not found", "failed", "unable",
	} {
		if strings.Contains(a, s) {
			return true
		}
	}
	return false
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	// Byte truncation splits a rune, which this repository has fixed
	// twice already; cut on a boundary.
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
