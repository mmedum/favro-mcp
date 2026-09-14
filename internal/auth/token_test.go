package auth

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestToken_Apply_SetsBasicAuthAndOrgHeader(t *testing.T) {
	t.Parallel()

	tok := Token{Email: "user@example.com", APIToken: "tok-xyz", OrganizationID: "org-1"}
	req, err := http.NewRequest(http.MethodGet, "https://example.com", http.NoBody)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	tok.Apply(req)

	user, pass, ok := req.BasicAuth()
	if !ok {
		t.Error("Apply must set Basic Auth")
	}
	if got := user; got != "user@example.com" {
		t.Errorf("user = %v, want %v", got, "user@example.com")
	}
	if got := pass; got != "tok-xyz" {
		t.Errorf("pass = %v, want %v", got, "tok-xyz")
	}
	if got := req.Header.Get("organizationId"); got != "org-1" {
		t.Errorf("req.Header.Get(\"organizationId\") = %v, want %v", got, "org-1")
	}
}

func TestToken_Apply_OmitsEmptyOrgID(t *testing.T) {
	t.Parallel()

	tok := Token{Email: "u@example.test", APIToken: "t"}
	req, err := http.NewRequest(http.MethodGet, "https://example.com", http.NoBody)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	tok.Apply(req)

	if len(req.Header.Get("organizationId")) != 0 {
		t.Errorf("empty OrganizationID must not produce an empty header: got %v", req.Header.Get("organizationId"))
	}
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
			tok:  Token{Email: "u@example.test", APIToken: "t", OrganizationID: "o"},
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
			tok:     Token{Email: "u@example.test", OrganizationID: "o"},
			wantErr: true,
			missing: []string{"API token"},
		},
		{
			name:    "missing org only",
			tok:     Token{Email: "u@example.test", APIToken: "t"},
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
				if err := err; err != nil {
					t.Fatalf("err: %v", err)
				}
				return
			}
			var mfe *missingFieldError
			if !errors.As(err, &mfe) {
				t.Fatalf("got %v, want mfe", err)
			}
			if got := mfe.fields; !reflect.DeepEqual(got, tc.missing) {
				t.Errorf("mfe.fields = %v, want %v", got, tc.missing)
			}
		})
	}
}

func TestMissingFieldError_Message(t *testing.T) {
	t.Parallel()

	err := &missingFieldError{fields: []string{"email", "API token"}}
	msg := err.Error()
	if !strings.Contains(msg, "email") {
		t.Errorf("message must name 'email': %q missing", "email")
	}
	if !strings.Contains(msg, "API token") {
		t.Errorf("message must name 'API token': %q missing", "API token")
	}
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
			tok:  Token{Email: "u@example.test\r", APIToken: "t", OrganizationID: "o"},
			want: []string{"email"},
		},
		{
			name: "LF in API token",
			tok:  Token{Email: "u@example.test", APIToken: "tok\nevil", OrganizationID: "o"},
			want: []string{"API token"},
		},
		{
			name: "CRLF in organization id",
			tok:  Token{Email: "u@example.test", APIToken: "t", OrganizationID: "o\r\nX-Injected: 1"},
			want: []string{"organization id"},
		},
		{
			name: "multiple bad fields",
			tok:  Token{Email: "u\r@example.test", APIToken: "t\nx", OrganizationID: "o"},
			want: []string{"email", "API token"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.tok.Validate()
			var ife *invalidFieldError
			if !errors.As(err, &ife) {
				t.Fatalf("got %v, want ife", err)
			}
			if got := ife.fields; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ife.fields = %v, want %v", got, tc.want)
			}
		})
	}
}
