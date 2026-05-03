package auth

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToken_Apply_SetsBasicAuthAndOrgHeader(t *testing.T) {
	t.Parallel()

	tok := Token{Email: "user@example.com", APIToken: "tok-xyz", OrganizationID: "org-1"}
	req, err := http.NewRequest(http.MethodGet, "https://example.com", http.NoBody)
	require.NoError(t, err)

	tok.Apply(req)

	user, pass, ok := req.BasicAuth()
	require.True(t, ok, "Apply must set Basic Auth")
	require.Equal(t, "user@example.com", user)
	require.Equal(t, "tok-xyz", pass)
	require.Equal(t, "org-1", req.Header.Get("organizationId"))
}

func TestToken_Apply_OmitsEmptyOrgID(t *testing.T) {
	t.Parallel()

	tok := Token{Email: "u@e.com", APIToken: "t"}
	req, err := http.NewRequest(http.MethodGet, "https://example.com", http.NoBody)
	require.NoError(t, err)

	tok.Apply(req)

	require.Empty(t, req.Header.Get("organizationId"),
		"empty OrganizationID must not produce an empty header")
}

func TestToken_Validate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		tok     Token
		wantErr bool
		missing []string
	}{
		{
			name: "all fields present",
			tok:  Token{Email: "u@e.com", APIToken: "t", OrganizationID: "o"},
		},
		{
			name:    "all fields missing",
			tok:     Token{},
			wantErr: true,
			missing: []string{"email", "API token", "organization id"},
		},
		{
			name:    "missing email only",
			tok:     Token{APIToken: "t", OrganizationID: "o"},
			wantErr: true,
			missing: []string{"email"},
		},
		{
			name:    "missing token only",
			tok:     Token{Email: "u@e.com", OrganizationID: "o"},
			wantErr: true,
			missing: []string{"API token"},
		},
		{
			name:    "missing org only",
			tok:     Token{Email: "u@e.com", APIToken: "t"},
			wantErr: true,
			missing: []string{"organization id"},
		},
		{
			name:    "whitespace email is treated as missing",
			tok:     Token{Email: "   ", APIToken: "t", OrganizationID: "o"},
			wantErr: true,
			missing: []string{"email"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.tok.Validate()
			if !tc.wantErr {
				require.NoError(t, err)
				return
			}
			var mfe *missingFieldError
			require.ErrorAs(t, err, &mfe, "expected *missingFieldError")
			require.Equal(t, tc.missing, mfe.fields)
		})
	}
}

func TestMissingFieldError_Message(t *testing.T) {
	t.Parallel()

	err := &missingFieldError{fields: []string{"email", "API token"}}
	msg := err.Error()
	require.Contains(t, msg, "email", "message must name 'email'")
	require.Contains(t, msg, "API token", "message must name 'API token'")
}

func TestToken_Validate_RejectsCRLF(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		tok  Token
		want []string
	}{
		{
			name: "CR in email",
			tok:  Token{Email: "u@e.com\r", APIToken: "t", OrganizationID: "o"},
			want: []string{"email"},
		},
		{
			name: "LF in API token",
			tok:  Token{Email: "u@e.com", APIToken: "tok\nevil", OrganizationID: "o"},
			want: []string{"API token"},
		},
		{
			name: "CRLF in organization id",
			tok:  Token{Email: "u@e.com", APIToken: "t", OrganizationID: "o\r\nX-Injected: 1"},
			want: []string{"organization id"},
		},
		{
			name: "multiple bad fields",
			tok:  Token{Email: "u\r@e.com", APIToken: "t\nx", OrganizationID: "o"},
			want: []string{"email", "API token"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.tok.Validate()
			var ife *invalidFieldError
			require.ErrorAs(t, err, &ife, "expected *invalidFieldError")
			require.Equal(t, tc.want, ife.fields)
		})
	}
}
