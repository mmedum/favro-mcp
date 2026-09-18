package favroapi

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mmedum/favro-mcp/internal/render"
)

func TestAuthError_Message_NeverIncludesCredentialsOrValues(t *testing.T) {
	t.Parallel()

	// AuthError is only for 401 (genuine auth-layer rejection).
	// 403 is *ForbiddenError so the LLM doesn't get a misleading
	// "check your env vars" message for resources it just can't see.
	err := &AuthError{Status: 401}
	msg := err.Error()
	if !strings.Contains(msg, "FAVRO_USER_EMAIL") {
		t.Errorf("should name the env var, not its value: %q missing", "FAVRO_USER_EMAIL")
	}
	if !strings.Contains(msg, "FAVRO_API_TOKEN") {
		t.Errorf("should name the env var, not its value: %q missing", "FAVRO_API_TOKEN")
	}
	if !strings.Contains(msg, fmt.Sprintf("%d", 401)) {
		t.Errorf("msg does not contain %q", fmt.Sprintf("%d", 401))
	}
	if strings.Contains(msg, "@") {
		t.Errorf("AuthError must not embed any email: %q present", "@")
	}
}

func TestForbiddenError_MessageVariants(t *testing.T) {
	t.Parallel()

	withPath := &ForbiddenError{Status: 403, Path: "/cards/missing-or-private"}
	msg := withPath.Error()
	if !strings.Contains(msg, "/cards/missing-or-private") {
		t.Errorf("msg does not contain %q", "/cards/missing-or-private")
	}
	if !strings.Contains(msg, "403") {
		t.Errorf("msg does not contain %q", "403")
	}
	// Must NOT direct the caller at the auth env-vars — that's the
	// misleading message we replaced (Phase 3.10 follow-up).
	if strings.Contains(msg, "FAVRO_USER_EMAIL") {
		t.Errorf("msg unexpectedly contains %q", "FAVRO_USER_EMAIL")
	}
	if strings.Contains(msg, "FAVRO_API_TOKEN") {
		t.Errorf("msg unexpectedly contains %q", "FAVRO_API_TOKEN")
	}
	if strings.Contains(msg, "@") {
		t.Errorf("msg unexpectedly contains %q", "@")
	}

	noPath := &ForbiddenError{Status: 403}
	if !strings.Contains(noPath.Error(), "403") {
		t.Errorf("noPath.Error() does not contain %q", "403")
	}
	if strings.Contains(noPath.Error(), "FAVRO_USER_EMAIL") {
		t.Errorf("noPath.Error() unexpectedly contains %q", "FAVRO_USER_EMAIL")
	}
}

func TestRateLimitError_Message(t *testing.T) {
	t.Parallel()

	withRetry := &RateLimitError{Status: 429, RetryAfter: 5 * time.Second}
	if !strings.Contains(withRetry.Error(), "5s") {
		t.Errorf("withRetry.Error() does not contain %q", "5s")
	}
	if !strings.Contains(withRetry.Error(), "429") {
		t.Errorf("withRetry.Error() does not contain %q", "429")
	}

	noRetry := &RateLimitError{Status: 429}
	if !strings.Contains(noRetry.Error(), "429") {
		t.Errorf("noRetry.Error() does not contain %q", "429")
	}
	if strings.Contains(noRetry.Error(), "retry after") {
		t.Errorf("noRetry.Error() unexpectedly contains %q", "retry after")
	}
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
				if !strings.Contains(msg, fragment) {
					t.Errorf("msg does not contain %q", fragment)
				}
			}
		})
	}
}

func TestValidationError_HandlesEmptyBody(t *testing.T) {
	t.Parallel()

	withBody := &ValidationError{Status: 400, Body: "field 'name' required"}
	if !strings.Contains(withBody.Error(), "name") {
		t.Errorf("withBody.Error() does not contain %q", "name")
	}

	noBody := &ValidationError{Status: 422}
	if !strings.Contains(noBody.Error(), "422") {
		t.Errorf("noBody.Error() does not contain %q", "422")
	}
	if strings.HasSuffix(noBody.Error(), ":") {
		t.Error("trailing colon when body is empty looks awkward")
	}
}

func TestTransientError_NamesAttempts(t *testing.T) {
	t.Parallel()

	err := &TransientError{Status: 502, Attempts: 3}
	msg := err.Error()
	if !strings.Contains(msg, "502") {
		t.Errorf("msg does not contain %q", "502")
	}
	if !strings.Contains(msg, "3 attempts") {
		t.Errorf("msg does not contain %q", "3 attempts")
	}
}

func TestAPIError_FallbackForUnknownStatus(t *testing.T) {
	t.Parallel()

	err := &APIError{Status: 418, Body: "I am a teapot", Path: "/teapot"}
	msg := err.Error()
	if !strings.Contains(msg, "418") {
		t.Errorf("msg does not contain %q", "418")
	}
	if !strings.Contains(msg, "/teapot") {
		t.Errorf("msg does not contain %q", "/teapot")
	}
	if !strings.Contains(msg, "teapot") {
		t.Errorf("msg does not contain %q", "teapot")
	}
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
		"AuthError":         render.ClassAuth,
		"ForbiddenError":    render.ClassForbidden,
		"RateLimitError":    render.ClassRateLimited,
		"NotFoundError":     render.ClassNotFound,
		"ValidationError":   render.ClassInvalid,
		"TransientError":    render.ClassUnavailable,
		"APIError":          render.ClassInvalid, // by status; see its ErrorClass
		"WriteIgnoredError": render.ClassUnavailable,
	}
	samples := map[string]error{
		"AuthError":         &AuthError{Status: 401},
		"ForbiddenError":    &ForbiddenError{Status: 403},
		"RateLimitError":    &RateLimitError{Status: 429},
		"NotFoundError":     &NotFoundError{Resource: "card"},
		"ValidationError":   &ValidationError{Status: 400},
		"TransientError":    &TransientError{Status: 503, Attempts: 4},
		"APIError":          &APIError{Status: 410},
		"WriteIgnoredError": &WriteIgnoredError{Field: "column", Want: "col-2"},
	}

	declared := declaredErrorTypes(t)
	if len(declared) < 8 {
		t.Errorf("read %d error types out of errors.go: got %v, want at least %v", len(declared), len(declared), 8)
	}

	for _, name := range declared {
		want, ok := expected[name]
		if !ok {
			t.Errorf("errors.go declares %s and nothing here classifies it — it would reach render's ClassInvalid fallback silently", name)
		}
		if got := render.Classify(samples[name]); got != want {
			t.Errorf("%s does not classify as %s: got %v, want %v", name, want, got, want)
		}
	}
	for name := range expected {
		if !slices.Contains(declared, name) {
			t.Errorf("%s is expected here but no longer declared: %q missing", name, name)
		}
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
		if got := (&APIError{Status: status}).ErrorClass(); got != want {
			t.Errorf("status %d: got %v, want %v", status, got, want)
		}
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
		if got := (&RateLimitError{RetryAfter: in}).RetryAfterSeconds(); got != want {
			t.Errorf("%s: got %v, want %v", in, got, want)
		}
	}
}

// declaredErrorTypes returns the struct types errors.go declares.
func declaredErrorTypes(t *testing.T) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "errors.go", nil, 0)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

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
