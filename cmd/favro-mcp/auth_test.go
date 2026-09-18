package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/mmedum/favro-mcp/internal/auth"
)

func TestRunAuth_NoArgs_PrintsUsage(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	if err := runAuth(nil, strings.NewReader(""), &stderr); err != nil {
		t.Fatalf("runAuth(nil, strings.NewReader(\"\"), &stderr): %v", err)
	}
	if !strings.Contains(stderr.String(), "favro-mcp auth login") {
		t.Errorf("stderr.String() does not contain %q", "favro-mcp auth login")
	}
}

func TestAuthLogin_StoresInKeyringWithoutEchoingTheToken(t *testing.T) {
	requireNonTTYStdin(t)
	isolateCredentials(t)

	stdin := strings.NewReader(testEmail + "\n" + testAPIToken + "\n" + testOrgID + "\n")
	var stderr bytes.Buffer

	if err := runAuth([]string{"login"}, stdin, &stderr); err != nil {
		t.Fatalf("runAuth([]string{\"login\"}, stdin, &stderr): %v", err)
	}
	if strings.Contains(stderr.String(), testAPIToken) {
		t.Errorf("auth login must never echo the API token: %q present", testAPIToken)
	}
	if !strings.Contains(stderr.String(), "OS keyring") {
		t.Errorf("stderr.String() does not contain %q", "OS keyring")
	}

	got, err := auth.KeyringSource{}.Load(context.Background())
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got; got != testCredentials() {
		t.Errorf("got = %v, want %v", got, testCredentials())
	}
}

// TestAuthLogin_RejectsIncompleteInput leans on Token.Validate inside
// KeyringSource.Save — cmd deliberately does not pre-validate, so this
// pins that nothing half-formed reaches the keyring.
func TestAuthLogin_RejectsIncompleteInput(t *testing.T) {
	requireNonTTYStdin(t)
	isolateCredentials(t)

	// Email and token supplied, organization id left blank.
	stdin := strings.NewReader(testEmail + "\n" + testAPIToken + "\n\n")
	var stderr bytes.Buffer

	if runAuth([]string{"login"}, stdin, &stderr) == nil {
		t.Fatal("runAuth([]string{\"login\"}, stdin, &stderr) should have failed")
	}

	_, err := auth.KeyringSource{}.Load(context.Background())
	if err == nil {
		t.Fatal("a rejected login must leave the keyring untouched")
	}
}

func TestAuthStatus_ReportsSourceAndOrgButNeverTheToken(t *testing.T) {
	isolateCredentials(t)
	t.Setenv(auth.EnvUserEmail, testEmail)
	t.Setenv(auth.EnvAPIToken, testAPIToken)
	t.Setenv(auth.EnvOrganizationID, testOrgID)

	var stderr bytes.Buffer
	if err := authStatus(context.Background(), &stderr); err != nil {
		t.Fatalf("authStatus(context.Background(), &stderr): %v", err)
	}

	if !strings.Contains(stderr.String(), "env") {
		t.Errorf("stderr.String() does not contain %q", "env")
	}
	if !strings.Contains(stderr.String(), testOrgID) {
		t.Errorf("stderr.String() does not contain %q", testOrgID)
	}
	if strings.Contains(stderr.String(), testAPIToken) {
		t.Errorf("the API token is never displayed, not even partially: %q present", testAPIToken)
	}
}

func TestAuthStatus_NoCredentials_ErrorsWithHint(t *testing.T) {
	isolateCredentials(t)

	var stderr bytes.Buffer
	if authStatus(context.Background(), &stderr) == nil {
		t.Fatal("authStatus(context.Background(), &stderr) should have failed")
	}
	if !strings.Contains(stderr.String(), "No Favro credentials configured") {
		t.Errorf("stderr.String() does not contain %q", "No Favro credentials configured")
	}
	if !strings.Contains(stderr.String(), auth.EnvOrganizationID) {
		t.Errorf("stderr.String() does not contain %q", auth.EnvOrganizationID)
	}
}

func TestAuthWhich_EnvWinsOverKeyring(t *testing.T) {
	isolateCredentials(t)
	saveKeyringToken(t, keyringOnlyCredentials())
	t.Setenv(auth.EnvUserEmail, testEmail)
	t.Setenv(auth.EnvAPIToken, testAPIToken)
	t.Setenv(auth.EnvOrganizationID, testOrgID)

	var stderr bytes.Buffer
	if err := authWhich(context.Background(), &stderr); err != nil {
		t.Fatalf("authWhich(context.Background(), &stderr): %v", err)
	}
	if got := stderr.String(); got != "env\n" {
		t.Errorf("stderr.String() = %v, want %v", got, "env\n")
	}
}

