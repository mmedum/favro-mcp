package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mmedum/favro-mcp/v4/internal/auth"
	"github.com/mmedum/favro-mcp/v4/internal/livecover"
	"github.com/mmedum/favro-mcp/v4/internal/redact"
)

// TestMain runs this test binary as a helper child when
// LIVEFAVRO_HELPER names a role, before the testing package parses the
// command line: the driver starts its server with --dry-run, which the
// testing package would refuse.
func TestMain(m *testing.M) {
	if role := os.Getenv("LIVEFAVRO_HELPER"); role != "" {
		os.Exit(helper(role))
	}
	os.Exit(m.Run())
}

// The steps run against objects the organization already holds, so the
// server they run through must refuse to send any write, whatever a
// step carries.
func TestTheStepsRunThroughAServerStartedWithDryRun(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("LIVEFAVRO_HELPER", "server")
	t.Setenv("LIVEFAVRO_ARGS_FILE", argsFile)

	if err := run(os.Args[0], "favro_ping", false, false, auth.Token{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "--dry-run" {
		t.Errorf("the steps' server was started with %q, want --dry-run", got)
	}
}

// Each line of a run names its step by the name a code comment cites,
// and a failure or a skip says why after the name. Under -v a step's Why
// follows its name.
func TestEachStepIsPrintedByItsName(t *testing.T) {
	steps := []livecover.Step{
		{Name: "read a tag", Tool: "favro_get_tag", Args: map[string]any{}, Why: "a tag by id"},
		{Name: "a tag that does not exist is [not_found]", Tool: "favro_get_tag", Args: map[string]any{}, ExpectError: "not_found"},
		{Name: "read a group", Tool: "favro_get_group", Args: map[string]any{}, Why: "a group by id"},
		{Name: "read a card's tag", Tool: "favro_get_tag", Args: map[string]any{"card_id": livecover.AnyCardID}},
	}
	for _, tc := range []struct {
		verbose bool
		want    []string
	}{
		{false, []string{
			"ok   read a tag",
			"FAIL a tag that does not exist is [not_found]: expected [not_found], got [forbidden] Favro denied access (HTTP 403)",
			"SKIP read a group: favro_get_group is not registered",
			"SKIP read a card's tag: no {card_id} seen yet in this organization",
		}},
		{true, []string{
			"ok   read a tag (a tag by id)",
			"FAIL a tag that does not exist is [not_found]: expected [not_found], got [forbidden] Favro denied access (HTTP 403)",
			"SKIP read a group (a group by id): favro_get_group is not registered",
			"SKIP read a card's tag: no {card_id} seen yet in this organization",
		}},
	} {
		var buf bytes.Buffer
		out := &printer{red: redact.New(), w: bufio.NewWriter(&buf)}
		fake := &fakeFavro{results: map[string][]string{"favro_get_tag": {`{}`, "[forbidden] Favro denied access (HTTP 403)"}}}

		got := runSteps(fake, out, newPool(""), steps, map[string]tool{"favro_get_tag": {readOnly: true}}, tc.verbose)
		if got != (tally{passed: 1, failed: 1, skipped: 2}) {
			t.Errorf("-v %v: tally %+v, want 1 passed, 1 failed and 2 skipped", tc.verbose, got)
		}
		lines := strings.Split(buf.String(), "\n")
		for _, want := range tc.want {
			if !slices.Contains(lines, want) {
				t.Errorf("-v %v: no line %q in the transcript:\n%s", tc.verbose, want, buf.String())
			}
		}
	}
}

// The share steps name the signed-in account, found in the users
// listing by the address the server signs in with. AnyUserID stays the
// first userId a result carried, which need not be a member.
func TestThePoolFindsTheSignedInAccountByItsEmail(t *testing.T) {
	const users = `{"items":[
		{"userId":"user-someone-else","name":"Someone","organizationRole":"fullMember"},
		{"userId":"user-also-else","email":"also@example.test","organizationRole":"fullMember"},
		{"userId":"user-signed-in","email":"Signed.In@Example.test","organizationRole":"administrator"}
	]}`
	cases := []struct {
		name, email string
		want        any // OwnUserID's value, or nil when it stays unresolved
	}{
		{"the entry with the account's email, in any case", " signed.in@example.test ", "user-signed-in"},
		{"no entry, when no email is known", "", nil},
		{"no entry, when none carries the email", "elsewhere@example.test", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPool(tc.email)
			p.absorb(&callResult{StructuredContent: json.RawMessage(users)})

			own, seen := p.byPlaceholder[livecover.OwnUserID]
			if (tc.want == nil && seen) || (tc.want != nil && own != tc.want) {
				t.Errorf("OwnUserID resolved to %v (seen %v), want %v", own, seen, tc.want)
			}
			if got := p.byPlaceholder[livecover.AnyUserID]; got != "user-someone-else" {
				t.Errorf("AnyUserID resolved to %v, want the first userId, user-someone-else", got)
			}
		})
	}
}

// Every placeholder a step names is resolved before it is sent, however
// deep it sits, and a step naming one nothing has supplied is skipped.
// A share entry is a map inside a list, and the driver sent its
// "{own_user_id}" as written; the server refused it as nobody in the
// organization, which read as the server refusing the signed-in account.
func TestEveryPlaceholderAStepNamesIsResolvedOrSkipsTheStep(t *testing.T) {
	full := newPool("")
	for _, ph := range livecover.Placeholders {
		full.byPlaceholder[ph] = "resolved-" + strings.Trim(string(ph), "{}")
	}
	named := map[livecover.Placeholder]int{}
	for _, step := range livecover.Steps() {
		sent, err := json.Marshal(step.Args)
		if err != nil {
			t.Fatal(err)
		}
		var namesOne bool
		for _, ph := range livecover.Placeholders {
			n := strings.Count(string(sent), `"`+string(ph)+`"`)
			named[ph] += n
			namesOne = namesOne || n > 0
		}

		args, missing := full.resolve(step.Args)
		if missing != "" {
			t.Errorf("%s (%q): %s unresolved with every placeholder supplied", step.Tool, step.Name, missing)
			continue
		}
		got, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		for _, ph := range livecover.Placeholders {
			if strings.Contains(string(got), `"`+string(ph)+`"`) {
				t.Errorf("%s (%q) sends %s as written: %s", step.Tool, step.Name, ph, got)
			}
		}

		if _, missing := newPool("").resolve(step.Args); namesOne && missing == "" {
			t.Errorf("%s (%q) names a placeholder and was not skipped with none supplied", step.Tool, step.Name)
		}
	}
	// The floor: every placeholder was found in some step, so the check
	// read the steps that carry them.
	for _, ph := range livecover.Placeholders {
		if named[ph] == 0 {
			t.Errorf("no step names %s, so its resolution was not checked", ph)
		}
	}
	if named[livecover.OwnUserID] < 4 {
		t.Errorf("found %s in %d places, want at least the four share steps' entries", livecover.OwnUserID, named[livecover.OwnUserID])
	}
}

// stop closes the write end of the stdin pipe: StdinPipe sets cmd.Stdin
// to the READ end and returns the write end, and a type assertion to a
// Closer succeeds on either. Closing the wrong end leaves the server
// waiting for EOF and the driver hung after its summary.
//
// The child is this test binary re-invoked, rather than `cat` or
// `sleep`, because `make check` runs the suite on the Windows runner
// too.
func TestStopReturnsWhenTheServerExitsOnEOF(t *testing.T) {
	s := helperSession(t, "eof")

	done := make(chan struct{})
	go func() { s.stop(); close(done) }()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("stop did not return: the server never saw EOF on its stdin, which is the hang " +
			"that made every `make live` run need a Ctrl-C")
	}
}

