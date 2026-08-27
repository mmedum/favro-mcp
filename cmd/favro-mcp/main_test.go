package main

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/auth"
)

// TestRun_Version_PrintsToStdout pins the discipline that --version
// output goes to stdout (so users can `favro-mcp --version | grep`)
// while every diagnostic goes to stderr.
func TestRun_Version_PrintsToStdout(t *testing.T) {
	t.Parallel()

	stdin := strings.NewReader("")
	var stdout, stderr bytes.Buffer

	require.NoError(t, run([]string{"--version"}, stdin, &stdout, &stderr))

	require.Contains(t, stdout.String(), "favro-mcp")
	require.Empty(t, stderr.String(), "version flag must not emit diagnostics to stderr")
}

func TestRun_Help_PrintsToStdout(t *testing.T) {
	t.Parallel()

	stdin := strings.NewReader("")
	var stdout, stderr bytes.Buffer

	require.NoError(t, run([]string{"--help"}, stdin, &stdout, &stderr))

	require.Contains(t, stdout.String(), "favro-mcp — Model Context Protocol server for Favro")
}

func TestRun_AuthSubcommand_Routed(t *testing.T) {
	t.Parallel()

	stdin := strings.NewReader("")
	var stdout, stderr bytes.Buffer

	// `auth help` should hit the auth dispatcher and emit usage to stderr.
	require.NoError(t, run([]string{"auth", "help"}, stdin, &stdout, &stderr))
	require.Empty(t, stdout.String(), "auth subcommands must not write to stdout")
	require.Contains(t, stderr.String(), "favro-mcp auth")
}

func TestRun_UnknownAuthSubcommand_Errors(t *testing.T) {
	t.Parallel()

	stdin := strings.NewReader("")
	var stdout, stderr bytes.Buffer

	err := run([]string{"auth", "no-such-thing"}, stdin, &stdout, &stderr)
	require.Error(t, err)
	require.Contains(t, stderr.String(), "unknown subcommand")
}

func TestParseLogLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		in         string
		want       slog.Level
		recognized bool
	}{
		{"empty defaults to info", "", slog.LevelInfo, true},
		{"info", "info", slog.LevelInfo, true},
		{"debug", "debug", slog.LevelDebug, true},
		{"warn", "warn", slog.LevelWarn, true},
		{"warning alias", "warning", slog.LevelWarn, true},
		{"error", "error", slog.LevelError, true},
		{"unknown falls back to info", "loud", slog.LevelInfo, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, recognized := parseLogLevel(tc.in)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.recognized, recognized)
		})
	}
}

func TestConfigureLogging_UnrecognizedLevel_WarnsOnStderr(t *testing.T) {
	t.Setenv(envLogLevel, "loud")
	restoreDefaultLogger(t)

	var stderr bytes.Buffer
	configureLogging(&stderr)

	require.Contains(t, stderr.String(), "unrecognized log level")
	require.Contains(t, stderr.String(), "loud", "the rejected value belongs in the warning")
}

// TestConfigureLogging_LevelFromEnv covers the two things the env var
// promises: the value is case- and whitespace-insensitive, and it
// really filters records below the threshold.
func TestConfigureLogging_LevelFromEnv(t *testing.T) {
	t.Setenv(envLogLevel, "  ERROR ")
	restoreDefaultLogger(t)

	var stderr bytes.Buffer
	configureLogging(&stderr)
	slog.Info("below-threshold")
	slog.Error("at-threshold")

	require.NotContains(t, stderr.String(), "below-threshold")
	require.Contains(t, stderr.String(), "at-threshold")
	require.NotContains(t, stderr.String(), "unrecognized log level")
}

func TestMissingCredsHint_NamesEveryCredentialEnvVar(t *testing.T) {
	t.Parallel()

	hint := missingCredsHint()
	require.Contains(t, hint, auth.EnvUserEmail)
	require.Contains(t, hint, auth.EnvAPIToken)
	require.Contains(t, hint, auth.EnvOrganizationID)
	require.Contains(t, hint, "auth login", "the hint must offer the keyring path too")
}