func TestAuthWhich_FallsBackToKeyring(t *testing.T) {
	isolateCredentials(t)
	saveKeyringToken(t, testCredentials())

	var stderr bytes.Buffer
	if err := authWhich(context.Background(), &stderr); err != nil {
		t.Fatalf("authWhich(context.Background(), &stderr): %v", err)
	}
	if got := stderr.String(); got != "keyring\n" {
		t.Errorf("stderr.String() = %v, want %v", got, "keyring\n")
	}
}

func TestAuthWhich_NoCredentials_Errors(t *testing.T) {
	isolateCredentials(t)

	var stderr bytes.Buffer
	if authWhich(context.Background(), &stderr) == nil {
		t.Fatal("authWhich(context.Background(), &stderr) should have failed")
	}
	if !strings.Contains(stderr.String(), "no credentials configured") {
		t.Errorf("stderr.String() does not contain %q", "no credentials configured")
	}
}

func TestAuthLogout_ClearsKeyringAndIsIdempotent(t *testing.T) {
	isolateCredentials(t)
	saveKeyringToken(t, testCredentials())

	var stderr bytes.Buffer
	if err := authLogout(context.Background(), &stderr); err != nil {
		t.Fatalf("authLogout(context.Background(), &stderr): %v", err)
	}
	if !strings.Contains(stderr.String(), "Removed keyring entries") {
		t.Errorf("stderr.String() does not contain %q", "Removed keyring entries")
	}

	_, err := auth.KeyringSource{}.Load(context.Background())
	if err == nil {
		t.Fatal("err should have failed")
	}

	// Logging out twice is a no-op, not a failure.
	if err := authLogout(context.Background(), io.Discard); err != nil {
		t.Fatalf("authLogout(context.Background(), io.Discard): %v", err)
	}
}

func TestPromptLine_TrimsSurroundingWhitespace(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	got, err := promptLine(bufio.NewReader(strings.NewReader("  padded  \n")), &stderr, "email: ")

	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got; got != "padded" {
		t.Errorf("got = %v, want %v", got, "padded")
	}
	if got := stderr.String(); got != "email: " {
		t.Errorf("prompts go to stderr, never stdout: got %v, want %v", got, "email: ")
	}
}

// TestPromptLine_UnterminatedFinalLine covers input that ends without a
// newline — ReadString hands back the data alongside io.EOF, which is a
// complete answer rather than a failure.
func TestPromptLine_UnterminatedFinalLine(t *testing.T) {
	t.Parallel()

	got, err := promptLine(bufio.NewReader(strings.NewReader("last")), io.Discard, "")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got; got != "last" {
		t.Errorf("got = %v, want %v", got, "last")
	}
}

func TestPromptSecret_NonTTY_WarnsThatInputIsVisible(t *testing.T) {
	requireNonTTYStdin(t)

	var stderr bytes.Buffer
	got, err := promptSecret(bufio.NewReader(strings.NewReader(testAPIToken+"\n")), &stderr, "token: ")

	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got; got != testAPIToken {
		t.Errorf("got = %v, want %v", got, testAPIToken)
	}
	if !strings.Contains(stderr.String(), "not a terminal") {
		t.Errorf("callers must be told masking is off: %q missing", "not a terminal")
	}
}

// TestRunAuth_DispatchesEverySubcommand walks the dispatch table in one
// pass. Order matters: logout runs last so which/status still have a
// stored token to report on.
func TestRunAuth_DispatchesEverySubcommand(t *testing.T) {
	isolateCredentials(t)
	saveKeyringToken(t, testCredentials())

	stdin := strings.NewReader("")
	var stderr bytes.Buffer

	if err := runAuth([]string{"which"}, stdin, &stderr); err != nil {
		t.Fatalf("runAuth([]string{\"which\"}, stdin, &stderr): %v", err)
	}
	if !strings.Contains(stderr.String(), "keyring") {
		t.Errorf("stderr.String() does not contain %q", "keyring")
	}

	stderr.Reset()
	if err := runAuth([]string{"status"}, stdin, &stderr); err != nil {
		t.Fatalf("runAuth([]string{\"status\"}, stdin, &stderr): %v", err)
	}
	if !strings.Contains(stderr.String(), testOrgID) {
		t.Errorf("stderr.String() does not contain %q", testOrgID)
	}

	stderr.Reset()
	if err := runAuth([]string{"logout"}, stdin, &stderr); err != nil {
		t.Fatalf("runAuth([]string{\"logout\"}, stdin, &stderr): %v", err)
	}
	if !strings.Contains(stderr.String(), "Removed keyring entries") {
		t.Errorf("stderr.String() does not contain %q", "Removed keyring entries")
	}
}

