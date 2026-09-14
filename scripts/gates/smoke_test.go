package main

import (
	"os"
	"strings"
	"testing"
)

func built(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestLineWithFindsTheResponse(t *testing.T) {
	out := `{"id":1,"result":{}}` + "\n" + `{"id":3,"result":{"isError":true}}` + "\n"
	if got := lineWith(out, `"id":3`); !strings.Contains(got, "isError") {
		t.Errorf("lineWith returned %q", got)
	}
	if got := lineWith(out, `"id":9`); got != "" {
		t.Errorf("a missing id should return nothing, got %q", got)
	}
}

// A failure message that dumps a server's whole output into a CI log is
// how a gate that protects a transcript stops protecting it.
func TestClipIsBounded(t *testing.T) {
	long := strings.Repeat("x", 5000)
	if got := clip(long); len(got) > 810 {
		t.Errorf("clip left %d characters", len(got))
	}
	if got := clip("short"); got != "short" {
		t.Errorf("clip mangled a short string: %q", got)
	}
}

// Every value the smoke gate starts a server with has to be invented,
// or the gate's own environment would be reported by the leak scan —
// and the fix somebody reaches for then is to exempt the file.
func TestSmokeEnvironmentIsInvented(t *testing.T) {
	for _, kv := range smokeEnv {
		if found := findLeaks(kv); len(found) > 0 {
			t.Errorf("smokeEnv carries %q, which the leak scan reports: %v", kv, found)
		}
	}
}

func TestBinOrFallsBackToTheBuildOutput(t *testing.T) {
	if got := binOr([]string{"some/binary"}); got != "some/binary" {
		t.Errorf("an explicit argument should win, got %q", got)
	}
	if got := binOr(nil); !strings.Contains(got, "favro-mcp") {
		t.Errorf("the default should be the built binary, got %q", got)
	}
}
