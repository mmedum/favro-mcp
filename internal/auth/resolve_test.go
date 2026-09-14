package auth

import (
	"context"
	"errors"
	"testing"
)

// stubSource is a minimal Source double for resolve tests. The
// production sources are tested separately; resolveToken's contract
// (skip errNotConfigured, surface other errors, prefer earlier sources)
// is what we want to pin down here.
type stubSource struct {
	name string
	tok  Token
	err  error
}

func (s *stubSource) Name() string                          { return s.name }
func (s *stubSource) Load(_ context.Context) (Token, error) { return s.tok, s.err }

func TestResolveToken_FirstSourceWins(t *testing.T) {
	t.Parallel()

	primary := &stubSource{name: "primary", tok: Token{Email: "a@b", APIToken: "t1", OrganizationID: "o"}}
	secondary := &stubSource{name: "secondary", tok: Token{Email: "x@y", APIToken: "t2", OrganizationID: "o"}}

	got, err := resolveToken(context.Background(), []Source{primary, secondary})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.Source; got != "primary" {
		t.Errorf("got.Source = %v, want %v", got, "primary")
	}
	if got := got.Token.APIToken; got != "t1" {
		t.Errorf("got.Token.APIToken = %v, want %v", got, "t1")
	}
}

func TestResolveToken_FallthroughOnNotConfigured(t *testing.T) {
	t.Parallel()

	primary := &stubSource{name: "primary", err: errNotConfigured}
	secondary := &stubSource{name: "secondary", tok: Token{Email: "u@e", APIToken: "t", OrganizationID: "o"}}

	got, err := resolveToken(context.Background(), []Source{primary, secondary})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.Source; got != "secondary" {
		t.Errorf("got.Source = %v, want %v", got, "secondary")
	}
}

func TestResolveToken_StopsOnRealError(t *testing.T) {
	t.Parallel()

	boom := errors.New("keyring D-Bus connection refused")
	primary := &stubSource{name: "primary", err: boom}
	secondary := &stubSource{name: "secondary", tok: Token{Email: "u@e", APIToken: "t", OrganizationID: "o"}}

	got, err := resolveToken(context.Background(), []Source{primary, secondary})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want boom", err)
	}
	if len(got.Source) != 0 {
		t.Errorf("got.Source = %v, want empty", got.Source)
	}
}

func TestResolveToken_NoSources_ReturnsNoCredentials(t *testing.T) {
	t.Parallel()

	_, err := resolveToken(context.Background(), nil)
	if !errors.Is(err, errNoCredentials) {
		t.Fatalf("got %v, want errNoCredentials", err)
	}
}

func TestResolveToken_AllNotConfigured_ReturnsNoCredentials(t *testing.T) {
	t.Parallel()

	a := &stubSource{name: "a", err: errNotConfigured}
	b := &stubSource{name: "b", err: errNotConfigured}

	_, err := resolveToken(context.Background(), []Source{a, b})
	if !errors.Is(err, errNoCredentials) {
		t.Fatalf("got %v, want errNoCredentials", err)
	}
}

func TestDefaultSources_OrderIsEnvThenKeyring(t *testing.T) {
	t.Parallel()

	srcs := defaultSources()
	if len(srcs) != 2 {
		t.Fatalf("len(srcs) = %d, want 2", len(srcs))
	}
	if got := srcs[0].Name(); got != "env" {
		t.Errorf("env must win so a quick override works without `auth login`: got %v, want %v", got, "env")
	}
	if got := srcs[1].Name(); got != "keyring" {
		t.Errorf("srcs[1].Name() = %v, want %v", got, "keyring")
	}
}
