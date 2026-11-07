package main

import (
	"io"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// sink is an io.Writer a test can make assertions about. Every gate
// reports how much it read, and that sentence is the only difference
// between "found nothing" and "looked at nothing".
type sink struct{ strings.Builder }

func (s *sink) mustSay(t *testing.T, want string) {
	t.Helper()
	if !strings.Contains(s.String(), want) {
		t.Errorf("the gate printed %q, which does not say %q — a gate that cannot say how much "+
			"it read cannot be distinguished from one that read nothing", s.String(), want)
	}
}

// funcName is the name of the function a registry entry runs, for the
// test that requires each command to be exercised somewhere.
func funcName(f func(io.Writer, []string) error) string {
	full := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
	return full[strings.LastIndexByte(full, '.')+1:]
}

// putAll writes each file's body to its path under root.
func putAll(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		put(t, root, rel, body)
	}
}

// isolateGit keeps the machine's git configuration from every git the
// test runs, the gates' own included, so a personal excludes file or hook
// cannot change what a gate sees, and gives a commit an author.
func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"XDG_CONFIG_HOME": t.TempDir(), "GIT_AUTHOR_NAME": "Fixture", "GIT_AUTHOR_EMAIL": "fixture@example.com",
		"GIT_COMMITTER_NAME": "Fixture", "GIT_COMMITTER_EMAIL": "fixture@example.com",
	} {
		t.Setenv(k, v)
	}
}

// initRepo makes an empty git repository in a temporary directory, on
// branch main, and moves the test into it, git isolated as isolateGit
// isolates it.
func initRepo(t *testing.T) string {
	t.Helper()
	isolateGit(t)
	dir := t.TempDir()
	t.Chdir(dir)
	mustGit(t, "init", "--quiet", "--initial-branch=main")
	return dir
}

// mustGit runs git for a test, which wants a fatal rather than an error.
func mustGit(t *testing.T, args ...string) {
	t.Helper()
	if _, err := git(args...); err != nil {
		t.Fatal(err)
	}
}
