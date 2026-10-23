// Command livefavro drives the built binary against a real Favro
// organization.
//
// It exists because unit tests cannot catch what §2.1 describes: Favro
// answers 200 for a body it ignored, and its reference disagrees with
// the live API in places. Every one of those disagreements this
// repository knows about was found by a call, and most of them by a
// call made after the tests were already green — a fixture written to
// match an assumption agrees with the assumption.
//
// Everything it prints goes through one *redact.Redactor, and the
// `transcript` gate fails the build if anything else reaches the
// terminal. The steps come from internal/livecover, which the
// `live-cover` gate reads too, so the claim that the driver covers the
// surface is checked against the binary's own schema rather than
// asserted here.
//
//	make build && go run ./scripts/livefavro            # every step
//	go run ./scripts/livefavro -only favro_list_cards   # one tool
//	go run ./scripts/livefavro -v                       # with results
//
// It needs real credentials, so it is never part of `make check`.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmedum/favro-mcp/v4/internal/livecover"
	"github.com/mmedum/favro-mcp/v4/internal/redact"
)

const (
	defaultBinary = "./bin/favro-mcp"
	callTimeout   = 90 * time.Second
)

// stopTimeout is how long a server gets to exit after its stdin closes,
// before it is killed. It should need none of it. A variable rather than
// a constant so the test for the kill path need not spend five seconds.
var stopTimeout = 5 * time.Second

func main() {
	bin := flag.String("bin", defaultBinary, "the built server to drive")
	only := flag.String("only", "", "run only the steps for this tool")
	verbose := flag.Bool("v", false, "print each result, redacted")
	writes := flag.Bool("writes", false, "run the write checks instead of the steps: a probe tag and a probe "+
		"collection this run creates are deleted without and with confirm, and the collection is opened to the "+
		"organization, read back and narrowed again")
	flag.Parse()

	if err := run(*bin, *only, *verbose, *writes); err != nil {
		fatal(err)
	}
}

// fatal is the one write to the terminal that does not go through a
// Redactor, and the `transcript` gate allows it by name.
//
// It is safe because of what reaches it: run returns either an error
// this file wrote or one from a JSON-RPC frame, and a step's failure
// detail is printed through the redactor long before it gets here. It
// is a named function rather than an inline Fprintln so that the rule
// the gate enforces is "one exception, this one" rather than "some
// prints are fine".
func fatal(err error) {
	fmt.Fprintln(os.Stderr, "livefavro:", err)
	os.Exit(1)
}

func run(bin, only string, verbose, writes bool) (err error) {
	if _, err := os.Stat(bin); err != nil { //nolint:gosec // G703: bin is the server the maintainer named with -bin
		return fmt.Errorf("%s: %w (run `make build` first)", bin, err)
	}

	// The credentials this process holds are redacted literally, because
	// no pattern should be trusted to find a token.
	red := redact.New(
		os.Getenv("FAVRO_USER_EMAIL"),
		os.Getenv("FAVRO_API_TOKEN"),
		os.Getenv("FAVRO_ORGANIZATION_ID"),
	)
	out := newPrinter(red)

	// The cleanups run however the run ends: finished, failed or
	// interrupted. A deferred call alone does not run on a signal.
	lc := &lifecycle{}
	defer lc.watch(out)()
	defer func() { err = errors.Join(err, lc.cleanup()) }()

	uploadDir, err := makeUploadDir()
	if err != nil {
		return err
	}
	lc.atExit(func() error { return os.RemoveAll(uploadDir) })

	// Destructive tools are registered so their dry-run paths can be
	// exercised. -writes is the one mode that writes, and runWrites says
	// what.
	env := []string{"FAVRO_ENABLE_DESTRUCTIVE=true", "FAVRO_UPLOAD_DIR=" + uploadDir}
	if writes {
		return runWrites(lc, bin, env, out, red)
	}

	// The steps run against the first objects the organization lists,
	// which the run did not create. Every step that writes carries
	// dry_run, which the live-cover gate holds, and --dry-run makes the
	// server's client refuse to send any write whatever a step carries.
	srv, err := lc.launch(bin, []string{"--dry-run"}, nil, env...)
	if err != nil {
		return err
	}
	defer srv.stop()

	tools, err := srv.listTools()
	if err != nil {
		return err
	}
	out.line("connected: %d tools registered", len(tools))

	steps := livecover.Steps()
	if only != "" {
		steps = filter(steps, only)
		if len(steps) == 0 {
			return fmt.Errorf("no step calls %q", only)
		}
	}

	tally := runSteps(srv, out, newPool(), steps, tools, verbose)

	out.line("")
	out.line("%d passed, %d failed, %d skipped; %d distinct values redacted",
		tally.passed, tally.failed, tally.skipped, red.Count())
	if tally.failed > 0 {
		return fmt.Errorf("%d steps failed", tally.failed)
	}
	return nil
}

