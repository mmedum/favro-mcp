package config

import (
	"log/slog"
	"strings"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in         string
		want       slog.Level
		recognized bool
	}{
		{"", slog.LevelInfo, true},
		{"info", slog.LevelInfo, true},
		{"debug", slog.LevelDebug, true},
		{"warn", slog.LevelWarn, true},
		{"warning", slog.LevelWarn, true},
		{"error", slog.LevelError, true},
		{"loud", slog.LevelInfo, false},
	}
	for _, tc := range cases {
		got, recognized := ParseLogLevel(tc.in)
		if got := got; got != tc.want {
			t.Errorf("level for %q: got %v, want %v", tc.in, got, tc.want)
		}
		if got := recognized; got != tc.recognized {
			t.Errorf("recognized for %q: got %v, want %v", tc.in, got, tc.recognized)
		}
	}
}

// TestLoadDestructive pins the parse. The default and every value Go
// cannot read mean off: a typo in the variable that enables deletes
// must not enable deletes.
func TestLoadDestructive(t *testing.T) {
	cases := []struct {
		name      string
		env       string
		want      bool
		wantRawIn bool // the value is reported back as unreadable
	}{
		{"unset", "", false, false},
		{"true", "true", true, false},
		{"upper", "TRUE", true, false},
		{"one", "1", true, false},
		{"false", "false", false, false},
		{"yes", "yes", false, true},
		{"padded", "  true ", false, true}, // ParseBool does not trim, and neither do we
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvEnableDestructive, tc.env)
			cfg := Load()
			if got := cfg.Destructive; got != tc.want {
				t.Errorf("cfg.Destructive = %v, want %v", got, tc.want)
			}
			if tc.wantRawIn {
				if len(cfg.Warnings) != 1 {
					t.Fatalf("an unreadable value must be reported, not silently dropped: got %d", len(cfg.Warnings))
				}
				if !strings.Contains(cfg.Warnings[0], tc.env) {
					t.Errorf("cfg.Warnings[0] does not contain %q", tc.env)
				}
			} else if len(cfg.Warnings) != 0 {
				t.Errorf("cfg.Warnings = %v, want empty", cfg.Warnings)
			}
		})
	}
}

func TestLoadSkipValidateAndLevel(t *testing.T) {
	t.Setenv(EnvSkipValidate, "")
	t.Setenv(EnvLogLevel, "  ERROR ")
	cfg := Load()
	if cfg.SkipValidate {
		t.Error("cfg.SkipValidate = true, want false")
	}
	if got := cfg.LogLevel; got != slog.LevelError {
		t.Errorf("the value is trimmed and case-folded: got %v, want %v", got, slog.LevelError)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("cfg.Warnings = %v, want empty", cfg.Warnings)
	}

	t.Setenv(EnvSkipValidate, "1")
	if !Load().SkipValidate {
		t.Error("any non-empty value skips validation")
	}
}

// TestWarningsNameTheVariable is what makes an unreadable setting
// findable: Load runs before the logger exists, so the only way a
// person learns their value was ignored is this sentence.
func TestWarningsNameTheVariable(t *testing.T) {
	t.Setenv(EnvLogLevel, "loud")
	t.Setenv(EnvEnableDestructive, "perhaps")

	warnings := Load().Warnings
	if len(warnings) != 2 {
		t.Fatalf("len(warnings) = %d, want 2", len(warnings))
	}

	joined := warnings[0] + "\n" + warnings[1]
	if !strings.Contains(joined, EnvLogLevel) {
		t.Errorf("joined does not contain %q", EnvLogLevel)
	}
	if !strings.Contains(joined, "loud") {
		t.Errorf("joined does not contain %q", "loud")
	}
	if !strings.Contains(joined, EnvEnableDestructive) {
		t.Errorf("joined does not contain %q", EnvEnableDestructive)
	}
	if !strings.Contains(joined, "perhaps") {
		t.Errorf("joined does not contain %q", "perhaps")
	}
	if !strings.Contains(joined, "stay unregistered") {
		t.Errorf("the warning has to say what the ignored value cost: %q missing", "stay unregistered")
	}
}
