package auth

import "context"

// Source is read-only: only KeyringSource has Save / Delete, and they
// live on the concrete type because polymorphic credential writes
// don't make sense (env can't accept them).
//
// Load returns errNotConfigured when the source has nothing to offer
// for this run; any other error is a real failure (corrupt entry,
// dbus down) and resolution stops.
type Source interface {
	// Name is a short identifier for diagnostics ("env", "keyring").
	// Surfaced via `favro-mcp auth which`.
	Name() string

	// Load returns the Token this Source is configured to provide,
	// or errNotConfigured if it isn't configured for this run.
	Load(ctx context.Context) (Token, error)
}
