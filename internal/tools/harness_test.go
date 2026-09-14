package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Errorf("missing %s must surface as a tool error", fieldName)
	}
	if !strings.Contains(strings.ToLower(serializedResponseString(t, res)), fieldName) {
		t.Errorf("the LLM-visible error must name the missing field: %q missing", fieldName)
	}
	if got := calls; got != 0 {
		t.Errorf("missing %s must short-circuit before any Favro call: got %v, want %v", fieldName, got, 0)
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{
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
		listWebhooksToolName,
		deleteWebhookToolName,
	} {
		if !slices.Contains(names, want) {
			t.Errorf("tools/list must advertise every registered tool; %s is missing", want)
		}
	}
}

// TestMCP_ToolsList_NoUnlistedTools is the other half of the subset
// assertion above: it fails when a tool is registered but not named
// in that list, so the coverage check can't silently fall behind.
func TestMCP_ToolsList_NoUnlistedTools(t *testing.T) {
	t.Parallel()

	cs := connectInMemory(t)

	res, err := cs.ListTools(t.Context(), nil)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	if len(res.Tools) != registeredToolCount {
		t.Fatalf("a tool was registered or removed without updating registeredToolCount \" +\n\t\"and the tools/list coverage assertion above: got %d", len(res.Tools))
	}
}

// registeredToolCount is the number of tools New() registers. Bump it
// together with the name list in TestMCP_ToolsList_IncludesFavroPing.
const registeredToolCount = 85

func TestMCP_FavroPing_ReturnsExpectedFields(t *testing.T) {
	t.Parallel()

	cs := connectInMemory(t)

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      pingToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("favro_ping must not return as a tool error")
	}

	out := decodeStructured[PingOutput](t, res)
	if got := out.Server; got != ServerName {
		t.Errorf("out.Server = %v, want %v", got, ServerName)
	}
	if got := out.Version; got != "v0.1.0-test" {
		t.Errorf("out.Version = %v, want %v", got, "v0.1.0-test")
	}
	if got := out.OrganizationID; got != favroapitest.Token().OrganizationID {
		t.Errorf("out.OrganizationID = %v, want %v", got, favroapitest.Token().OrganizationID)
	}
	if got := out.CredentialSource; got != "env" {
		t.Errorf("out.CredentialSource = %v, want %v", got, "env")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	tok := fixtureToken()
	full := serializedResponseString(t, res)

	if strings.Contains(full, tok.Email) {
		t.Errorf("ping response leaked email: %q: %q present", full, tok.Email)
	}
	if strings.Contains(full, tok.APIToken) {
		t.Errorf("ping response leaked API token: %q: %q present", full, tok.APIToken)
	}
	if strings.Contains(strings.ToLower(full), "authorization") {
		t.Errorf("ping response includes the word 'authorization', which suggests a header leaked: %q: %q present", full, "authorization")
	}
}

func TestMCP_RateLimitStatus_NoObservationsYet(t *testing.T) {
	t.Parallel()

	cs := connectInMemory(t)

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      rateLimitToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[RateLimitOutput](t, res)
	if out.HaveSeen {
		t.Error("out.HaveSeen = true, want false")
	}
	if got := out.Remaining; got != -1 {
		t.Errorf("Remaining must distinguish 'not seen' from 'zero': got %v, want %v", got, -1)
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if callRes.IsError {
		t.Error("callRes.IsError = true, want false")
	}

	out := decodeStructured[RateLimitOutput](t, callRes)
	if !out.HaveSeen {
		t.Error("out.HaveSeen = false, want true")
	}
	if got := out.Limit; got != 1000 {
		t.Errorf("out.Limit = %v, want %v", got, 1000)
	}
	if got := out.Remaining; got != 987 {
		t.Errorf("out.Remaining = %v, want %v", got, 987)
	}
	if got := out.LastPath; got != "/anything" {
		t.Errorf("out.LastPath = %v, want %v", got, "/anything")
	}
	if got := out.LastStatus; got != http.StatusOK {
		t.Errorf("out.LastStatus = %v, want %v", got, http.StatusOK)
	}
	if out.LastObservedUnix == 0 {
		t.Error("out.LastObservedUnix = 0, want non-zero")
	}
	if len(out.LastObservedAgo) == 0 {
		t.Fatal("out.LastObservedAgo is empty")
	}
}

// decodeStructured pulls the typed Output out of a CallToolResult.
// The SDK serializes structured output into res.StructuredContent.
func decodeStructured[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	if res.StructuredContent == nil {
		t.Fatal("expected structured output, got nil")
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("json.Unmarshal(raw, &out): %v", err)
	}
	return out
}

// serializedResponseString flattens a CallToolResult into a single
// string so leak-detection assertions can grep across structured + text
// content + every header echoed back.
func serializedResponseString(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	raw, err := json.Marshal(res)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	return string(raw)
}
