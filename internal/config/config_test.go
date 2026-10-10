package config

import (
	"log/slog"
	"os"
	"path/filepath"
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

// TestLoadRequirePrompt pins the parse. The variable exists to refuse,
// so a value Go cannot read means on, and says so.
func TestLoadRequirePrompt(t *testing.T) {
	for _, tc := range []struct {
		env        string
		want, warn bool
	}{
		{"", false, false},
		{"true", true, false},
		{"0", false, false},
		{"yes", true, true},
	} {
		t.Setenv(EnvRequirePrompt, tc.env)
		cfg := Load()
		if cfg.RequirePrompt != tc.want || (len(cfg.Warnings) > 0) != tc.warn {
			t.Errorf("%q: RequirePrompt %v, warnings %q", tc.env, cfg.RequirePrompt, cfg.Warnings)
		}
	}
}

// TestLoadInteractionHint pins the parse. The mark is on unless the
// variable says false; a value Go cannot read keeps it on, and says so.
func TestLoadInteractionHint(t *testing.T) {
	for _, tc := range []struct {
		env              string
		suppressed, warn bool
	}{
		{"", false, false},
		{"true", false, false},
		{"false", true, false},
		{"0", true, false},
		{"off", false, true},
	} {
		t.Setenv(EnvInteractionHint, tc.env)
		cfg := Load()
		// Only this variable's warnings: another one set in the shell
		// must not decide the case.
		var mine []string
		for _, w := range cfg.Warnings {
			if strings.Contains(w, EnvInteractionHint) {
				mine = append(mine, w)
			}
		}
		warned := strings.Join(mine, "\n")
		if cfg.SuppressInteractionHint != tc.suppressed || (warned != "") != tc.warn {
			t.Errorf("%q: SuppressInteractionHint %v, warnings %q", tc.env, cfg.SuppressInteractionHint, warned)
		}
		if tc.warn && !strings.Contains(warned, "treated as true") {
			t.Errorf("%q: the warning does not say what it means: %q", tc.env, warned)
		}
	}
}

// TestLoadUploadDir pins what FAVRO_UPLOAD_DIR accepts: an absolute path
// to a directory that exists, cleaned. Anything else leaves uploads off
// and says why.
func TestLoadUploadDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		env, want, warn string
	}{
		{"", "", ""},
		{dir, dir, ""},
		{" " + dir + "/sub/.. ", dir, ""},
		{"relative/dir", "", "not an absolute path"},
		{file, "", "not a directory"},
		{filepath.Join(dir, "missing"), "", "cannot be read"},
	} {
		t.Setenv(EnvUploadDir, tc.env)
		if tc.env == " "+dir+"/sub/.. " {
			if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil && !os.IsExist(err) {
				t.Fatal(err)
			}
		}
		cfg := Load()
		warned := strings.Join(cfg.Warnings, "\n")
		if cfg.UploadDir != tc.want || (tc.warn == "") != (warned == "") || !strings.Contains(warned, tc.warn) {
			t.Errorf("%q: UploadDir %q, warnings %q", tc.env, cfg.UploadDir, warned)
		}
		if tc.warn != "" && !strings.Contains(warned, "upload tools stay unregistered") {
			t.Errorf("%q: the warning does not say what it means: %q", tc.env, warned)
		}
	}
}
