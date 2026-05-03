package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRun_Version_PrintsToStdout pins the discipline that --version
// output goes to stdout (so users can `favro-mcp --version | grep`)
// while every diagnostic goes to stderr.
func TestRun_Version_PrintsToStdout(t *testing.T) {
	t.Parallel()

	stdin := strings.NewReader("")
	var stdout, stderr bytes.Buffer

	require.NoError(t, run([]string{"--version"}, stdin, &stdout, &stderr))

	require.Contains(t, stdout.String(), "favro-mcp")
	require.Empty(t, stderr.String(), "version flag must not emit diagnostics to stderr")
}

func TestRun_Help_PrintsToStdout(t *testing.T) {
	t.Parallel()

	stdin := strings.NewReader("")
	var stdout, stderr bytes.Buffer

	require.NoError(t, run([]string{"--help"}, stdin, &stdout, &stderr))

	require.Contains(t, stdout.String(), "favro-mcp — Model Context Protocol server for Favro")
}

func TestRun_AuthSubcommand_Routed(t *testing.T) {
	t.Parallel()

	stdin := strings.NewReader("")
	var stdout, stderr bytes.Buffer

	// `auth help` should hit the auth dispatcher and emit usage to stderr.
	require.NoError(t, run([]string{"auth", "help"}, stdin, &stdout, &stderr))
	require.Empty(t, stdout.String(), "auth subcommands must not write to stdout")
	require.Contains(t, stderr.String(), "favro-mcp auth")
}

func TestRun_UnknownAuthSubcommand_Errors(t *testing.T) {
	t.Parallel()

	stdin := strings.NewReader("")
	var stdout, stderr bytes.Buffer

	err := run([]string{"auth", "no-such-thing"}, stdin, &stdout, &stderr)
	require.Error(t, err)
	require.Contains(t, stderr.String(), "unknown subcommand")
}
