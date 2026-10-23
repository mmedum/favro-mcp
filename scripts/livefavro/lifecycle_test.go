package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/mmedum/favro-mcp/v4/internal/redact"
)

// A run that a signal ends must still clean up: delete its probes and
// its upload directory. Go runs no deferred call on a signal, so without
// a handler none of it happens. SIGHUP is the terminal closing.
func TestEachInterruptRunsTheCleanups(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no SIGTERM or SIGHUP to send")
	}
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "cleaned-up")
			cmd := exec.Command(os.Args[0])
			cmd.Env = append(os.Environ(), "LIVEFAVRO_HELPER=signals", "LIVEFAVRO_MARKER="+marker)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })

			r := bufio.NewReader(stdout)
			if line, err := r.ReadString('\n'); err != nil || line != "ready\n" {
				t.Fatalf("the helper said %q, %v; want ready", line, err)
			}
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}

			done := make(chan struct{})
			go func() { _, _ = io.Copy(io.Discard, r); _ = cmd.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Fatalf("the run did not end on %s", sig)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Errorf("%s ended the run without running its cleanups", sig)
			}
		})
	}
}

// helperSignals is a run with one cleanup, which writes
// LIVEFAVRO_MARKER, waiting to be interrupted.
func helperSignals() int {
	out := newPrinter(redact.New())
	lc := &lifecycle{}
	lc.atExit(func() error { return os.WriteFile(os.Getenv("LIVEFAVRO_MARKER"), []byte("cleaned up"), 0o600) })
	stop := lc.watch(out)
	defer stop()
	out.line("ready")
	time.Sleep(time.Minute)
	return 3
}

// An interrupt kills the servers the run launched before it cleans up,
// so that the run cannot send a write after the cleanup has looked for
// what it made; and it launches none after.
func TestAnInterruptKillsTheServersBeforeTheCleanups(t *testing.T) {
	t.Setenv("LIVEFAVRO_HELPER", "server")
	lc := &lifecycle{}
	srv, err := lc.launch(os.Args[0], nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.cmd.Process.Kill() })

	exited := make(chan struct{})
	go func() { _ = srv.cmd.Wait(); close(exited) }()
	lc.atExit(func() error {
		select {
		case <-exited:
			return nil
		case <-time.After(10 * time.Second):
			return errors.New("the server was still running when the cleanups ran")
		}
	})

	if err := lc.interrupt(); err != nil {
		t.Error(err)
	}
	if _, err := lc.launch(os.Args[0], nil, nil); !errors.Is(err, errInterrupted) {
		t.Errorf("launch after an interrupt returned %v, want errInterrupted", err)
	}
}
