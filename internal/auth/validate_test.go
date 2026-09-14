package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	return Token{Email: "u@example.test", APIToken: "tok", OrganizationID: "org-1"}
}

func TestValidator_OK(t *testing.T) {
	t.Parallel()

	srv := fakeFavro(t, http.StatusOK)
	v := &Validator{BaseURL: srv.URL, Client: srv.Client()}

	if err := v.Validate(context.Background(), goodToken()); err != nil {
		t.Fatalf("v.Validate(context.Background(), goodToken()): %v", err)
	}
}

func TestValidator_AuthFailed(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			srv := fakeFavro(t, status)
			v := &Validator{BaseURL: srv.URL, Client: srv.Client()}

			err := v.Validate(context.Background(), goodToken())
			if !errors.Is(err, ErrAuthFailed) {
				t.Fatalf("got %v, want ErrAuthFailed", err)
			}
		})
	}
}

func TestValidator_OtherStatus_WrappedError(t *testing.T) {
	t.Parallel()

	srv := fakeFavro(t, http.StatusInternalServerError)
	v := &Validator{BaseURL: srv.URL, Client: srv.Client()}

	err := v.Validate(context.Background(), goodToken())
	if err == nil {
		t.Fatal("err should have failed")
	}
	if errors.Is(err, ErrAuthFailed) {
		t.Fatalf("got %v, want anything but ErrAuthFailed", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error message should name the status code, got %q: %q missing", err.Error(), "500")
	}
}

func TestValidator_RejectsIncompleteToken_NoNetworkCall(t *testing.T) {
	t.Parallel()

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	t.Cleanup(srv.Close)

	v := &Validator{BaseURL: srv.URL, Client: srv.Client()}
	err := v.Validate(context.Background(), Token{Email: "u@example.test"}) // missing token + org

	var mfe *missingFieldError
	if !errors.As(err, &mfe) {
		t.Fatalf("got %v, want mfe", err)
	}
	if called {
		t.Error("must short-circuit before contacting Favro when token is incomplete")
	}
}

func TestValidator_NetworkError_Wrapped(t *testing.T) {
	t.Parallel()

	// Closed server so Do returns a connect error.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	v := &Validator{BaseURL: srv.URL, Client: &http.Client{Timeout: 250 * time.Millisecond}}
	err := v.Validate(context.Background(), goodToken())

	if err == nil {
		t.Fatal("err should have failed")
	}
	if !strings.Contains(err.Error(), "contact Favro") {
		t.Errorf("network errors should be wrapped with a 'contact Favro' prefix, got %q: %q missing", err.Error(), "contact Favro")
	}
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
	if err := v.Validate(context.Background(), goodToken()); err != nil {
		t.Fatalf("v.Validate(context.Background(), goodToken()): %v", err)
	}

	if !strings.HasPrefix(seenAuth, "Basic ") {
		t.Errorf("Authorization header must be Basic Auth, got %q", seenAuth)
	}
}

func TestDefaultValidator_HasReasonableDefaults(t *testing.T) {
	t.Parallel()

	v := DefaultValidator()
	if got := v.BaseURL; got != defaultBaseURL {
		t.Errorf("v.BaseURL = %v, want %v", got, defaultBaseURL)
	}
	if v.Client == nil {
		t.Fatal("v.Client is nil")
	}
	if v.Client.Timeout <= time.Duration(0) {
		t.Errorf("DefaultValidator must set a non-zero client timeout so a wedged DNS lookup doesn't hold up startup: got %v, want greater than %v", v.Client.Timeout, time.Duration(0))
	}
}
