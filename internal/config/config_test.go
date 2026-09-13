package config

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
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
		require.Equal(t, tc.want, got, "level for %q", tc.in)
		require.Equal(t, tc.recognized, recognized, "recognized for %q", tc.in)
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
			require.Equal(t, tc.want, cfg.Destructive)
			if tc.wantRawIn {
				require.Len(t, cfg.Warnings, 1,
					"an unreadable value must be reported, not silently dropped")
				require.Contains(t, cfg.Warnings[0], tc.env)
			} else {
				require.Empty(t, cfg.Warnings)
			}
		})
	}
}

func TestLoadSkipValidateAndLevel(t *testing.T) {
	t.Setenv(EnvSkipValidate, "")
	t.Setenv(EnvLogLevel, "  ERROR ")
	cfg := Load()
	require.False(t, cfg.SkipValidate)
	require.Equal(t, slog.LevelError, cfg.LogLevel, "the value is trimmed and case-folded")
	require.Empty(t, cfg.Warnings)

	t.Setenv(EnvSkipValidate, "1")
	require.True(t, Load().SkipValidate, "any non-empty value skips validation")
}

// TestWarningsNameTheVariable is what makes an unreadable setting
// findable: Load runs before the logger exists, so the only way a
// person learns their value was ignored is this sentence.
func TestWarningsNameTheVariable(t *testing.T) {
	t.Setenv(EnvLogLevel, "loud")
	t.Setenv(EnvEnableDestructive, "perhaps")

	warnings := Load().Warnings
	require.Len(t, warnings, 2)

	joined := warnings[0] + "\n" + warnings[1]
	require.Contains(t, joined, EnvLogLevel)
	require.Contains(t, joined, "loud")
	require.Contains(t, joined, EnvEnableDestructive)
	require.Contains(t, joined, "perhaps")
	require.Contains(t, joined, "stay unregistered", "the warning has to say what the ignored value cost")
}
