package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	if err := run(os.Args[0], "favro_ping", false, false); err != nil {
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

// The driver printed its summary and then hung forever, holding the
// server process open, because stop closed the wrong end of the stdin
// pipe: StdinPipe sets cmd.Stdin to the READ end and returns the write
// end, and a type assertion to a Closer succeeds on either. `make live`
// never returned, and the only reason it was survivable is that the
// summary prints first and a person reads it and presses Ctrl-C.
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
