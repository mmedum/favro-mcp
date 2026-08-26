package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/mmedum/favro-mcp/internal/auth"
)

func TestRunAuth_NoArgs_PrintsUsage(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	require.NoError(t, runAuth(nil, strings.NewReader(""), &stderr))
	require.Contains(t, stderr.String(), "favro-mcp auth login")
}

func TestAuthLogin_StoresInKeyringWithoutEchoingTheToken(t *testing.T) {
	requireNonTTYStdin(t)
	isolateCredentials(t)

	stdin := strings.NewReader(testEmail + "\n" + testAPIToken + "\n" + testOrgID + "\n")
	var stderr bytes.Buffer

	require.NoError(t, runAuth([]string{"login"}, stdin, &stderr))
	require.NotContains(t, stderr.String(), testAPIToken, "auth login must never echo the API token")
	require.Contains(t, stderr.String(), "OS keyring")

	got, err := auth.KeyringSource{}.Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, testCredentials(), got)
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

	require.Error(t, runAuth([]string{"login"}, stdin, &stderr))

	_, err := auth.KeyringSource{}.Load(context.Background())
	require.Error(t, err, "a rejected login must leave the keyring untouched")
}

func TestAuthStatus_ReportsSourceAndOrgButNeverTheToken(t *testing.T) {
	isolateCredentials(t)
	t.Setenv(auth.EnvUserEmail, testEmail)
	t.Setenv(auth.EnvAPIToken, testAPIToken)
	t.Setenv(auth.EnvOrganizationID, testOrgID)

	var stderr bytes.Buffer
	require.NoError(t, authStatus(context.Background(), &stderr))

	require.Contains(t, stderr.String(), "env")
	require.Contains(t, stderr.String(), testOrgID)
	require.NotContains(t, stderr.String(), testAPIToken, "the API token is never displayed, not even partially")
}

func TestAuthStatus_NoCredentials_ErrorsWithHint(t *testing.T) {
	isolateCredentials(t)

	var stderr bytes.Buffer
	require.Error(t, authStatus(context.Background(), &stderr))
	require.Contains(t, stderr.String(), "No Favro credentials configured")
	require.Contains(t, stderr.String(), auth.EnvOrganizationID)
}

func TestAuthWhich_EnvWinsOverKeyring(t *testing.T) {
	isolateCredentials(t)
	saveKeyringToken(t, keyringOnlyCredentials())
	t.Setenv(auth.EnvUserEmail, testEmail)
	t.Setenv(auth.EnvAPIToken, testAPIToken)
	t.Setenv(auth.EnvOrganizationID, testOrgID)

	var stderr bytes.Buffer
	require.NoError(t, authWhich(context.Background(), &stderr))
	require.Equal(t, "env\n", stderr.String())
}

func TestAuthWhich_FallsBackToKeyring(t *testing.T) {
	isolateCredentials(t)
	saveKeyringToken(t, testCredentials())

	var stderr bytes.Buffer
	require.NoError(t, authWhich(context.Background(), &stderr))
	require.Equal(t, "keyring\n", stderr.String())
}

func TestAuthWhich_NoCredentials_Errors(t *testing.T) {
	isolateCredentials(t)

	var stderr bytes.Buffer
	require.Error(t, authWhich(context.Background(), &stderr))
	require.Contains(t, stderr.String(), "no credentials configured")
}

func TestAuthLogout_ClearsKeyringAndIsIdempotent(t *testing.T) {
	isolateCredentials(t)
	saveKeyringToken(t, testCredentials())

	var stderr bytes.Buffer
	require.NoError(t, authLogout(context.Background(), &stderr))
	require.Contains(t, stderr.String(), "Removed keyring entries")

	_, err := auth.KeyringSource{}.Load(context.Background())
	require.Error(t, err)

	// Logging out twice is a no-op, not a failure.
	require.NoError(t, authLogout(context.Background(), io.Discard))
}

func TestPromptLine_TrimsSurroundingWhitespace(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	got, err := promptLine(bufio.NewReader(strings.NewReader("  padded  \n")), &stderr, "email: ")

	require.NoError(t, err)
	require.Equal(t, "padded", got)
	require.Equal(t, "email: ", stderr.String(), "prompts go to stderr, never stdout")
}

// TestPromptLine_UnterminatedFinalLine covers input that ends without a
// newline — ReadString hands back the data alongside io.EOF, which is a
// complete answer rather than a failure.
func TestPromptLine_UnterminatedFinalLine(t *testing.T) {
	t.Parallel()

	got, err := promptLine(bufio.NewReader(strings.NewReader("last")), io.Discard, "")
	require.NoError(t, err)
	require.Equal(t, "last", got)
}

func TestPromptSecret_NonTTY_WarnsThatInputIsVisible(t *testing.T) {
	requireNonTTYStdin(t)

	var stderr bytes.Buffer
	got, err := promptSecret(bufio.NewReader(strings.NewReader(testAPIToken+"\n")), &stderr, "token: ")

	require.NoError(t, err)
	require.Equal(t, testAPIToken, got)
	require.Contains(t, stderr.String(), "not a terminal", "callers must be told masking is off")
}

// TestRunAuth_DispatchesEverySubcommand walks the dispatch table in one
// pass. Order matters: logout runs last so which/status still have a
// stored token to report on.
func TestRunAuth_DispatchesEverySubcommand(t *testing.T) {
	isolateCredentials(t)
	saveKeyringToken(t, testCredentials())

	stdin := strings.NewReader("")
	var stderr bytes.Buffer

	require.NoError(t, runAuth([]string{"which"}, stdin, &stderr))
	require.Contains(t, stderr.String(), "keyring")

	stderr.Reset()
	require.NoError(t, runAuth([]string{"status"}, stdin, &stderr))
	require.Contains(t, stderr.String(), testOrgID)

	stderr.Reset()
	require.NoError(t, runAuth([]string{"logout"}, stdin, &stderr))
	require.Contains(t, stderr.String(), "Removed keyring entries")
}

func TestAuthLogout_KeyringFailure_Surfaces(t *testing.T) {
	isolateCredentials(t)
	keyring.MockInitWithError(errors.New("secret service unavailable"))
	t.Cleanup(keyring.MockInit) // leave a clean mock for later tests

	var stderr bytes.Buffer
	require.Error(t, authLogout(context.Background(), &stderr))
	require.Contains(t, stderr.String(), "Failed to delete keyring entries")
}

// errReader fails every Read so promptLine's non-EOF error branch is
// reachable without a genuinely broken pipe.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("stdin closed") }

func TestPromptLine_ReadFailure_Propagates(t *testing.T) {
	t.Parallel()

	_, err := promptLine(bufio.NewReader(errReader{}), io.Discard, "email: ")
	require.ErrorContains(t, err, "stdin closed")
}
