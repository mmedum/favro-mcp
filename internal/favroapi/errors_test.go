package favroapi

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/render"
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

// TestEveryErrorTypeNamesItsClass reads this file for the error types
// it declares and requires each to name its class.
//
// The class used to be read off these types from outside, by a switch
// in internal/render, and the failure mode was silent: an eighth type
// added here fell through to the ClassInvalid fallback with nothing to
// fail. The types name their own class now, and this is what stops the
// next one being added without one. The list is parsed rather than
// typed out, so it cannot drift from the file it describes.
func TestEveryErrorTypeNamesItsClass(t *testing.T) {
	t.Parallel()

	expected := map[string]render.Class{
		"AuthError":       render.ClassAuth,
		"ForbiddenError":  render.ClassForbidden,
		"RateLimitError":  render.ClassRateLimited,
		"NotFoundError":   render.ClassNotFound,
		"ValidationError": render.ClassInvalid,
		"TransientError":  render.ClassUnavailable,
		"APIError":        render.ClassInvalid, // by status; see its ErrorClass
	}
	samples := map[string]error{
		"AuthError":       &AuthError{Status: 401},
		"ForbiddenError":  &ForbiddenError{Status: 403},
		"RateLimitError":  &RateLimitError{Status: 429},
		"NotFoundError":   &NotFoundError{Resource: "card"},
		"ValidationError": &ValidationError{Status: 400},
		"TransientError":  &TransientError{Status: 503, Attempts: 4},
		"APIError":        &APIError{Status: 410},
	}

	declared := declaredErrorTypes(t)
	require.GreaterOrEqual(t, len(declared), 7,
		"read %d error types out of errors.go", len(declared))

	for _, name := range declared {
		want, ok := expected[name]
		require.True(t, ok,
			"errors.go declares %s and nothing here classifies it — it would reach render's ClassInvalid fallback silently", name)
		require.Equal(t, want, render.Classify(samples[name]),
			"%s does not classify as %s", name, want)
	}
	for name := range expected {
		require.Contains(t, declared, name, "%s is expected here but no longer declared", name)
	}
}

// TestAPIErrorClassByStatus covers the one type whose class depends on
// what it carries. The statuses are those *APIError can actually hold:
// Do peels off 2xx, 429 and 5xx, and 401/403/404/400/422 become their
// own types, so 3xx and the leftover 4xx are what reach it.
func TestAPIErrorClassByStatus(t *testing.T) {
	t.Parallel()

	cases := map[int]render.Class{
		http.StatusConflict:         render.ClassConflict,
		http.StatusMethodNotAllowed: render.ClassUnsupported,
		http.StatusGone:             render.ClassInvalid,
		http.StatusTeapot:           render.ClassInvalid,
	}
	for status, want := range cases {
		require.Equal(t, want, (&APIError{Status: status}).ErrorClass(), "status %d", status)
	}
}

// TestRateLimitRetryAfterSeconds pins the rounding: a sub-second wait
// must never render as "retry immediately".
func TestRateLimitRetryAfterSeconds(t *testing.T) {
	t.Parallel()

	cases := map[time.Duration]int{
		0:                       0,
		-time.Second:            0,
		200 * time.Millisecond:  1,
		time.Second:             1,
		1500 * time.Millisecond: 2,
		90 * time.Second:        90,
	}
	for in, want := range cases {
		require.Equal(t, want, (&RateLimitError{RetryAfter: in}).RetryAfterSeconds(), "%s", in)
	}
}

// declaredErrorTypes returns the struct types errors.go declares.
func declaredErrorTypes(t *testing.T) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "errors.go", nil, 0)
	require.NoError(t, err)

	var out []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if _, ok := ts.Type.(*ast.StructType); ok {
				out = append(out, ts.Name.Name)
			}
		}
	}
	return out
}
