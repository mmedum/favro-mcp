package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeFavro is the minimal subset of the Favro API behavior the
// validator interacts with: a single GET /organizations endpoint that
// returns a configurable status. Handler-side checks use t.Errorf
// rather than require because require.* calls t.FailNow, which is only
// safe to call from the goroutine that started the test.
func fakeFavro(t *testing.T, status int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/organizations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected method GET, got %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok {
			t.Error("validator must send Basic Auth")
			http.Error(w, "missing auth", http.StatusBadRequest)
			return
		}
		if user == "" || pass == "" {
			t.Error("Basic Auth user and password must be non-empty")
			http.Error(w, "incomplete auth", http.StatusBadRequest)
			return
		}
		w.WriteHeader(status)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func goodToken() Token {
	return Token{Email: "u@e.com", APIToken: "tok", OrganizationID: "org-1"}
}

func TestValidator_OK(t *testing.T) {
	t.Parallel()

	srv := fakeFavro(t, http.StatusOK)
	v := &Validator{BaseURL: srv.URL, Client: srv.Client()}

	require.NoError(t, v.Validate(context.Background(), goodToken()))
}

func TestValidator_AuthFailed(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			srv := fakeFavro(t, status)
			v := &Validator{BaseURL: srv.URL, Client: srv.Client()}

			err := v.Validate(context.Background(), goodToken())
			require.ErrorIs(t, err, ErrAuthFailed)
		})
	}
}

func TestValidator_OtherStatus_WrappedError(t *testing.T) {
	t.Parallel()

	srv := fakeFavro(t, http.StatusInternalServerError)
	v := &Validator{BaseURL: srv.URL, Client: srv.Client()}

	err := v.Validate(context.Background(), goodToken())
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrAuthFailed,
		"5xx must not be reported as auth failure — it confuses the operator")
	require.Contains(t, err.Error(), "500",
		"error message should name the status code, got %q", err.Error())
}

func TestValidator_RejectsIncompleteToken_NoNetworkCall(t *testing.T) {
	t.Parallel()

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	t.Cleanup(srv.Close)

	v := &Validator{BaseURL: srv.URL, Client: srv.Client()}
	err := v.Validate(context.Background(), Token{Email: "u@e.com"}) // missing token + org

	var mfe *missingFieldError
	require.ErrorAs(t, err, &mfe)
	require.False(t, called, "must short-circuit before contacting Favro when token is incomplete")
}

func TestValidator_NetworkError_Wrapped(t *testing.T) {
	t.Parallel()

	// Closed server so Do returns a connect error.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	v := &Validator{BaseURL: srv.URL, Client: &http.Client{Timeout: 250 * time.Millisecond}}
	err := v.Validate(context.Background(), goodToken())

	require.Error(t, err)
	require.Contains(t, err.Error(), "contact Favro",
		"network errors should be wrapped with a 'contact Favro' prefix, got %q", err.Error())
}

func TestValidator_AuthorizationHeaderIsBasic(t *testing.T) {
	t.Parallel()

	var seenAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/organizations", func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	v := &Validator{BaseURL: srv.URL, Client: srv.Client()}
	require.NoError(t, v.Validate(context.Background(), goodToken()))

	require.True(t, strings.HasPrefix(seenAuth, "Basic "),
		"Authorization header must be Basic Auth, got %q", seenAuth)
}

func TestDefaultValidator_HasReasonableDefaults(t *testing.T) {
	t.Parallel()

	v := DefaultValidator()
	require.Equal(t, defaultBaseURL, v.BaseURL)
	require.NotNil(t, v.Client)
	require.Greater(t, v.Client.Timeout, time.Duration(0),
		"DefaultValidator must set a non-zero client timeout so a wedged DNS lookup doesn't hold up startup")
}