func TestRunServer_UnknownFlag_ErrorsWithUsage(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	err := runServer([]string{"--no-such-flag"}, &stderr)

	require.Error(t, err)
	require.Contains(t, stderr.String(), "no-such-flag")
	require.Contains(t, stderr.String(), "Usage:")
}

func TestRunServer_HelpFlag_PrintsUsageWithoutError(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	require.NoError(t, runServer([]string{"-h"}, &stderr))
	require.Contains(t, stderr.String(), "Usage:")
}

func TestRunServer_NoCredentials_ErrorsBeforeContactingFavro(t *testing.T) {
	isolateCredentials(t)
	var logs bytes.Buffer
	captureLogs(t, &logs)

	err := runServer(nil, io.Discard)

	require.Error(t, err)
	require.Contains(t, logs.String(), "could not resolve Favro credentials")
	require.Contains(t, logs.String(), auth.EnvAPIToken, "the failure must name what to set")
}

// TestRunServer_PartialEnvCredentials_Errors pins the env-binding rule:
// a half-filled FAVRO_* set is an operator mistake, so startup fails
// loudly instead of silently falling through to the keyring.
func TestRunServer_PartialEnvCredentials_Errors(t *testing.T) {
	isolateCredentials(t)
	saveKeyringToken(t, keyringOnlyCredentials())
	t.Setenv(auth.EnvUserEmail, "partial@example.test")

	var logs bytes.Buffer
	captureLogs(t, &logs)

	err := runServer(nil, io.Discard)

	require.Error(t, err)
	require.Contains(t, logs.String(), "could not resolve Favro credentials")
	require.NotContains(t, logs.String(), keyringOnlyCredentials().OrganizationID,
		"a partial env set must not fall through to the keyring")
}

func TestRunServer_DryRunFlag_AnnouncedAtStartup(t *testing.T) {
	isolateCredentials(t)
	var logs bytes.Buffer
	captureLogs(t, &logs)

	// Startup still fails at credential resolution; what matters here is
	// that --dry-run parsed and was announced before that point.
	require.Error(t, runServer([]string{"--dry-run"}, io.Discard))
	require.Contains(t, logs.String(), "all mutating Favro requests will short-circuit")
}

func TestRun_NoArgs_TakesTheServerPath(t *testing.T) {
	isolateCredentials(t)
	isolateLogging(t)

	stdin := strings.NewReader("")
	var stdout, stderr bytes.Buffer

	err := run(nil, stdin, &stdout, &stderr)

	require.Error(t, err)
	require.Empty(t, stdout.String(), "stdout stays reserved for the MCP protocol stream")
	require.Contains(t, stderr.String(), "could not resolve Favro credentials")
}

// envRunMainForTest marks the re-executed child process in
// TestMain_ExitsNonZeroOnStartupFailure.
const envRunMainForTest = "FAVRO_MCP_TEST_RUN_MAIN"

// TestMain_ExitsNonZeroOnStartupFailure re-runs this test binary as a
// child so main()'s os.Exit is observable. The exit code is part of the
// CLI contract — a supervisor or wrapper script restarting favro-mcp
// has nothing else to branch on.
//
// The child fails on an unknown flag rather than on missing
// credentials: that path returns before any keyring or network access,
// so the test can't be perturbed by whatever the developer has stored.
func TestMain_ExitsNonZeroOnStartupFailure(t *testing.T) {
	if os.Getenv(envRunMainForTest) != "" {
		os.Args = []string{"favro-mcp", "--no-such-flag"}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMain_ExitsNonZeroOnStartupFailure")
	cmd.Env = append(os.Environ(), envRunMainForTest+"=1")
	err := cmd.Run()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	require.Equal(t, 1, exitErr.ExitCode())
}
