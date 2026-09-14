package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

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
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
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

// TestHelperProcess is not a test. It is the child the two tests above
// drive, selected by an environment variable so it does nothing during
// an ordinary run.
func TestHelperProcess(t *testing.T) {
	switch os.Getenv("LIVEFAVRO_HELPER") {
	case "eof":
		// What a well-behaved MCP server does: read until stdin closes,
		// then exit.
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	case "ignore":
		// What a wedged one does.
		time.Sleep(5 * time.Minute)
		os.Exit(0)
	}
}
