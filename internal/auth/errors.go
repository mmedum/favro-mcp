package auth

import "errors"

// errNotConfigured is returned by Source.Load when the source has no
// credentials configured for this run — env vars unset, or no keyring
// entry. Resolution falls through to the next source.
var errNotConfigured = errors.New("source not configured")
