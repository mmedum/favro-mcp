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

	"github.com/mmedum/favro-mcp/internal/auth"
	"github.com/mmedum/favro-mcp/internal/config"
)

// TestRun_Version_PrintsToStdout pins the discipline that --version
// output goes to stdout (so users can `favro-mcp --version | grep`)
// while every diagnostic goes to stderr.
func TestRun_Version_PrintsToStdout(t *testing.T) {
	t.Parallel()

	stdin := strings.NewReader("")
	var stdout, stderr bytes.Buffer

	if err := run([]string{"--version"}, stdin, &stdout, &stderr); err != nil {
		t.Fatalf("run([]string{\"--version\"}, stdin, &stdout, &stderr): %v", err)
	}

	if !strings.Contains(stdout.String(), "favro-mcp") {
		t.Errorf("stdout.String() does not contain %q", "favro-mcp")
	}
	if len(stderr.String()) != 0 {
		t.Errorf("version flag must not emit diagnostics to stderr: got %v", stderr.String())
	}
}

func TestRun_Help_PrintsToStdout(t *testing.T) {
	t.Parallel()

	stdin := strings.NewReader("")
	var stdout, stderr bytes.Buffer

	if err := run([]string{"--help"}, stdin, &stdout, &stderr); err != nil {
		t.Fatalf("run([]string{\"--help\"}, stdin, &stdout, &stderr): %v", err)
	}

	if !strings.Contains(stdout.String(), "favro-mcp — Model Context Protocol server for Favro") {
		t.Errorf("stdout.String() does not contain %q", "favro-mcp — Model Context Protocol server for Favro")
	}
}

func TestRun_AuthSubcommand_Routed(t *testing.T) {
	t.Parallel()

	stdin := strings.NewReader("")
	var stdout, stderr bytes.Buffer

	// `auth help` should hit the auth dispatcher and emit usage to stderr.
	if err := run([]string{"auth", "help"}, stdin, &stdout, &stderr); err != nil {
		t.Fatalf("run([]string{\"auth\", \"help\"}, stdin, &stdout, &stderr): %v", err)
	}
	if len(stdout.String()) != 0 {
		t.Errorf("auth subcommands must not write to stdout: got %v", stdout.String())
	}
	if !strings.Contains(stderr.String(), "favro-mcp auth") {
		t.Errorf("stderr.String() does not contain %q", "favro-mcp auth")
	}
}

func TestRun_UnknownAuthSubcommand_Errors(t *testing.T) {
	t.Parallel()

	stdin := strings.NewReader("")
	var stdout, stderr bytes.Buffer

	err := run([]string{"auth", "no-such-thing"}, stdin, &stdout, &stderr)
	if err == nil {
		t.Fatal("err should have failed")
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Errorf("stderr.String() does not contain %q", "unknown subcommand")
	}
}

func TestConfigureLogging_UnrecognizedLevel_WarnsOnStderr(t *testing.T) {
	t.Setenv(config.EnvLogLevel, "loud")
	restoreDefaultLogger(t)

	var stderr bytes.Buffer
	configureLogging(&stderr)

	if !strings.Contains(stderr.String(), "unrecognized "+config.EnvLogLevel) {
		t.Errorf("stderr.String() does not contain %q", "unrecognized "+config.EnvLogLevel)
	}
	if !strings.Contains(stderr.String(), "loud") {
		t.Errorf("the rejected value belongs in the warning: %q missing", "loud")
	}
}

// TestConfigureLogging_LevelFromEnv covers the two things the env var
// promises: the value is case- and whitespace-insensitive, and it
// really filters records below the threshold.
func TestConfigureLogging_LevelFromEnv(t *testing.T) {
	t.Setenv(config.EnvLogLevel, "  ERROR ")
	restoreDefaultLogger(t)

	var stderr bytes.Buffer
	configureLogging(&stderr)
	slog.Info("below-threshold")
	slog.Error("at-threshold")

	if strings.Contains(stderr.String(), "below-threshold") {
		t.Errorf("stderr.String() unexpectedly contains %q", "below-threshold")
	}
	if !strings.Contains(stderr.String(), "at-threshold") {
		t.Errorf("stderr.String() does not contain %q", "at-threshold")
	}
	if strings.Contains(stderr.String(), "unrecognized log level") {
		t.Errorf("stderr.String() unexpectedly contains %q", "unrecognized log level")
	}
}