// makeUploadDir makes the FAVRO_UPLOAD_DIR the upload tools need to
// exist: a file for the dry-run uploads to read, and a folder for the
// not-a-regular-file guard. The confinement steps name paths outside
// it, which must be refused before anything is read.
func makeUploadDir() (string, error) {
	dir, err := os.MkdirTemp("", "livefavro-upload-")
	if err != nil {
		return "", err
	}
	err = os.WriteFile(filepath.Join(dir, livecover.UploadFile), []byte("livefavro probe\n"), 0o600)
	if err == nil {
		err = os.Mkdir(filepath.Join(dir, livecover.UploadFolder), 0o700)
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// tally is what a run amounted to.
type tally struct{ passed, failed, skipped int }

// runSteps executes each step in order, feeding every result back into
// the pool so a later step can be called against something this
// organization actually has.
func runSteps(srv *session, out *printer, p *pool, steps []livecover.Step, tools map[string]tool, verbose bool) tally {
	var t tally
	for _, step := range steps {
		if _, registered := tools[step.Tool]; !registered {
			out.line("SKIP %-38s not registered", step.Tool)
			t.skipped++
			continue
		}
		args, missing := p.resolve(step.Args)
		if missing != "" {
			out.line("SKIP %-38s no %s seen yet in this organization", step.Tool, missing)
			t.skipped++
			continue
		}

		res, callErr := srv.call(step.Tool, args)
		if v := judge(step, res, callErr); v.ok {
			t.passed++
			out.line("ok   %-38s %s", step.Tool, step.Why)
			if verbose && res != nil {
				out.result(res)
			}
		} else {
			t.failed++
			out.line("FAIL %-38s %s", step.Tool, v.detail)
			if res != nil {
				out.result(res)
			}
		}
		if res != nil {
			p.absorb(res)
		}
	}
	return t
}

// filter returns the steps for one tool.
func filter(steps []livecover.Step, tool string) []livecover.Step {
	var out []livecover.Step
	for _, s := range steps {
		if s.Tool == tool {
			out = append(out, s)
		}
	}
	return out
}

// verdict is the outcome of one step.
type verdict struct {
	ok     bool
	detail string
}

// judge decides whether a step did what it said it would.
//
// A step that expects an error is not satisfied by any failure: it
// names the class, because §6.2's vocabulary is only worth something if
// the classes that come back from a real API are the documented ones.
func judge(step livecover.Step, res *callResult, err error) verdict {
	if err != nil {
		return verdict{detail: "call failed: " + err.Error()}
	}
	if step.ExpectError != "" {
		switch {
		case !res.IsError:
			return verdict{detail: "expected [" + step.ExpectError + "], got a result"}
		case !strings.HasPrefix(res.text(), "["+step.ExpectError+"]"):
			return verdict{detail: "expected [" + step.ExpectError + "], got " + firstLine(res.text())}
		default:
			return verdict{ok: true}
		}
	}
	if res.IsError {
		return verdict{detail: firstLine(res.text())}
	}
	return verdict{ok: true}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// printer is the only thing in this program that writes to the
// terminal, and it redacts everything on the way. It is safe to share
// with the goroutine that handles an interrupt.
type printer struct {
	mu  sync.Mutex
	red *redact.Redactor
	w   *bufio.Writer
}

func newPrinter(red *redact.Redactor) *printer {
	return &printer{red: red, w: bufio.NewWriter(os.Stdout)}
}

// line prints one redacted line.
func (p *printer) line(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = p.w.WriteString(p.red.Stringf(format, args...))
	_ = p.w.WriteByte('\n')
	_ = p.w.Flush()
}

// result prints a call's two halves, redacted and indented.
func (p *printer) result(res *callResult) {
	for _, l := range strings.Split(strings.TrimRight(res.text(), "\n"), "\n") {
		p.line("       %s", l)
	}
	if len(res.StructuredContent) > 0 {
		p.line("       structured: %s", clip(string(res.StructuredContent), 300))
	}
}

// clip shortens a value for one line, on a rune boundary.
//
// Slicing at a byte offset is what the first version did, and it wrote
// invalid UTF-8 into the transcript the moment a real card name
// contained an emoji — the same defect this repository had already
// fixed once in internal/render, reintroduced by writing the obvious
// three lines again.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := 0
	for i := range s {
		if i > n {
			break
		}
		cut = i
	}
	return s[:cut] + "…"
}

// pool remembers ids seen in earlier results, so a later step can be
// called against something that exists in this organization.
type pool struct {
	// Values keep their JSON type: a sequential id is an integer on the
	// wire and sending "42" would fail the tool's own input schema.
	byPlaceholder map[livecover.Placeholder]any
}

func newPool() *pool {
	return &pool{byPlaceholder: map[livecover.Placeholder]any{}}
}

// wireKeys maps a placeholder to the JSON keys a Favro response uses
// for it. Listed rather than derived: the point of a placeholder is
// that the driver knows what a card id is called, and a rule that
// guessed would quietly resolve the wrong field.
var wireKeys = map[livecover.Placeholder][]string{
	livecover.AnyCollectionID:   {"collectionId"},
	livecover.AnyWidgetCommonID: {"widgetCommonId"},
	livecover.AnyColumnID:       {"columnId"},
	livecover.AnyCardID:         {"cardId"},
	livecover.AnyCardCommonID:   {"cardCommonId"},
	livecover.AnyUserID:         {"userId"},
	livecover.AnyTagID:          {"tagId"},
	livecover.AnyCustomFieldID:  {"customFieldId"},
	livecover.AnyGroupID:        {"groupId"},
	livecover.AnyCommentID:      {"commentId"},
	livecover.AnyTaskID:         {"taskId"},
	livecover.AnyTaskListID:     {"taskListId"},
	livecover.AnySequentialID:   {"sequentialId"},
}

// absorb records any id this result carries.
func (p *pool) absorb(res *callResult) {
	if len(res.StructuredContent) == 0 {
		return
	}
	var decoded any
	if err := json.Unmarshal(res.StructuredContent, &decoded); err != nil {
		return
	}
	walk(decoded, func(key string, value any) {
		if s, ok := value.(string); ok && s == "" {
			return
		}
		for placeholder, keys := range wireKeys {
			for _, k := range keys {
				if k != key {
					continue
				}
				if _, seen := p.byPlaceholder[placeholder]; !seen {
					p.byPlaceholder[placeholder] = normalize(value)
				}
			}
		}
	})
}

// walk visits every scalar field in a decoded JSON value.
func walk(v any, visit func(key string, value any)) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			switch child.(type) {
			case string, float64:
				visit(k, child)
			default:
				walk(child, visit)
			}
		}
	case []any:
		for _, child := range t {
			walk(child, visit)
		}
	}
}

