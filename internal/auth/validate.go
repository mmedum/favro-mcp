package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// defaultBaseURL is the production Favro REST API base URL.
const defaultBaseURL = "https://favro.com/api/v1"

// defaultValidateTimeout caps the live validation request. Five seconds
// is generous for a single authenticated GET against Favro and short
// enough that a wedged DNS lookup doesn't hold up server startup.
const defaultValidateTimeout = 5 * time.Second

// ErrAuthFailed is returned by Validator.Validate when Favro rejects
// the credentials with 401 or 403. The message names env-var names,
// never values, so it can be logged or shown to an LLM-driven host
// without leaking the token.
var ErrAuthFailed = errors.New("authentication failed — check " + EnvUserEmail + " and " + EnvAPIToken)

// Validator runs the live "is this token good?" check against Favro
// at startup, so a bad token fails fast instead of being discovered
// tool-call by tool-call later. The full Favro client (with retry,
// pagination, caching, redacted debug logging) lands in Phase 2;
// Validator is intentionally minimal — one request, no retry.
type Validator struct {
	// BaseURL overrides the default Favro REST base URL. Tests point
	// it at an httptest.Server; production leaves it empty.
	BaseURL string
	// Client overrides the default *http.Client. Empty means a fresh
	// client with defaultValidateTimeout.
	Client *http.Client
}

// DefaultValidator returns a Validator configured for the production
// Favro API.
func DefaultValidator() *Validator {
	return &Validator{
		BaseURL: defaultBaseURL,
		Client:  newHTTPClient(),
	}
}

// newHTTPClient returns the *http.Client used by both DefaultValidator
// and Validate's nil-Client fallback, so the timeout default lives in
// one place.
func newHTTPClient() *http.Client {
	return &http.Client{Timeout: defaultValidateTimeout}
}

// Validate sends a single GET /organizations request authenticated
// with tok. /organizations is the cheapest authenticated endpoint that
// works on every Favro plan and doesn't require an organizationId
// header to be valid.
//
// Errors:
//   - An error from Token.Validate (*missingFieldError or
//     *invalidFieldError) if tok is incomplete or contains CR/LF — no
//     network call is made.
//   - ErrAuthFailed on 401 or 403 from Favro.
//   - A wrapped error for transport failures or any other status code.
func (v *Validator) Validate(ctx context.Context, tok Token) error {
	if err := tok.Validate(); err != nil {
		return err
	}

	base := v.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	client := v.Client
	if client == nil {
		client = newHTTPClient()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/organizations", http.NoBody)
	if err != nil {
		return fmt.Errorf("build validation request: %w", err)
	}
	tok.Apply(req)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "favro-mcp/auth-validator")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("contact Favro: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrAuthFailed
	default:
		return fmt.Errorf("favro returned HTTP %d when validating credentials", resp.StatusCode)
	}
}
