package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

// resetKeyring switches the keyring lib to its in-memory mock backend
// and clears the favro-mcp service entries so each test starts clean.
// MockInit is idempotent; the deletes are best-effort.
func resetKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	// Best-effort: ignore not-found errors. We don't enumerate entries
	// because the in-memory backend has no list API; tests that wrote
	// non-default keys must clean them up themselves.
	_ = keyring.DeleteAll(keyringService)
	_ = keyring.DeleteAll(keyringActiveService)
}

func TestKeyringSource_Name(t *testing.T) {
	t.Parallel()
	require.Equal(t, "keyring", KeyringSource{}.Name())
}

func TestKeyringSource_RoundTrip(t *testing.T) {
	resetKeyring(t)

	src := KeyringSource{}
	original := Token{Email: "u@e.com", APIToken: "tok-xyz", OrganizationID: "org-1"}

	require.NoError(t, src.Save(context.Background(), original))

	got, err := src.Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, original, got)
}

func TestKeyringSource_Load_NoActivePointer_ReturnsNotConfigured(t *testing.T) {
	resetKeyring(t)

	_, err := KeyringSource{}.Load(context.Background())
	require.ErrorIs(t, err, errNotConfigured,
		"empty keyring must surface errNotConfigured so resolution can fall through")
}

func TestKeyringSource_Load_DanglingPointer_ReturnsNotConfigured(t *testing.T) {
	resetKeyring(t)

	// Active pointer says "u@e.com" but no payload entry exists.
	require.NoError(t, keyring.Set(keyringActiveService, keyringActiveAccount, "u@e.com"))

	_, err := KeyringSource{}.Load(context.Background())
	require.ErrorIs(t, err, errNotConfigured,
		"a pointer with no matching payload should still resolve as not configured")
}

func TestKeyringSource_Load_CorruptPayload_Errors(t *testing.T) {
	resetKeyring(t)

	require.NoError(t, keyring.Set(keyringActiveService, keyringActiveAccount, "u@e.com"))
	require.NoError(t, keyring.Set(keyringService, "u@e.com", "not-json"))

	_, err := KeyringSource{}.Load(context.Background())
	require.Error(t, err)
	require.NotErrorIs(t, err, errNotConfigured,
		"a corrupt payload is a real failure, not a missing source")
}

func TestKeyringSource_Save_RejectsIncompleteToken(t *testing.T) {
	resetKeyring(t)

	err := KeyringSource{}.Save(context.Background(), Token{Email: "u@e.com"})
	var mfe *missingFieldError
	require.ErrorAs(t, err, &mfe, "Save must validate before writing anything")
}

func TestKeyringSource_Delete_Idempotent(t *testing.T) {
	resetKeyring(t)

	// Delete on empty keyring is a no-op.
	require.NoError(t, KeyringSource{}.Delete(context.Background()))

	// After Save then Delete, Load returns errNotConfigured again.
	src := KeyringSource{}
	require.NoError(t, src.Save(context.Background(), Token{
		Email: "u@e.com", APIToken: "t", OrganizationID: "o",
	}))
	require.NoError(t, src.Delete(context.Background()))

	_, err := src.Load(context.Background())
	require.ErrorIs(t, err, errNotConfigured)
}
