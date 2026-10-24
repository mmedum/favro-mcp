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

// A removed setting still turned on refuses to start: ignored, it
// would quietly allow what its owner set it to stop. Off and unset are
// still accepted, and a value that is not a boolean may mean on.
func TestARemovedSettingIsRefusedUnlessOff(t *testing.T) {
	for _, tc := range []struct {
		env     string
		refused bool
	}{
		{"", false},
		{"false", false},
		{" 0 ", false},
		{"true", true},
		{"1", true},
		{"yes", true},
	} {
		t.Setenv(EnvRequirePrompt, tc.env)
		cfg := Load()
		if got := len(cfg.Refusals) == 1; got != tc.refused || len(cfg.Refusals) > 1 {
			t.Errorf("FAVRO_REQUIRE_PROMPT=%q: refusals %q, want refused %v", tc.env, cfg.Refusals, tc.refused)
		}
		if tc.refused && len(cfg.Refusals) == 1 && !strings.HasPrefix(cfg.Refusals[0], "FAVRO_REQUIRE_PROMPT was removed") {
			t.Errorf("FAVRO_REQUIRE_PROMPT=%q: the refusal does not name the setting: %q", tc.env, cfg.Refusals[0])
		}
	}
}

// FAVRO_INTERACTION_HINT=false took the mark off, so that `claude -p`
// could run the marked deletes. It is ignored now, which only makes the
// server stricter, so the server starts; but a deployment that relied
// on it hears why those calls are refused. Unset or true changes
// nothing and says nothing.
func TestARemovedHintIsWarnedWhenOff(t *testing.T) {
	for _, tc := range []struct {
		env    string
		warned bool
	}{
		{"", false},
		{"true", false},
		{"false", true},
		{"0", true},
		{"nope", true},
	} {
		t.Setenv(EnvInteractionHint, tc.env)
		cfg := Load()
		var warned []string
		for _, w := range cfg.Warnings {
			if strings.HasPrefix(w, "FAVRO_INTERACTION_HINT was removed and is ignored") {
				warned = append(warned, w)
			}
		}
		if (len(warned) == 1) != tc.warned || len(warned) > 1 || len(cfg.Refusals) != 0 {
			t.Errorf("FAVRO_INTERACTION_HINT=%q: warnings %q, refusals %q; want warned %v and no refusal",
				tc.env, cfg.Warnings, cfg.Refusals, tc.warned)
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

// LoadFrom reads only the environment it is given. The bundle gate
// loads Claude Desktop's environment through it and holds the result to
// an empty one's, and a read that reached the process environment would
// make that comparison depend on the shell the gate ran in.
func TestLoadFromReadsOnlyTheEnvironmentItIsGiven(t *testing.T) {
	t.Setenv(EnvEnableDestructive, "true")
	t.Setenv(EnvLogLevel, "debug")
	t.Setenv(EnvSkipValidate, "1")
	t.Setenv(EnvUploadDir, t.TempDir())
	t.Setenv(EnvRequirePrompt, "true")
	t.Setenv(EnvInteractionHint, "false")

	cfg := LoadFrom(func(string) string { return "" })
	if cfg.Destructive || cfg.LogLevel != slog.LevelInfo || cfg.SkipValidate || cfg.UploadDir != "" ||
		len(cfg.Refusals) != 0 || len(cfg.Warnings) != 0 {
		t.Errorf("an empty environment loaded %+v", cfg)
	}

	given := map[string]string{EnvEnableDestructive: "true"}
	if !LoadFrom(func(name string) string { return given[name] }).Destructive {
		t.Error("LoadFrom did not read the environment it was given")
	}
}
