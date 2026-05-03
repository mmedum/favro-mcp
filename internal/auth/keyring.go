package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const (
	// keyringService is the OS keyring "service" namespace for
	// per-account credential payloads.
	keyringService = "favro-mcp"
	// keyringActiveService + keyringActiveAccount form the pointer
	// entry that tells the server which account to load on startup.
	// The indirection is overkill for v0.1 (single account) but lets
	// a future switch-between-accounts feature land without a
	// storage migration.
	keyringActiveService = "favro-mcp:active"
	keyringActiveAccount = "active"
)

// KeyringSource reads and writes credentials in the OS keyring —
// macOS Keychain, Windows Credential Manager, or Linux Secret Service
// (gnome-keyring / kwallet via D-Bus).
//
// Schema:
//
//	service="favro-mcp:active",  account="active",  value=<email>
//	service="favro-mcp",         account=<email>,   value=JSON({api_token, organization_id})
type KeyringSource struct{}

// keyringPayload is the JSON shape persisted under the per-email entry.
// The user's email lives in the keyring account name, not in this blob.
type keyringPayload struct {
	APIToken       string `json:"api_token"`
	OrganizationID string `json:"organization_id"`
}

// Name returns "keyring".
func (KeyringSource) Name() string { return "keyring" }

// Load resolves the active email from the pointer entry and reads that
// account's payload. Returns errNotConfigured if either entry is
// missing — that's the "user hasn't run `auth login` yet" case.
//
// Errors are intentionally generic — they never include the user's
// email — so a corrupt-keyring failure that surfaces in stderr logs
// can't leak credential identifiers, per the project rule "never log
// FAVRO_USER_EMAIL".
func (KeyringSource) Load(_ context.Context) (Token, error) {
	email, err := getKeyring(keyringActiveService, keyringActiveAccount, "active account")
	if err != nil {
		return Token{}, err
	}
	raw, err := getKeyring(keyringService, email, "active account credentials")
	if err != nil {
		return Token{}, err
	}

	var payload keyringPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return Token{}, fmt.Errorf("parse keyring payload: %w", err)
	}

	tok := Token{
		Email:          email,
		APIToken:       payload.APIToken,
		OrganizationID: payload.OrganizationID,
	}
	if err := tok.Validate(); err != nil {
		return Token{}, err
	}
	return tok, nil
}

// Save persists tok by writing the per-email payload and the
// active-account pointer. The pointer write happens last so a partially
// failed Save leaves the previous active account intact rather than
// pointing at a half-written entry.
func (KeyringSource) Save(_ context.Context, tok Token) error {
	if err := tok.Validate(); err != nil {
		return err
	}
	// gosec G117 flags the "api_token" JSON tag as a secret pattern, but
	// serializing the token to the OS keyring is the entire point of the
	// keyring path — the keyring is the secret store.
	payload, err := json.Marshal(keyringPayload{ //nolint:gosec
		APIToken:       tok.APIToken,
		OrganizationID: tok.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("encode keyring payload: %w", err)
	}
	if err := keyring.Set(keyringService, tok.Email, string(payload)); err != nil {
		return fmt.Errorf("write credentials to keyring: %w", err)
	}
	if err := keyring.Set(keyringActiveService, keyringActiveAccount, tok.Email); err != nil {
		return fmt.Errorf("write active account to keyring: %w", err)
	}
	return nil
}

// Delete removes the active pointer and the active account's payload.
// Idempotent — returns nil if no entries exist. Errors from deleting
// individual entries are joined so a partial cleanup still surfaces.
func (KeyringSource) Delete(_ context.Context) error {
	email, err := keyring.Get(keyringActiveService, keyringActiveAccount)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("read active account from keyring: %w", err)
	}

	var errs []error
	if err := keyring.Delete(keyringService, email); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		errs = append(errs, fmt.Errorf("delete active account credentials from keyring: %w", err))
	}
	if err := keyring.Delete(keyringActiveService, keyringActiveAccount); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		errs = append(errs, fmt.Errorf("delete active account pointer from keyring: %w", err))
	}
	return errors.Join(errs...)
}

// getKeyring reads (service, account) and maps keyring.ErrNotFound to
// errNotConfigured so callers can let resolution fall through.
func getKeyring(service, account, desc string) (string, error) {
	v, err := keyring.Get(service, account)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", errNotConfigured
		}
		return "", fmt.Errorf("read %s from keyring: %w", desc, err)
	}
	return v, nil
}
