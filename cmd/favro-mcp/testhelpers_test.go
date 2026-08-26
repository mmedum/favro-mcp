package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
	"golang.org/x/term"

	"github.com/mmedum/favro-mcp/internal/auth"
)

// TestMain installs go-keyring's in-memory mock before any test in this
// package runs. Several tests here reach KeyringSource.Save and .Delete
// through the auth subcommands; without this backstop, one test that
// forgot isolateCredentials would read, overwrite, or wipe the
// developer's real Favro credentials. isolateCredentials still calls
// MockInit per test — that is the per-test reset, this is the guard.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

// Credential values used across these tests are fabricated. Real tenant
// identifiers never appear in this repository — see CLAUDE.md.
const (
	testEmail    = "user@example.test"
	testAPIToken = "tok-not-a-real-secret"
	testOrgID    = "org-test"
)

// testCredentials is the fixture most tests export or store.
func testCredentials() auth.Token {
	return auth.Token{Email: testEmail, APIToken: testAPIToken, OrganizationID: testOrgID}
}

// keyringOnlyCredentials is deliberately distinct from testCredentials
// in every field, so a test can tell a keyring-sourced value from an
// env-sourced one. Keep them different.
func keyringOnlyCredentials() auth.Token {
	return auth.Token{Email: "stored@example.test", APIToken: "tok-stored", OrganizationID: "org-stored"}
}

// isolateCredentials makes credential resolution deterministic: the
// three FAVRO_* vars are blanked so a developer's real environment
// can't leak into a test, and the keyring is pointed at go-keyring's
// in-memory mock. MockInit installs a fresh empty store on every call,
// so each test starts with nothing saved.
//
// t.Setenv means callers cannot be parallel. That is fine — the
// parallel tests in this package touch neither env nor keyring, and Go
// finishes every sequential test (restoring env via t.Setenv's cleanup)
// before resuming parallel bodies.
func isolateCredentials(t *testing.T) {
	t.Helper()
	t.Setenv(auth.EnvUserEmail, "")
	t.Setenv(auth.EnvAPIToken, "")
	t.Setenv(auth.EnvOrganizationID, "")
	keyring.MockInit()
}

// saveKeyringToken stores tok in the mocked keyring through the same
// public path `favro-mcp auth login` uses.
func saveKeyringToken(t *testing.T, tok auth.Token) {
	t.Helper()
	require.NoError(t, auth.KeyringSource{}.Save(context.Background(), tok))
}

// restoreDefaultLogger puts slog's process-wide default back when the
// test ends. configureLogging replaces it by design.
func restoreDefaultLogger(t *testing.T) {
	t.Helper()
	orig := slog.Default()
	t.Cleanup(func() { slog.SetDefault(orig) })
}

// isolateLogging pins FAVRO_LOG_LEVEL to debug so assertions don't
// depend on whatever the developer happens to export, and restores the
// default logger afterwards.
func isolateLogging(t *testing.T) {
	t.Helper()
	t.Setenv(envLogLevel, "debug")
	restoreDefaultLogger(t)
}

// captureLogs routes every slog record into buf. runServer and the auth
// subcommands report through slog, so this is how their diagnostics
// become assertable. Sequential tests only — buf is not goroutine-safe
// and slog's default is process-wide.
func captureLogs(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	isolateLogging(t)
	configureLogging(buf)
}

// requireNonTTYStdin skips when the test binary is attached to a real
// terminal. promptSecret reads masked input straight from stdin's fd in
// that case and would block forever. `go test` wires stdin to
// os.DevNull, so this only trips when someone runs the compiled test
// binary by hand.
func requireNonTTYStdin(t *testing.T) {
	t.Helper()
	if term.IsTerminal(int(os.Stdin.Fd())) {
		t.Skip("stdin is a terminal; promptSecret would block on masked input")
	}
}