func TestMissingCredsHint_NamesEveryCredentialEnvVar(t *testing.T) {
	t.Parallel()

	hint := missingCredsHint()
	if !strings.Contains(hint, auth.EnvUserEmail) {
		t.Errorf("hint does not contain %q", auth.EnvUserEmail)
	}
	if !strings.Contains(hint, auth.EnvAPIToken) {
		t.Errorf("hint does not contain %q", auth.EnvAPIToken)
	}
	if !strings.Contains(hint, auth.EnvOrganizationID) {
		t.Errorf("hint does not contain %q", auth.EnvOrganizationID)
	}
	if !strings.Contains(hint, "auth login") {
		t.Errorf("the hint must offer the keyring path too: %q missing", "auth login")
	}
}

func TestRunServer_UnknownFlag_ErrorsWithUsage(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	err := runServer([]string{"--no-such-flag"}, config.Load(), io.Discard, &stderr)

	if err == nil {
		t.Fatal("err should have failed")
	}
	if !strings.Contains(stderr.String(), "no-such-flag") {
		t.Errorf("stderr.String() does not contain %q", "no-such-flag")
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("stderr.String() does not contain %q", "Usage:")
	}
}

func TestRunServer_HelpFlag_PrintsUsageWithoutError(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	if err := runServer([]string{"-h"}, config.Load(), io.Discard, &stderr); err != nil {
		t.Fatalf("runServer([]string{\"-h\"}, config.Load(), io.Discard, &stderr): %v", err)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("stderr.String() does not contain %q", "Usage:")
	}
}

func TestRunServer_NoCredentials_ErrorsBeforeContactingFavro(t *testing.T) {
	isolateCredentials(t)
	var logs bytes.Buffer
	captureLogs(t, &logs)

	err := runServer(nil, config.Load(), io.Discard, io.Discard)

	if err == nil {
		t.Fatal("err should have failed")
	}
	if !strings.Contains(logs.String(), "could not resolve Favro credentials") {
		t.Errorf("logs.String() does not contain %q", "could not resolve Favro credentials")
	}
	if !strings.Contains(logs.String(), auth.EnvAPIToken) {
		t.Errorf("the failure must name what to set: %q missing", auth.EnvAPIToken)
	}
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

	err := runServer(nil, config.Load(), io.Discard, io.Discard)

	if err == nil {
		t.Fatal("err should have failed")
	}
	if !strings.Contains(logs.String(), "could not resolve Favro credentials") {
		t.Errorf("logs.String() does not contain %q", "could not resolve Favro credentials")
	}
	if strings.Contains(logs.String(), keyringOnlyCredentials().OrganizationID) {
		t.Errorf("a partial env set must not fall through to the keyring: %q present", keyringOnlyCredentials().OrganizationID)
	}
}

func TestRunServer_DryRunFlag_AnnouncedAtStartup(t *testing.T) {
	isolateCredentials(t)
	var logs bytes.Buffer
	captureLogs(t, &logs)

	// Startup still fails at credential resolution; what matters here is
	// that --dry-run parsed and was announced before that point.
	if runServer([]string{"--dry-run"}, config.Load(), io.Discard, io.Discard) == nil {
		t.Fatal("runServer([]string{\"--dry-run\"}, config.Load(), io.Discard, io.Discard) should have failed")
	}
	if !strings.Contains(logs.String(), "all mutating Favro requests will short-circuit") {
		t.Errorf("logs.String() does not contain %q", "all mutating Favro requests will short-circuit")
	}
}

