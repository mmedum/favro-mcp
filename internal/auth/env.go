package auth

import (
	"context"
	"os"

	"github.com/mmedum/favro-mcp/v4/internal/config"
)

// Favro env-var names. Exported so cmd/favro-mcp can interpolate them
// into error messages and usage text without restating the strings.
// internal/config owns them, so its registry of variables lists them.
const (
	EnvUserEmail      = config.EnvUserEmail
	EnvAPIToken       = config.EnvAPIToken
	EnvOrganizationID = config.EnvOrganizationID
)

// EnvSource reads credentials from environment variables. Read-only.
//
// All three of FAVRO_USER_EMAIL, FAVRO_API_TOKEN, FAVRO_ORGANIZATION_ID
// must be set. If all three are empty, Load returns errNotConfigured
// so resolution falls through to the keyring. If only some are set,
// Load returns a *missingFieldError so the operator sees "you set X
// but forgot Y" rather than silent fallthrough.
type EnvSource struct {
	// Lookup overrides os.Getenv. Tests inject a deterministic map;
	// production leaves it nil.
	Lookup func(string) string
}

// Name returns "env".
func (EnvSource) Name() string { return "env" }

// Load reads the three FAVRO_* env vars and returns a validated Token.
func (s EnvSource) Load(_ context.Context) (Token, error) {
	get := s.Lookup
	if get == nil {
		get = os.Getenv
	}
	email := config.Lookup(get, EnvUserEmail)
	token := config.Lookup(get, EnvAPIToken)
	orgID := config.Lookup(get, EnvOrganizationID)

	if email == "" && token == "" && orgID == "" {
		return Token{}, errNotConfigured
	}

	tok := Token{Email: email, APIToken: token, OrganizationID: orgID}
	if err := tok.Validate(); err != nil {
		return Token{}, err
	}
	return tok, nil
}
