package auth

import "context"

// EnvSource offers the credentials the environment sets. Its caller
// reads them, through config.CredentialsFrom, so that this package
// imports nothing of the server's and every variable is read in one
// place.
//
// All three of the email, the API token and the organization id must be
// set. If all three are empty, Load returns errNotConfigured so
// resolution falls through to the keyring. If only some are set, Load
// returns a *missingFieldError so the operator sees "you set X but
// forgot Y" rather than silent fallthrough.
type EnvSource struct {
	Token Token
}

// Name returns "env".
func (EnvSource) Name() string { return "env" }

// Load returns the credentials the environment set, validated.
func (s EnvSource) Load(_ context.Context) (Token, error) {
	if s.Token == (Token{}) {
		return Token{}, errNotConfigured
	}
	if err := s.Token.Validate(); err != nil {
		return Token{}, err
	}
	return s.Token, nil
}
