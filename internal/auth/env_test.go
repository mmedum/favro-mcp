package auth

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestEnvSource_Name(t *testing.T) {
	t.Parallel()
	if got := (EnvSource{}.Name()); got != "env" {
		t.Errorf("EnvSource{}.Name() = %v, want %v", got, "env")
	}
}

// EnvSource holds what its caller read from the environment, so these
// cases are tokens rather than environments; the spaces around a value
// are config.CredentialsFrom's to drop.
func TestEnvSource_Load(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		env         Token
		wantErr     error
		wantMissing []string
		wantToken   Token
	}{
		{
			name:    "all empty -> errNotConfigured",
			wantErr: errNotConfigured,
		},
		{
			name:      "all set -> Token",
			env:       Token{Email: "u@example.test", APIToken: "tok", OrganizationID: "org-1"},
			wantToken: Token{Email: "u@example.test", APIToken: "tok", OrganizationID: "org-1"},
		},
		{
			name:        "partial -> missingFieldError, not errNotConfigured",
			env:         Token{Email: "u@example.test", APIToken: "tok"},
			wantMissing: []string{"organization id"},
		},
		{
			name:        "only org set -> missingFieldError naming the missing two",
			env:         Token{OrganizationID: "org-1"},
			wantMissing: []string{"email", "API token"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tok, err := EnvSource{Token: tc.env}.Load(context.Background())

			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got %v, want tc.wantErr", err)
				}
				if got := tok; got != (Token{}) {
					t.Errorf("tok = %v, want %v", got, Token{})
				}
			case tc.wantMissing != nil:
				var mfe *missingFieldError
				if !errors.As(err, &mfe) {
					t.Fatalf("got %v, want mfe", err)
				}
				if got := mfe.fields; !reflect.DeepEqual(got, tc.wantMissing) {
					t.Errorf("mfe.fields = %v, want %v", got, tc.wantMissing)
				}
				if got := tok; got != (Token{}) {
					t.Errorf("tok = %v, want %v", got, Token{})
				}
			default:
				if err := err; err != nil {
					t.Fatalf("err: %v", err)
				}
				if got := tok; got != tc.wantToken {
					t.Errorf("tok = %v, want %v", got, tc.wantToken)
				}
			}
		})
	}
}
