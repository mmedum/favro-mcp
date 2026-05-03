package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// staticLookup builds a Lookup func over a fixed map for deterministic
// tests — using os.Setenv would couple tests to process state and break
// t.Parallel.
func staticLookup(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func TestEnvSource_Name(t *testing.T) {
	t.Parallel()
	require.Equal(t, "env", EnvSource{}.Name())
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
				EnvUserEmail:      "u@e.com",
				EnvAPIToken:       "tok",
				EnvOrganizationID: "org-1",
			},
			wantToken: Token{Email: "u@e.com", APIToken: "tok", OrganizationID: "org-1"},
		},
		{
			name: "whitespace is trimmed",
			env: map[string]string{
				EnvUserEmail:      "  u@e.com  ",
				EnvAPIToken:       " tok ",
				EnvOrganizationID: " org-1 ",
			},
			wantToken: Token{Email: "u@e.com", APIToken: "tok", OrganizationID: "org-1"},
		},
		{
			name: "partial -> missingFieldError, not errNotConfigured",
			env: map[string]string{
				EnvUserEmail: "u@e.com",
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
				require.ErrorIs(t, err, tc.wantErr)
				require.Equal(t, Token{}, tok)
			case tc.wantMissing != nil:
				var mfe *missingFieldError
				require.ErrorAs(t, err, &mfe, "expected *missingFieldError")
				require.Equal(t, tc.wantMissing, mfe.fields)
				require.Equal(t, Token{}, tok)
			default:
				require.NoError(t, err)
				require.Equal(t, tc.wantToken, tok)
			}
		})
	}
}
