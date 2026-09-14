package auth

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// staticLookup builds a Lookup func over a fixed map for deterministic
// tests — using os.Setenv would couple tests to process state and break
// t.Parallel.
func staticLookup(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func TestEnvSource_Name(t *testing.T) {
	t.Parallel()
	if got := (EnvSource{}.Name()); got != "env" {
		t.Errorf("EnvSource{}.Name() = %v, want %v", got, "env")
	}
}

func TestEnvSource_Load(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		env         map[string]string
		wantErr     error
		wantMissing []string
		wantToken   Token
	}{
		{
			name:    "all empty -> errNotConfigured",
			env:     map[string]string{},
			wantErr: errNotConfigured,
		},
		{
			name: "all set -> Token",
			env: map[string]string{
				EnvUserEmail:      "u@example.test",
				EnvAPIToken:       "tok",
				EnvOrganizationID: "org-1",
			},
			wantToken: Token{Email: "u@example.test", APIToken: "tok", OrganizationID: "org-1"},
		},
		{
			name: "whitespace is trimmed",
			env: map[string]string{
				EnvUserEmail:      "  u@example.test  ",
				EnvAPIToken:       " tok ",
				EnvOrganizationID: " org-1 ",
			},
			wantToken: Token{Email: "u@example.test", APIToken: "tok", OrganizationID: "org-1"},
		},
		{
			name: "partial -> missingFieldError, not errNotConfigured",
			env: map[string]string{
				EnvUserEmail: "u@example.test",
				EnvAPIToken:  "tok",
				// no FAVRO_ORGANIZATION_ID
			},
			wantMissing: []string{"organization id"},
		},
		{
			name: "only org set -> missingFieldError naming the missing two",
			env: map[string]string{
				EnvOrganizationID: "org-1",
			},
			wantMissing: []string{"email", "API token"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := EnvSource{Lookup: staticLookup(tc.env)}
			tok, err := src.Load(context.Background())

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
