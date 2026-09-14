package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// smokeEnv is the environment both runs get: credentials shaped like
// credentials and belonging to nobody, and the startup validation call
// switched off so the gate reaches no network. Every value carries a
// marker word, which is what keeps the leak gate from reporting this
// file.
var smokeEnv = []string{
	"FAVRO_USER_EMAIL=smoke@example.test",
	"FAVRO_API_TOKEN=synthetic-token-for-the-smoke-gate",
	"FAVRO_ORGANIZATION_ID=synthetic-organization",
	"FAVRO_MCP_SKIP_VALIDATE=1",
	"FAVRO_LOG_LEVEL=error",
}

// smoke drives the built binary over stdio, twice.
//
// Two runs, because one cannot prove both things. The first writes every
// message, waits, then closes stdin, and asserts what came back: the
// server answers, lists its tools, and serves a call. The second closes
// stdin the instant the last byte is written and asserts only the exit
// code — that is how a client actually goes away, and it is a different
// path, because the server is still working when the pipe ends.
//
// The second run is not hypothetical here. This binary exited 1 on every
// ordinary disconnect until the commit that added this gate: the SDK
// reports a closed connection as JSON-RPC -32004 with the EOF only in
// the message text, and `errors.Is(err, io.EOF)` never matched it. A run
// that sleeps before closing stdin cannot see that — with nothing in
// flight the exit really is 0 — which is why this one must not wait.
//
// The tool it calls is favro_ping, the one tool that answers from
// process state alone. A gate that reached Favro would need a token, and
// would fail when somebody else's network did.
func smoke(w io.Writer, args []string) error {
	bin, err := filepath.Abs(binOr(args))
	if err != nil {
		return err
	}
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("%s: %w (run `make build` first)", bin, err)
	}
	env := append(os.Environ(), smokeEnv...)

	const initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}`
	conversation := strings.Join([]string{
		initialize,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"favro_ping","arguments":{}}}`,
	}, "\n") + "\n"

	out, stderr, err := drive(bin, env, conversation, time.Second)
	if err != nil {
		return fmt.Errorf("the conversation run failed: %w\n%s", err, clip(stderr))
	}
	for _, want := range []struct{ needle, why string }{
		{`"id":1`, "no initialize response"},
		{`"name":"favro_ping"`, "favro_ping missing from tools/list"},
		{`"name":"favro_get_card_full"`, "favro_get_card_full missing from tools/list"},
	} {
		if !strings.Contains(out, want.needle) {
			return fmt.Errorf("%s:\n%s", want.why, clip(out))
		}
	}
	call := lineWith(out, `"id":3`)
	if call == "" {
		return fmt.Errorf("no response to the favro_ping call:\n%s", clip(out))
	}
	if strings.Contains(call, `"isError":true`) {
		return fmt.Errorf("favro_ping answers from process state and must not error:\n%s", clip(call))
	}
	if !strings.Contains(call, "synthetic-organization") {
		return fmt.Errorf("favro_ping did not report the organization it was started with:\n%s", clip(call))
	}

	// The abrupt close. No pause: the point is that the server is still
	// working when the pipe ends.
	if _, stderr, err := drive(bin, env, initialize+"\n", 0); err != nil {
		return fmt.Errorf("an abrupt client disconnect should exit 0: %w\n%s", err, clip(stderr))
	}

	_, err = fmt.Fprintln(w, "stdio smoke ok: a conversation and an abrupt disconnect")
	return err
}

// drive runs the binary with the given input, closing stdin after pause,
// and fails when it exits non-zero.
func drive(bin string, env []string, input string, pause time.Duration) (stdout, stderr string, err error) {
	cmd := exec.Command(bin)
	cmd.Env = env
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", "", err
	}
	if err := cmd.Start(); err != nil {
		return "", "", err
	}
	// Past Start there is a child process, and every way out of this
	// function has to take it with it: a server that dies during startup
	// makes the write fail with EPIPE, and returning there would leave
	// an MCP server running with an open stdin for as long as the gate
	// lives. One deferred func does both jobs — reap whatever is still
	// running, then publish the buffers — which is also what keeps the
	// read off the copying goroutines.
	reaped := false
	defer func() {
		if !reaped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		stdout, stderr = out.String(), errBuf.String()
	}()

	if _, err := io.WriteString(stdin, input); err != nil {
		return "", "", err
	}
	if pause > 0 {
		time.Sleep(pause)
	}
	if err := stdin.Close(); err != nil {
		return "", "", err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
		reaped = true
		return "", "", err
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		<-done // that goroutine's Wait is the reap, and it flushes the buffers
		reaped = true
		return "", "", errors.New("the server did not exit after stdin closed")
	}
}

func lineWith(text, needle string) string {
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, needle) {
			return l
		}
	}
	return ""
}

// clip keeps a failure message readable and bounded. This gate drives a
// server with invented credentials, so nothing here can carry a tenant's
// data — and it is bounded anyway, because an unbounded dump of server
// output into a CI log is how that stops being true.
func clip(s string) string {
	const maximum = 800
	if len(s) <= maximum {
		return s
	}
	return s[:maximum] + "…"
}
