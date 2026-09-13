package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
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
	err := runServer([]string{"--no-such-flag"}, io.Discard, &stderr)

	require.Error(t, err)
	require.Contains(t, stderr.String(), "no-such-flag")
	require.Contains(t, stderr.String(), "Usage:")
}

func TestRunServer_HelpFlag_PrintsUsageWithoutError(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	require.NoError(t, runServer([]string{"-h"}, io.Discard, &stderr))
	require.Contains(t, stderr.String(), "Usage:")
}

func TestRunServer_NoCredentials_ErrorsBeforeContactingFavro(t *testing.T) {
	isolateCredentials(t)
	var logs bytes.Buffer
	captureLogs(t, &logs)

	err := runServer(nil, io.Discard, io.Discard)

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

	err := runServer(nil, io.Discard, io.Discard)

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
	require.Error(t, runServer([]string{"--dry-run"}, io.Discard, io.Discard))
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

// --dump-schemas is what the schema-diff and staleness gates read, and
// both run in CI where there is no keyring and no token. It also has to
// show the whole surface: a dump that omitted a tool would hide exactly
// the change — a tool disappearing — that the diff exists to catch.
func TestDumpSchemasNeedsNoCredentials(t *testing.T) {
	t.Setenv(auth.EnvUserEmail, "")
	t.Setenv(auth.EnvAPIToken, "")
	t.Setenv(auth.EnvOrganizationID, "")

	var stdout, stderr bytes.Buffer
	require.NoError(t, run([]string{"--dump-schemas"}, nil, &stdout, &stderr))

	var dump struct {
		Server string `json:"server"`
		Tools  []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &dump))
	require.Equal(t, "favro-mcp", dump.Server)

	names := make(map[string]bool, len(dump.Tools))
	for _, tool := range dump.Tools {
		require.NotEmpty(t, tool.Description, "%s has no description", tool.Name)
		require.NotEmpty(t, tool.InputSchema, "%s has no input schema", tool.Name)
		names[tool.Name] = true
	}
	// A read tool, a write tool and a destructive one. The last is the
	// reason this assertion names tools at all: once destructive tools
	// register only behind an environment flag, a dump built the easy
	// way stops listing them and nothing else would say so.
	for _, want := range []string{"favro_ping", "favro_list_cards", "favro_create_card", "favro_delete_card"} {
		require.True(t, names[want], "--dump-schemas omits %s", want)
	}
}

// cleanDisconnect decides whether this process exits 0 or 1, and it got
// that wrong for every ordinary disconnect until the smoke gate caught
// it: the SDK reports a closed stdio session as JSON-RPC -32004 with the
// EOF only as message text, so errors.Is(err, io.EOF) never matched and
// every host logged an ordinary shutdown as a crash.
func TestCleanDisconnect(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		err   error
		clean bool
	}{
		{"a server that stopped on its own", nil, true},
		{"the context was cancelled", context.Canceled, true},
		{"a wrapped cancellation", fmt.Errorf("run: %w", context.Canceled), true},
		{"a plain EOF", io.EOF, true},
		{"the server closing, as the SDK reports it", &jsonrpc.Error{Code: -32004, Message: "server is closing"}, true},
		{"the client closing", &jsonrpc.Error{Code: -32003, Message: "client is closing"}, true},
		{"the server closing, wrapped", fmt.Errorf("run: %w", &jsonrpc.Error{Code: -32004}), true},
		// Everything else still has to reach the exit code. A protocol
		// error that is not a disconnect is a real failure, and so is
		// anything the transport reports.
		{"a JSON-RPC error that is not a disconnect", &jsonrpc.Error{Code: -32600, Message: "invalid request"}, false},
		{"an ordinary failure", errors.New("the transport broke"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.clean, cleanDisconnect(tc.err))
		})
	}
}

// TestStartupLineNamesNoTenant is the second half of what §9 says A2
// owes. The startup line logged the organization id at INFO, which
// named the tenant in the first line of every session — not at debug,
// not behind a flag, in every log anyone has ever collected from this
// server.
//
// The assertion is over everything that was logged, not over the one
// line: the rule is that no forbidden value reaches a log at any
// level.
func TestStartupLineNamesNoTenant(t *testing.T) {
	requireNonTTYStdin(t)
	isolateCredentials(t)
	var logs bytes.Buffer
	captureLogs(t, &logs)

	tok := testCredentials()
	t.Setenv(auth.EnvUserEmail, tok.Email)
	t.Setenv(auth.EnvAPIToken, tok.APIToken)
	t.Setenv(auth.EnvOrganizationID, tok.OrganizationID)
	t.Setenv(envSkipValidate, "1")

	// stdin is /dev/null under `go test`, so the stdio transport sees
	// EOF at once and the server shuts down cleanly. The run is only
	// here to get past credential resolution and emit the line.
	require.NoError(t, runServer(nil, io.Discard, io.Discard))

	out := logs.String()
	require.Contains(t, out, "favro-mcp starting",
		"logged nothing, so this test proved nothing")
	require.Contains(t, out, "credential_source", "the line still has to be worth logging")

	require.NotContains(t, out, tok.OrganizationID, "the organization id names the tenant")
	require.NotContains(t, out, tok.Email)
	require.NotContains(t, out, tok.APIToken)
}

// TestDestructiveEnabled pins the parse. The default and every
// unreadable value mean off: a typo in the variable that enables
// deletes must not enable deletes.
func TestDestructiveEnabled(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want bool
	}{
		{"unset", "", false}, // unset and empty are the same to os.Getenv
		{"true", "true", true},
		{"upper", "TRUE", true},
		{"one", "1", true},
		{"false", "false", false},
		{"yes", "yes", false},
		{"padded", "  true ", false}, // ParseBool does not trim, and neither do we
	}

	for _, tc := range cases {
		env, want := tc.env, tc.want
		t.Run(tc.name, func(t *testing.T) {
			restoreDefaultLogger(t)
			configureLogging(io.Discard)
			t.Setenv(envEnableDestructive, env)
			require.Equal(t, want, destructiveEnabled())
		})
	}
}

// TestUsageDocumentsDestructiveFlag keeps --help honest: a tool surface
// that changes with an environment variable is undiscoverable if the
// variable is not listed.
func TestUsageDocumentsDestructiveFlag(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printUsage(&buf)
	require.Contains(t, buf.String(), envEnableDestructive)
}
