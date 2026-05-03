// Package auth resolves and validates Favro credentials.
//
// Where credentials come from (Source) is separated from what they are
// (Token). v0.1 ships two Source implementations — environment
// variables and the OS keyring — and the abstraction lets a future
// OAuth flow plug in without changing the HTTP client or the MCP
// tools.
package auth

import (
	"errors"
	"net/http"
	"strings"
)

// headerOrganizationID is the Favro REST header carrying the
// organizationId on every authenticated request.
const headerOrganizationID = "organizationId"

// Token is a fully resolved set of Favro credentials. APIToken is a
// secret — it must never appear in tool responses, error messages, or
// log lines.
//
// favro-mcp is single-organization for v0.1: OrganizationID is fixed
// at startup and tools never accept it as input.
type Token struct {
	Email          string
	APIToken       string
	OrganizationID string
}

// Apply sets HTTP Basic Auth credentials and the organizationId header
// on req. Safe to call repeatedly on the same request.
func (t Token) Apply(req *http.Request) {
	req.SetBasicAuth(t.Email, t.APIToken)
	if t.OrganizationID != "" {
		req.Header.Set(headerOrganizationID, t.OrganizationID)
	}
}

// Validate checks that every required field has non-whitespace content
// and contains no CR/LF (which would smuggle into HTTP headers — the
// stdlib catches it at request-write time, but failing early gives a
// clearer diagnostic and protects future field sources). Does not
// contact Favro — see Validator in validate.go for the network check.
func (t Token) Validate() error {
	var missing, invalid []string
	for _, f := range []struct {
		name  string
		value string
	}{
		{"email", t.Email},
		{"API token", t.APIToken},
		{"organization id", t.OrganizationID},
	} {
		if strings.TrimSpace(f.value) == "" {
			missing = append(missing, f.name)
			continue
		}
		if strings.ContainsAny(f.value, "\r\n") {
			invalid = append(invalid, f.name)
		}
	}
	if len(missing) > 0 {
		return &missingFieldError{fields: missing}
	}
	if len(invalid) > 0 {
		return &invalidFieldError{fields: invalid}
	}
	return nil
}

// missingFieldError is returned by Token.Validate when one or more
// required credential fields are empty. Internal-only — callers
// outside the package see it as a plain error.
type missingFieldError struct {
	fields []string
}

func (e *missingFieldError) Error() string {
	return "missing credential field(s): " + strings.Join(e.fields, ", ")
}

// invalidFieldError is returned by Token.Validate when a credential
// field contains CR/LF characters that would otherwise smuggle into
// HTTP headers.
type invalidFieldError struct {
	fields []string
}

func (e *invalidFieldError) Error() string {
	return "invalid credential field(s) — contains CR/LF: " + strings.Join(e.fields, ", ")
}

// errNoCredentials is returned by resolveToken when no Source produced
// a usable Token.
var errNoCredentials = errors.New("no Favro credentials found in environment or keyring")