// And the other half: a server wedged in a request will not exit on EOF
// either, and a driver run by hand should not need a Ctrl-C for that.
func TestStopKillsAServerThatIgnoresEOF(t *testing.T) {
	restore := stopTimeout
	stopTimeout = 200 * time.Millisecond
	t.Cleanup(func() { stopTimeout = restore })

	s := helperSession(t, "ignore")

	done := make(chan struct{})
	go func() { s.stop(); close(done) }()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("stop did not return for a server that ignores EOF; the bounded wait is not bounding")
	}
}

// helperSession starts this test binary as a child playing the part of a
// server, and wraps it in the session type stop operates on.
func helperSession(t *testing.T, role string) *session {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "LIVEFAVRO_HELPER="+role)

	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return &session{cmd: cmd, in: in, stdin: bufio.NewWriter(in)}
}

// helper is the child the tests drive, in the role they name, and
// returns its exit code.
func helper(role string) int {
	switch role {
	case "eof":
		// What a well-behaved MCP server does: read until stdin closes,
		// then exit.
		_, _ = io.Copy(io.Discard, os.Stdin)
		return 0
	case "ignore":
		// What a wedged one does.
		time.Sleep(5 * time.Minute)
		return 0
	case "server":
		return helperServer()
	case "signals":
		return helperSignals()
	}
	return 2
}

// helperServer is an MCP server with no tools. It writes the arguments
// it was started with to LIVEFAVRO_ARGS_FILE, when that is set.
//
// It answers through the printer, because nothing else in this package
// may name the terminal; nothing in these frames is a shape the
// redactor rewrites.
func helperServer() int {
	if path := os.Getenv("LIVEFAVRO_ARGS_FILE"); path != "" {
		if err := os.WriteFile(path, []byte(strings.Join(os.Args[1:], " ")), 0o600); err != nil {
			return 1
		}
	}
	out := newPrinter(redact.New())
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(in.Bytes(), &req) != nil || len(req.ID) == 0 {
			continue
		}
		result := `{}`
		if req.Method == "tools/list" {
			result = `{"tools":[]}`
		}
		out.line(`{"jsonrpc":"2.0","id":%s,"result":%s}`, req.ID, result)
	}
	return 0
}