func TestRun_NoArgs_TakesTheServerPath(t *testing.T) {
	isolateCredentials(t)
	isolateLogging(t)

	stdin := strings.NewReader("")
	var stdout, stderr bytes.Buffer

	err := run(nil, stdin, &stdout, &stderr)

	if err == nil {
		t.Fatal("err should have failed")
	}
	if len(stdout.String()) != 0 {
		t.Errorf("stdout stays reserved for the MCP protocol stream: got %v", stdout.String())
	}
	if !strings.Contains(stderr.String(), "could not resolve Favro credentials") {
		t.Errorf("stderr.String() does not contain %q", "could not resolve Favro credentials")
	}
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
	if !errors.As(err, &exitErr) {
		t.Fatalf("got %v, want exitErr", err)
	}
	if got := exitErr.ExitCode(); got != 1 {
		t.Errorf("exitErr.ExitCode() = %v, want %v", got, 1)
	}
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
	if err := run([]string{"--dump-schemas"}, nil, &stdout, &stderr); err != nil {
		t.Fatalf("run([]string{\"--dump-schemas\"}, nil, &stdout, &stderr): %v", err)
	}

	var dump struct {
		Server string `json:"server"`
		Tools  []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &dump); err != nil {
		t.Fatalf("json.Unmarshal(stdout.Bytes(), &dump): %v", err)
	}
	if got := dump.Server; got != "favro-mcp" {
		t.Errorf("dump.Server = %v, want %v", got, "favro-mcp")
	}

	names := make(map[string]bool, len(dump.Tools))
	for _, tool := range dump.Tools {
		if len(tool.Description) == 0 {
			t.Fatalf("%s has no description", tool.Name)
		}
		if len(tool.InputSchema) == 0 {
			t.Fatalf("%s has no input schema", tool.Name)
		}
		names[tool.Name] = true
	}
	// A read tool, a write tool and a destructive one. The last is the
	// reason this assertion names tools at all: once destructive tools
	// register only behind an environment flag, a dump built the easy
	// way stops listing them and nothing else would say so.
	for _, want := range []string{"favro_ping", "favro_list_cards", "favro_create_card", "favro_delete_card"} {
		if !names[want] {
			t.Errorf("--dump-schemas omits %s", want)
		}
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
			if got := cleanDisconnect(tc.err); got != tc.clean {
				t.Errorf("cleanDisconnect(tc.err) = %v, want %v", got, tc.clean)
			}
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
	t.Setenv(config.EnvSkipValidate, "1")

	// stdin is /dev/null under `go test`, so the stdio transport sees
	// EOF at once and the server shuts down cleanly. The run is only
	// here to get past credential resolution and emit the line.
	if err := runServer(nil, config.Load(), io.Discard, io.Discard); err != nil {
		t.Fatalf("runServer(nil, config.Load(), io.Discard, io.Discard): %v", err)
	}

	out := logs.String()
	if !strings.Contains(out, "favro-mcp starting") {
		t.Errorf("logged nothing, so this test proved nothing: %q missing", "favro-mcp starting")
	}
	if !strings.Contains(out, "credential_source") {
		t.Errorf("the line still has to be worth logging: %q missing", "credential_source")
	}

	if strings.Contains(out, tok.OrganizationID) {
		t.Errorf("the organization id names the tenant: %q present", tok.OrganizationID)
	}
	if strings.Contains(out, tok.Email) {
		t.Errorf("out unexpectedly contains %q", tok.Email)
	}
	if strings.Contains(out, tok.APIToken) {
		t.Errorf("out unexpectedly contains %q", tok.APIToken)
	}
}

// TestUsageDocumentsDestructiveFlag keeps --help honest: a tool surface
// that changes with an environment variable is undiscoverable if the
// variable is not listed.
func TestUsageDocumentsDestructiveFlag(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printUsage(&buf)
	if !strings.Contains(buf.String(), config.EnvEnableDestructive) {
		t.Errorf("buf.String() does not contain %q", config.EnvEnableDestructive)
	}
}
