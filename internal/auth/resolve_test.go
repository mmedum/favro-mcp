package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)
	require.Equal(t, "primary", got.Source)
	require.Equal(t, "t1", got.Token.APIToken)
}

func TestResolveToken_FallthroughOnNotConfigured(t *testing.T) {
	t.Parallel()

	primary := &stubSource{name: "primary", err: errNotConfigured}
	secondary := &stubSource{name: "secondary", tok: Token{Email: "u@e", APIToken: "t", OrganizationID: "o"}}

	got, err := resolveToken(context.Background(), []Source{primary, secondary})
	require.NoError(t, err)
	require.Equal(t, "secondary", got.Source)
}

func TestResolveToken_StopsOnRealError(t *testing.T) {
	t.Parallel()

	boom := errors.New("keyring D-Bus connection refused")
	primary := &stubSource{name: "primary", err: boom}
	secondary := &stubSource{name: "secondary", tok: Token{Email: "u@e", APIToken: "t", OrganizationID: "o"}}

	got, err := resolveToken(context.Background(), []Source{primary, secondary})
	require.ErrorIs(t, err, boom, "real errors must surface, not be swallowed by fallthrough")
	require.Empty(t, got.Source)
}

func TestResolveToken_NoSources_ReturnsNoCredentials(t *testing.T) {
	t.Parallel()

	_, err := resolveToken(context.Background(), nil)
	require.ErrorIs(t, err, errNoCredentials)
}

func TestResolveToken_AllNotConfigured_ReturnsNoCredentials(t *testing.T) {
	t.Parallel()

	a := &stubSource{name: "a", err: errNotConfigured}
	b := &stubSource{name: "b", err: errNotConfigured}

	_, err := resolveToken(context.Background(), []Source{a, b})
	require.ErrorIs(t, err, errNoCredentials)
}

func TestDefaultSources_OrderIsEnvThenKeyring(t *testing.T) {
	t.Parallel()

	srcs := defaultSources()
	require.Len(t, srcs, 2)
	require.Equal(t, "env", srcs[0].Name(), "env must win so a quick override works without `auth login`")
	require.Equal(t, "keyring", srcs[1].Name())
}
