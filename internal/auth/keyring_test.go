package auth

import (
	"context"
	"errors"
	"testing"

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
	if got := (KeyringSource{}.Name()); got != "keyring" {
		t.Errorf("KeyringSource{}.Name() = %v, want %v", got, "keyring")
	}
}

func TestKeyringSource_RoundTrip(t *testing.T) {
	resetKeyring(t)

	src := KeyringSource{}
	original := Token{Email: "u@example.test", APIToken: "tok-xyz", OrganizationID: "org-1"}

	if err := src.Save(context.Background(), original); err != nil {
		t.Fatalf("src.Save(context.Background(), original): %v", err)
	}

	got, err := src.Load(context.Background())
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got; got != original {
		t.Errorf("got = %v, want %v", got, original)
	}
}

func TestKeyringSource_Load_NoActivePointer_ReturnsNotConfigured(t *testing.T) {
	resetKeyring(t)

	_, err := KeyringSource{}.Load(context.Background())
	if !errors.Is(err, errNotConfigured) {
		t.Fatalf("got %v, want errNotConfigured", err)
	}
}

func TestKeyringSource_Load_DanglingPointer_ReturnsNotConfigured(t *testing.T) {
	resetKeyring(t)

	// Active pointer says "u@example.test" but no payload entry exists.
	if err := keyring.Set(keyringActiveService, keyringActiveAccount, "u@example.test"); err != nil {
		t.Fatalf("keyring.Set(keyringActiveService, keyringActiveAccount, \"u@example.test\"): %v", err)
	}

	_, err := KeyringSource{}.Load(context.Background())
	if !errors.Is(err, errNotConfigured) {
		t.Fatalf("got %v, want errNotConfigured", err)
	}
}

func TestKeyringSource_Load_CorruptPayload_Errors(t *testing.T) {
	resetKeyring(t)

	if err := keyring.Set(keyringActiveService, keyringActiveAccount, "u@example.test"); err != nil {
		t.Fatalf("keyring.Set(keyringActiveService, keyringActiveAccount, \"u@example.test\"): %v", err)
	}
	if err := keyring.Set(keyringService, "u@example.test", "not-json"); err != nil {
		t.Fatalf("keyring.Set(keyringService, \"u@example.test\", \"not-json\"): %v", err)
	}

	_, err := KeyringSource{}.Load(context.Background())
	if err == nil {
		t.Fatal("err should have failed")
	}
	if errors.Is(err, errNotConfigured) {
		t.Fatalf("got %v, want anything but errNotConfigured", err)
	}
}

func TestKeyringSource_Save_RejectsIncompleteToken(t *testing.T) {
	resetKeyring(t)

	err := KeyringSource{}.Save(context.Background(), Token{Email: "u@example.test"})
	var mfe *missingFieldError
	if !errors.As(err, &mfe) {
		t.Fatalf("got %v, want mfe", err)
	}
}

func TestKeyringSource_Delete_Idempotent(t *testing.T) {
	resetKeyring(t)

	// Delete on empty keyring is a no-op.
	if err := (KeyringSource{}.Delete(context.Background())); err != nil {
		t.Fatalf("KeyringSource{}.Delete(context.Background()): %v", err)
	}

	// After Save then Delete, Load returns errNotConfigured again.
	src := KeyringSource{}
	if err := src.Save(context.Background(), Token{
		Email: "u@example.test", APIToken: "t", OrganizationID: "o",
	}); err != nil {
		t.Fatalf("src.Save(context.Background(), Token{\n\tEmail:\t\"u@example.test\", APIToken: \"t\", OrganizationID: \"o\",\n}): %v", err)
	}
	if err := src.Delete(context.Background()); err != nil {
		t.Fatalf("src.Delete(context.Background()): %v", err)
	}

	_, err := src.Load(context.Background())
	if !errors.Is(err, errNotConfigured) {
		t.Fatalf("got %v, want errNotConfigured", err)
	}
}
