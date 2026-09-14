package main

import (
	"io"
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