// normalize turns a JSON number back into an integer where it is one,
// so a tool declaring an integer option is sent one.
func normalize(v any) any {
	if f, ok := v.(float64); ok && f == float64(int64(f)) {
		return int64(f)
	}
	return v
}

// resolve substitutes placeholders. The second return names the first
// placeholder nothing has supplied, so the step can be skipped with a
// reason rather than sent with a literal "{card_id}".
func (p *pool) resolve(args map[string]any) (map[string]any, string) {
	out := make(map[string]any, len(args))
	for k, v := range args {
		resolved, missing := p.value(v)
		if missing != "" {
			return nil, missing
		}
		out[k] = resolved
	}
	return out, ""
}

// value resolves one argument, descending into the slices and maps a
// composite option carries — a dependency entry names a card the same
// way a top-level argument does.
func (p *pool) value(v any) (any, string) {
	switch t := v.(type) {
	case livecover.Placeholder:
		resolved, seen := p.byPlaceholder[t]
		if !seen {
			return nil, string(t)
		}
		return resolved, ""
	case []map[string]any:
		out := make([]map[string]any, 0, len(t))
		for _, entry := range t {
			resolved, missing := p.resolve(entry)
			if missing != "" {
				return nil, missing
			}
			out = append(out, resolved)
		}
		return out, ""
	case []any:
		out := make([]any, 0, len(t))
		for _, entry := range t {
			resolved, missing := p.value(entry)
			if missing != "" {
				return nil, missing
			}
			out = append(out, resolved)
		}
		return out, ""
	default:
		return v, ""
	}
}

// --- the MCP client -------------------------------------------------

// session is a running server and the pipes to it.
type session struct {
	cmd *exec.Cmd
	// in is the WRITE end of the server's stdin, kept because stop has
	// to close it and cmd.Stdin is the other end — see stop.
	in     io.WriteCloser
	stdin  *bufio.Writer
	stdout *bufio.Reader
	nextID int

	// onElicit, when set, declares form elicitation and hears each
	// question the server puts with elicitation/create, which is then
	// declined.
	onElicit func(message string)
}

