package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/auth"
	"github.com/mmedum/favro-mcp/internal/favroapi"
	"github.com/mmedum/favro-mcp/internal/favroapi/favroapitest"
)

// These forward to the one shared fixture; see favroapitest for why
// there is only one. Two packages had grown their own invented values
// and their own comment explaining hard rule 1, which is how a rule
// ends up meaning two things.
func fixtureToken() auth.Token { return favroapitest.Token() }

// connectInMemory wires the server under test to a fresh client over a
// pair of in-memory transports. The returned ClientSession can be used
// to drive tools/list and tools/call without a subprocess.
func connectInMemory(t *testing.T) *mcp.ClientSession {
	t.Helper()
	return connectInMemoryWith(t, favroapi.NewClient(fixtureToken()))
}

// assertMissingRequiredFieldFails drives a tool with no arguments
// and asserts (a) the SDK surfaces a tool-level error, (b) the
// LLM-visible error names the missing required field, and (c) the
// call short-circuits before any Favro request is made.
//
// Centralizes the contract every required-field tool must uphold —
// originally just the favro_get_<resource> tools, now also resolvers
// and any future tool with a required input. One place to update if
// the SDK's required-field error format ever changes.
func assertMissingRequiredFieldFails(t *testing.T, toolName, fieldName string) {
	t.Helper()

	calls := 0
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      toolName,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.True(t, res.IsError, "missing %s must surface as a tool error", fieldName)
	require.Contains(t, strings.ToLower(serializedResponseString(t, res)), fieldName,
		"the LLM-visible error must name the missing field")
	require.Equal(t, 0, calls, "missing %s must short-circuit before any Favro call", fieldName)
}

// favroFixture wires a *favroapi.Client to an httptest.Server backed by
// the supplied handler. Returns the client; the server is auto-closed
// at test end. Used by every test here that drives real HTTP responses
// through the Favro client.
func favroFixture(t *testing.T, handler http.Handler) *favroapi.Client {
	t.Helper()
	return favroapitest.Client(t, handler)
}

// staticJSONFixture answers every request with the same JSON body
// and, when capturedBody is non-nil, records the last request body so
// wire-shape assertions can run against it. For tools whose handler
// needs to branch on method or path, build the handler inline with
// favroFixture instead.
func staticJSONFixture(t *testing.T, capturedBody *string, response string) *favroapi.Client {
	t.Helper()
	return favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capturedBody != nil {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read request body: %v", err)
				return
			}
			*capturedBody = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
}

// connectInMemoryWith mirrors connectInMemory but uses the supplied
// Favro client — so tests can wire it to an httptest server first.
//
// The server goroutine and the test are tied together via a `done`
// channel: t.Cleanup waits for the goroutine to exit before the test
// completes, so a stray t.Errorf can't fire after the test has ended
// (the testing framework panics on "Log in goroutine after the test
// has completed" otherwise).
func connectInMemoryWith(t *testing.T, favroClient *favroapi.Client) *mcp.ClientSession {
	t.Helper()
	// Destructive on, so the shared harness sees the whole surface:
	// the smoke test's rule is a row per registered tool, and a
	// harness that quietly dropped thirteen of them would turn that
	// rule into a smaller promise. TestDestructiveToolsAreOptIn is
	// where the default is checked.
	return connectInMemoryOpts(t, favroClient, Options{Destructive: true})
}

