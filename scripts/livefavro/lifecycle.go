package main

import (
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// interruptSignals end a run early, and each runs the cleanups first.
// SIGHUP is the terminal closing, which is how a run left in a tab
// ends.
var interruptSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

// errInterrupted is what launch returns once the run has been
// interrupted.
var errInterrupted = errors.New("the run was interrupted")

// lifecycle is what a run has to undo however it ends.
//
// Go runs no deferred call when a signal ends the process, so the
// cleanups are registered here and run once: when the run returns, or
// when a signal arrives. An interrupt kills the servers the run
// launched first, so the run can send nothing more while it is cleaned
// up after. A terminal's Ctrl-C reaches the server too, so a cleanup
// that needs a server starts its own.
type lifecycle struct {
	mu          sync.Mutex
	servers     []*session
	cleanups    []func() error
	interrupted bool

	once sync.Once
	err  error
}

// launch starts a server the run sends its calls through. An interrupt
// kills it.
func (l *lifecycle) launch(bin string, args []string, onElicit func(string), env ...string) (*session, error) {
	s, err := start(bin, args, onElicit, env...)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.interrupted {
		_ = s.cmd.Process.Kill()
		return nil, errInterrupted
	}
	l.servers = append(l.servers, s)
	return s, nil
}

// atExit registers a cleanup. Cleanups run last registered first.
func (l *lifecycle) atExit(fn func() error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanups = append(l.cleanups, fn)
}

// cleanup runs every cleanup once, and returns what failed. A second
// call waits for the first and returns the same.
func (l *lifecycle) cleanup() error {
	l.once.Do(func() {
		l.mu.Lock()
		fns := append([]func() error(nil), l.cleanups...)
		l.mu.Unlock()
		var errs []error
		for i := len(fns) - 1; i >= 0; i-- {
			errs = append(errs, fns[i]())
		}
		l.err = errors.Join(errs...)
	})
	return l.err
}

// interrupt kills every server the run launched, then cleans up.
func (l *lifecycle) interrupt() error {
	l.mu.Lock()
	l.interrupted = true
	servers := append([]*session(nil), l.servers...)
	l.mu.Unlock()
	for _, s := range servers {
		_ = s.cmd.Process.Kill()
	}
	return l.cleanup()
}

// watch interrupts the run on the first of interruptSignals and exits.
// The function it returns stops watching.
func (l *lifecycle) watch(out *printer) (stop func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, interruptSignals...)
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-signals:
			out.line("interrupted by %s: cleaning up", sig)
			if err := l.interrupt(); err != nil {
				out.line("cleanup failed: %v", err)
			}
			os.Exit(1)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
}
