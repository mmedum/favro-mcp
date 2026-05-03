package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/auth"
)

// fixtureToken is a deterministic resolved token for tests. The values
// are obviously fake — they do not match any real Favro account — and
// no test should be allowed to send them over the network.
func fixtureToken() auth.ResolvedToken {
	return auth.ResolvedToken{
		Token: auth.Token{
			Email:          "fixture@example.invalid",
			APIToken:       "fixture-api-token-do-not-use",
			OrganizationID: "fixture-org-1",
		},
		Source: "env",
	}
}

// connectInMemory wires the server under test to a fresh client over a
// pair of in-memory transports. The returned ClientSession can be used
// to drive tools/list and tools/call without a subprocess.
func connectInMemory(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := t.Context()

	srv := New(fixtureToken(), "v0.1.0-test")
	client := mcp.NewClient(&mcp.Implementation{Name: "favro-mcp-test", Version: "v0.0.0"}, nil)

	clientT, serverT := mcp.NewInMemoryTransports()

	go func() {
		if err := srv.Run(ctx, serverT); err != nil && ctx.Err() == nil {
			t.Errorf("server.Run returned error: %v", err)
		}
	}()

	cs, err := client.Connect(ctx, clientT, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestMCP_ToolsList_IncludesFavroPing(t *testing.T) {
	t.Parallel()

	cs := connectInMemory(t)

	res, err := cs.ListTools(t.Context(), nil)
	require.NoError(t, err)

	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	require.Contains(t, names, pingToolName,
		"tools/list must advertise %s; got %v", pingToolName, names)
}

func TestMCP_FavroPing_ReturnsExpectedFields(t *testing.T) {
	t.Parallel()

	cs := connectInMemory(t)

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      pingToolName,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "favro_ping must not return as a tool error")

	out := decodeStructured[PingOutput](t, res)
	require.Equal(t, serverName, out.Server)
	require.Equal(t, "v0.1.0-test", out.Version)
	require.Equal(t, "fixture-org-1", out.OrganizationID)
	require.Equal(t, "env", out.CredentialSource)
}

// TestMCP_FavroPing_OutputContainsNoSecrets is the safety net for the
// "never leak credentials in tool output" rule. If a future change adds
// the email or API token to PingOutput (or any other field that gets
// serialized), this test fails loudly.
func TestMCP_FavroPing_OutputContainsNoSecrets(t *testing.T) {
	t.Parallel()

	cs := connectInMemory(t)

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      pingToolName,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)

	tok := fixtureToken().Token
	full := serializedResponseString(t, res)

	require.NotContains(t, full, tok.Email,
		"ping response leaked email: %q", full)
	require.NotContains(t, full, tok.APIToken,
		"ping response leaked API token: %q", full)
	require.NotContains(t, strings.ToLower(full), "authorization",
		"ping response includes the word 'authorization', which suggests a header leaked: %q", full)
}

// decodeStructured pulls the typed Output out of a CallToolResult.
// The SDK serializes structured output into res.StructuredContent.
func decodeStructured[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	require.NotNil(t, res.StructuredContent, "expected structured output, got nil")
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	var out T
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// serializedResponseString flattens a CallToolResult into a single
// string so leak-detection assertions can grep across structured + text
// content + every header echoed back.
func serializedResponseString(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	raw, err := json.Marshal(res)
	require.NoError(t, err)
	return string(raw)
}