// connectInMemoryOpts is connectInMemoryWith with the server options
// spelled out, for the tests whose subject is what gets registered.
func connectInMemoryOpts(t *testing.T, favroClient *favroapi.Client, opts Options) *mcp.ClientSession {
	t.Helper()
	ctx := t.Context()

	// The server is built here rather than through internal/server,
	// which imports this package: an in-package test cannot import its
	// own importer. It is also the more honest harness — what these
	// tests exercise is the tool surface, and Register is the tool
	// surface. internal/server adds the Implementation name and
	// nothing else.
	srv := mcp.NewServer(&mcp.Implementation{Name: ServerName, Version: "v0.1.0-test"}, nil)
	opts.CredentialSource = "env"
	opts.Version = "v0.1.0-test"
	Register(srv, favroClient, opts)
	client := mcp.NewClient(&mcp.Implementation{Name: "favro-mcp-test", Version: "v0.0.0"}, nil)

	clientT, serverT := mcp.NewInMemoryTransports()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.Run(ctx, serverT); err != nil && ctx.Err() == nil {
			t.Errorf("server.Run returned error: %v", err)
		}
	}()

	cs, err := client.Connect(ctx, clientT, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = cs.Close()
		<-done
	})
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
	require.Subset(t, names, []string{
		pingToolName,
		rateLimitToolName,
		listOrgsToolName,
		getOrgToolName,
		listUsersToolName,
		getUserToolName,
		listCollectionsToolName,
		getCollectionToolName,
		listWidgetsToolName,
		getWidgetToolName,
		listColumnsToolName,
		getColumnToolName,
		listCardsToolName,
		getCardToolName,
		listCommentsToolName,
		getCommentToolName,
		listTagsToolName,
		getTagToolName,
		listCustomFieldsToolName,
		getCustomFieldToolName,
		listGroupsToolName,
		getGroupToolName,
		resolveTagToolName,
		resolveUserToolName,
		resolveCollectionToolName,
		resolveWidgetToolName,
		resolveColumnToolName,
		resolveCustomFieldToolName,
		resolveGroupToolName,
		searchCardsToolName,
		getCardFullToolName,
		createTagToolName,
		deleteTagToolName,
		updateTagToolName,
		updateTagsToolName,
		createCommentToolName,
		updateCommentToolName,
		deleteCommentToolName,
		createCardToolName,
		updateCardToolName,
		archiveCardToolName,
		unarchiveCardToolName,
		moveCardToolName,
		deleteCardToolName,
		createCollectionToolName,
		updateCollectionToolName,
		deleteCollectionToolName,
		createWidgetToolName,
		updateWidgetToolName,
		deleteWidgetToolName,
		createColumnToolName,
		updateColumnToolName,
		deleteColumnToolName,
		createGroupToolName,
		updateGroupToolName,
		deleteGroupToolName,
		setCardCustomFieldToolName,
		appendCardDescriptionToolName,
		prependCardDescriptionToolName,
		replaceInCardDescriptionToolName,
		addCommentToCardToolName,
		addTagToCardToolName,
		removeTagFromCardToolName,
		uploadAttachmentToolName,
		uploadCommentAttachmentToolName,
		removeAttachmentToolName,
		listTasksToolName,
		getTaskToolName,
		createTaskToolName,
		updateTaskToolName,
		deleteTaskToolName,
		listTasklistsToolName,
		getTasklistToolName,
		createTasklistToolName,
		updateTasklistToolName,
		deleteTasklistToolName,
		listDependenciesToolName,
		addDependenciesToolName,
		replaceDependenciesToolName,
		updateDependencyToolName,
		deleteDependencyToolName,
		deleteAllDependenciesToolName,
		listCardActivitiesToolName,
	}, "tools/list must advertise every registered tool; got %v", names)
}

// TestMCP_ToolsList_NoUnlistedTools is the other half of the subset
// assertion above: it fails when a tool is registered but not named
// in that list, so the coverage check can't silently fall behind.
func TestMCP_ToolsList_NoUnlistedTools(t *testing.T) {
	t.Parallel()

	cs := connectInMemory(t)

	res, err := cs.ListTools(t.Context(), nil)
	require.NoError(t, err)

	require.Len(t, res.Tools, registeredToolCount,
		"a tool was registered or removed without updating registeredToolCount "+
			"and the tools/list coverage assertion above")
}

// registeredToolCount is the number of tools New() registers. Bump it
// together with the name list in TestMCP_ToolsList_IncludesFavroPing.
const registeredToolCount = 83

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
	require.Equal(t, ServerName, out.Server)
	require.Equal(t, "v0.1.0-test", out.Version)
	require.Equal(t, favroapitest.Token().OrganizationID, out.OrganizationID)
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

	tok := fixtureToken()
	full := serializedResponseString(t, res)

	require.NotContains(t, full, tok.Email,
		"ping response leaked email: %q", full)
	require.NotContains(t, full, tok.APIToken,
		"ping response leaked API token: %q", full)
	require.NotContains(t, strings.ToLower(full), "authorization",
		"ping response includes the word 'authorization', which suggests a header leaked: %q", full)
}

func TestMCP_RateLimitStatus_NoObservationsYet(t *testing.T) {
	t.Parallel()

	cs := connectInMemory(t)

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      rateLimitToolName,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[RateLimitOutput](t, res)
	require.False(t, out.HaveSeen)
	require.Equal(t, -1, out.Remaining, "Remaining must distinguish 'not seen' from 'zero'")
}

func TestMCP_RateLimitStatus_AfterObservation(t *testing.T) {
	t.Parallel()

	favroClient := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "1000")
		w.Header().Set("X-RateLimit-Remaining", "987")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))

	// Drive a single request so the client records a snapshot.
	resp, err := favroClient.Do(context.Background(), http.MethodGet, "/anything", nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		if resp != nil {
			_ = resp.Body.Close()
		}
	})

	cs := connectInMemoryWith(t, favroClient)
	callRes, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      rateLimitToolName,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, callRes.IsError)

	out := decodeStructured[RateLimitOutput](t, callRes)
	require.True(t, out.HaveSeen)
	require.Equal(t, 1000, out.Limit)
	require.Equal(t, 987, out.Remaining)
	require.Equal(t, "/anything", out.LastPath)
	require.Equal(t, http.StatusOK, out.LastStatus)
	require.NotZero(t, out.LastObservedUnix)
	require.NotEmpty(t, out.LastObservedAgo)
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