// start runs the server with args, and, when onElicit is set, a client
// that can be asked.
func start(bin string, args []string, onElicit func(string), env ...string) (*session, error) {
	cmd := exec.Command(bin, args...) //nolint:gosec // G204: running the server the maintainer named, with flags this file writes, is the job
	cmd.Env = append(os.Environ(), env...)
	cmd.Stderr = nil // the server's own logs are not this transcript

	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	s := &session{
		cmd: cmd, in: in, stdin: bufio.NewWriter(in), stdout: bufio.NewReader(outPipe), nextID: 1,
		onElicit: onElicit,
	}
	if err := s.initialize(); err != nil {
		return nil, err
	}
	return s, nil
}

// stop closes the server's stdin and waits for it to go.
//
// **It has to close the write end, and cmd.Stdin is the read end.**
// StdinPipe sets cmd.Stdin to the read side of the pipe and RETURNS the
// write side; the previous version type-asserted cmd.Stdin to a Closer,
// which succeeds — it is an *os.File — and closed the wrong end. The
// server never saw EOF, so it never exited, so Wait never returned.
// Every run of this driver therefore printed its summary and then hung
// forever, holding the server process open, and `make live` never
// returned. It was survivable only because the summary comes first and
// a person reads it and presses Ctrl-C.
//
// Go's own documentation is where this is easy to get wrong: it says
// Wait closes the pipe after seeing the command exit, which for stdin
// is circular — the command exits when stdin closes.
//
// The bounded wait is the second half. A server wedged in a request
// will not exit on EOF either, and a driver run by hand should not need
// a Ctrl-C for that any more than for this.
func (s *session) stop() {
	_ = s.stdin.Flush()
	_ = s.in.Close()

	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(stopTimeout):
		_ = s.cmd.Process.Kill()
		<-done
	}
}

func (s *session) initialize() error {
	capabilities := map[string]any{}
	if s.onElicit != nil {
		capabilities["elicitation"] = map[string]any{"form": map[string]any{}}
	}
	_, err := s.request("initialize", map[string]any{
		"protocolVersion": "2025-11-25",
		"capabilities":    capabilities,
		"clientInfo":      map[string]any{"name": "livefavro", "version": "0"},
	})
	if err != nil {
		return err
	}
	return s.notify("notifications/initialized")
}

// tool is what tools/list says about one tool.
type tool struct {
	// readOnly is the tool's own readOnlyHint.
	readOnly bool
}

// listTools returns the registered tools, by name.
func (s *session) listTools() (map[string]tool, error) {
	raw, err := s.request("tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var result struct {
		Tools []struct {
			Name        string `json:"name"`
			Annotations struct {
				ReadOnlyHint bool `json:"readOnlyHint"`
			} `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	out := map[string]tool{}
	for _, t := range result.Tools {
		out[t.Name] = tool{readOnly: t.Annotations.ReadOnlyHint}
	}
	return out, nil
}

// callResult is one tools/call response.
type callResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

// text returns the readable half.
func (r *callResult) text() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range r.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

func (s *session) call(tool string, args map[string]any) (*callResult, error) {
	raw, err := s.request("tools/call", map[string]any{"name": tool, "arguments": args})
	if err != nil {
		return nil, err
	}
	var res callResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// request sends one JSON-RPC call and waits for its answer.
func (s *session) request(method string, params any) (json.RawMessage, error) {
	id := s.nextID
	s.nextID++

	if err := s.write(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	}); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(callTimeout)
	for time.Now().Before(deadline) {
		line, err := s.stdout.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("%s: %w", method, err)
		}
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(line, &frame); err != nil {
			continue // not a frame we sent for
		}
		if frame.Method != "" {
			// A request from the server, inside the call: answered here,
			// or the call it belongs to would never return.
			if len(frame.ID) > 0 {
				if err := s.answer(frame.ID, frame.Method, frame.Params); err != nil {
					return nil, err
				}
			}
			continue
		}
		if string(frame.ID) != strconv.Itoa(id) {
			continue
		}
		if frame.Error != nil {
			return nil, errors.New(frame.Error.Message)
		}
		return frame.Result, nil
	}
	return nil, fmt.Errorf("%s: no answer within %s", method, callTimeout)
}

// answer replies to a request the server made: a question, heard by
// onElicit and declined, and anything else as not supported.
func (s *session) answer(id json.RawMessage, method string, params json.RawMessage) error {
	if method == "elicitation/create" && s.onElicit != nil {
		var p struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(params, &p)
		s.onElicit(p.Message)
		return s.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"action": "decline"}})
	}
	return s.write(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": -32601, "message": "livefavro does not answer " + method},
	})
}

func (s *session) notify(method string) error {
	return s.write(map[string]any{"jsonrpc": "2.0", "method": method})
}

func (s *session) write(frame map[string]any) error {
	body, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	if _, err := s.stdin.Write(append(body, '\n')); err != nil {
		return err
	}
	return s.stdin.Flush()
}