func TestAuthLogout_KeyringFailure_Surfaces(t *testing.T) {
	isolateCredentials(t)
	keyring.MockInitWithError(errors.New("secret service unavailable"))
	t.Cleanup(keyring.MockInit) // leave a clean mock for later tests

	var stderr bytes.Buffer
	if authLogout(context.Background(), &stderr) == nil {
		t.Fatal("authLogout(context.Background(), &stderr) should have failed")
	}
	if !strings.Contains(stderr.String(), "Failed to delete keyring entries") {
		t.Errorf("stderr.String() does not contain %q", "Failed to delete keyring entries")
	}
}

// errReader fails every Read so promptLine's non-EOF error branch is
// reachable without a genuinely broken pipe.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("stdin closed") }

func TestPromptLine_ReadFailure_Propagates(t *testing.T) {
	t.Parallel()

	_, err := promptLine(bufio.NewReader(errReader{}), io.Discard, "email: ")
	if err == nil || !strings.Contains(err.Error(), "stdin closed") {
		t.Fatalf("got %v, want it to mention %q", err, "stdin closed")
	}
}

// trackingReader records whether anything was read from it. A help path
// must not touch stdin at all: `auth login --help` consuming a line of
// redirected input is the defect these tests pin.
type trackingReader struct{ read bool }

func (r *trackingReader) Read([]byte) (int, error) {
	r.read = true
	return 0, io.EOF
}

// TestRunAuth_HelpNeverActs pins what a help token has to do everywhere
// in the auth subtree: print usage, return nil, read no stdin, and
// leave the stored credentials alone. The tokens used to reach only
// `auth --help`; after a subcommand they fell through to the
// subcommand itself, so `auth login --help` prompted (reading whatever
// stdin was redirected from) and `auth logout --help` deleted the
// keyring entries.
func TestRunAuth_HelpNeverActs(t *testing.T) {
	for _, sub := range []string{"login", "status", "logout", "which"} {
		for _, token := range []string{"help", "--help", "-h"} {
			t.Run(sub+"_"+token, func(t *testing.T) {
				isolateCredentials(t)
				saveKeyringToken(t, testCredentials())

				stdin := &trackingReader{}
				var stderr bytes.Buffer

				if err := runAuth([]string{sub, token}, stdin, &stderr); err != nil {
					t.Fatalf("runAuth([]string{%q, %q}, stdin, &stderr): %v", sub, token, err)
				}
				if stdin.read {
					t.Error("help read from stdin; it must print usage without consuming input")
				}
				if !strings.Contains(stderr.String(), "favro-mcp auth login") {
					t.Errorf("stderr.String() does not contain %q", "favro-mcp auth login")
				}
				if _, err := (auth.KeyringSource{}).Load(context.Background()); err != nil {
					t.Errorf("help must leave stored credentials untouched: %v", err)
				}
			})
		}
	}
}

// TestRunAuth_HelpTokenAlone_PrintsUsage keeps `auth help` / `auth
// --help` / `auth -h` working now that the dispatch switch no longer
// carries a case for them.
func TestRunAuth_HelpTokenAlone_PrintsUsage(t *testing.T) {
	t.Parallel()

	for _, token := range []string{"help", "--help", "-h"} {
		t.Run(token, func(t *testing.T) {
			t.Parallel()

			stdin := &trackingReader{}
			var stderr bytes.Buffer

			if err := runAuth([]string{token}, stdin, &stderr); err != nil {
				t.Fatalf("runAuth([]string{%q}, stdin, &stderr): %v", token, err)
			}
			if stdin.read {
				t.Error("help read from stdin; it must print usage without consuming input")
			}
			if !strings.Contains(stderr.String(), "favro-mcp auth login") {
				t.Errorf("stderr.String() does not contain %q", "favro-mcp auth login")
			}
		})
	}
}
