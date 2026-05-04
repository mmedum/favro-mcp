package favro

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAuthError_Message_NeverIncludesCredentialsOrValues(t *testing.T) {
	t.Parallel()

	// AuthError is only for 401 (genuine auth-layer rejection).
	// 403 is *ForbiddenError so the LLM doesn't get a misleading
	// "check your env vars" message for resources it just can't see.
	err := &AuthError{Status: 401}
	msg := err.Error()
	require.Contains(t, msg, "FAVRO_USER_EMAIL", "should name the env var, not its value")
	require.Contains(t, msg, "FAVRO_API_TOKEN", "should name the env var, not its value")
	require.Contains(t, msg, fmt.Sprintf("%d", 401))
	require.NotContains(t, msg, "@", "AuthError must not embed any email")
}

func TestForbiddenError_MessageVariants(t *testing.T) {
	t.Parallel()

	withPath := &ForbiddenError{Status: 403, Path: "/cards/missing-or-private"}
	msg := withPath.Error()
	require.Contains(t, msg, "/cards/missing-or-private")
	require.Contains(t, msg, "403")
	// Must NOT direct the caller at the auth env-vars — that's the
	// misleading message we replaced (Phase 3.10 follow-up).
	require.NotContains(t, msg, "FAVRO_USER_EMAIL")
	require.NotContains(t, msg, "FAVRO_API_TOKEN")
	require.NotContains(t, msg, "@")

	noPath := &ForbiddenError{Status: 403}
	require.Contains(t, noPath.Error(), "403")
	require.NotContains(t, noPath.Error(), "FAVRO_USER_EMAIL")
}

func TestRateLimitError_Message(t *testing.T) {
	t.Parallel()

	withRetry := &RateLimitError{Status: 429, RetryAfter: 5 * time.Second}
	require.Contains(t, withRetry.Error(), "5s")
	require.Contains(t, withRetry.Error(), "429")

	noRetry := &RateLimitError{Status: 429}
	require.Contains(t, noRetry.Error(), "429")
	require.NotContains(t, noRetry.Error(), "retry after")
}

func TestNotFoundError_MessageVariants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  *NotFoundError
		want []string
	}{
		{"resource + id", &NotFoundError{Resource: "card", ID: "abc"}, []string{"card", `"abc"`}},
		{"resource only", &NotFoundError{Resource: "widget"}, []string{"widget"}},
		{"path fallback", &NotFoundError{Path: "/cards/missing"}, []string{"/cards/missing"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := tc.err.Error()
			for _, fragment := range tc.want {
				require.Contains(t, msg, fragment)
			}
		})
	}
}

func TestValidationError_HandlesEmptyBody(t *testing.T) {
	t.Parallel()

	withBody := &ValidationError{Status: 400, Body: "field 'name' required"}
	require.Contains(t, withBody.Error(), "name")

	noBody := &ValidationError{Status: 422}
	require.Contains(t, noBody.Error(), "422")
	require.False(t, strings.HasSuffix(noBody.Error(), ":"), "trailing colon when body is empty looks awkward")
}

func TestTransientError_NamesAttempts(t *testing.T) {
	t.Parallel()

	err := &TransientError{Status: 502, Attempts: 3}
	msg := err.Error()
	require.Contains(t, msg, "502")
	require.Contains(t, msg, "3 attempts")
}

func TestAPIError_FallbackForUnknownStatus(t *testing.T) {
	t.Parallel()

	err := &APIError{Status: 418, Body: "I am a teapot", Path: "/teapot"}
	msg := err.Error()
	require.Contains(t, msg, "418")
	require.Contains(t, msg, "/teapot")
	require.Contains(t, msg, "teapot")
}
