package tools

import (
	"context"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v3/internal/favroapi"
)

// The protocols a question goes out on: before 2026-07-28 the SDK asks
// with elicitation/create inside the call; from it, the call returns the
// question and comes back with the answer (§9.2).
var protocols = []string{"2025-06-18", "2025-11-25", "2026-07-28"}

// answerer answers the questions a test client is asked, and keeps them.
type answerer struct {
	mu        sync.Mutex
	questions []*mcp.ElicitParams
	action    string
}

func (p *answerer) handle(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.questions = append(p.questions, req.Params)
	return &mcp.ElicitResult{Action: p.action}, nil
}

func (p *answerer) asked() []*mcp.ElicitParams {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.questions)
}

// fakeFavro holds what the asking tools read, and records every request.
type fakeFavro struct {
	mu       sync.Mutex
	requests []string // "METHOD /path"
	tagName  string
	sharing  string // the collection's publicSharing
}

func (f *fakeFavro) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	tag, sharing := f.tagName, f.sharing
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	body := map[string]string{
		"GET /tags/tg-1":                 `{"tagId":"tg-1","name":"` + tag + `"}`,
		"GET /collections/co-1":          `{"collectionId":"co-1","name":"Roadmap","publicSharing":"` + sharing + `"}`,
		"GET /widgets/wi-1":              `{"widgetCommonId":"wi-1","name":"Sprint board"}`,
		"GET /groups/gr-1":               `{"groupId":"gr-1","name":"Design"}`,
		"GET /cards/ca-1":                `{"cardId":"ca-1","cardCommonId":"cc-1","name":"Ship the release"}`,
		"GET /comments/cm-1":             `{"commentId":"cm-1","comment":"see the attached draft"}`,
		"GET /webhooks":                  `[{"webhookId":"wh-1","name":"Deploys","postToUrl":"https://hooks.example/in"}]`,
		"DELETE /cards/ca-1":             `["ca-1"]`,
		"POST /cards/ca-1/attachment":    `{"name":"notes.txt","fileURL":"https://favro.invalid/f"}`,
		"POST /comments/cm-1/attachment": `{"name":"notes.txt","fileURL":"https://favro.invalid/f"}`,
		"POST /collections":              `{"collectionId":"co-2","name":"Launch","publicSharing":"public"}`,
		"PUT /collections/co-1":          `{"collectionId":"co-1","name":"Roadmap","publicSharing":"public"}`,
		"DELETE /tags/tg-1":              ``,
		"DELETE /collections/co-1":       ``,
		"DELETE /widgets/wi-1":           ``,
		"DELETE /groups/gr-1":            ``,
		"DELETE /webhooks/wh-1":          ``,
	}[r.Method+" "+r.URL.Path]
	if body == "" && r.Method == http.MethodGet {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_, _ = w.Write([]byte(body))
}

// count is how many requests were made with this method and path.
func (f *fakeFavro) count(methodPath string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if r == methodPath {
			n++
		}
	}
	return n
}

func (f *fakeFavro) made() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// uploadFile is the file every upload case sends, in the upload
// directory of the test.
const uploadFile = "notes.txt"

// connectAsking connects a client on protocol to a server with the whole
// surface over a fresh fake. A nil p declares no elicitation; adjust
// changes the options, and more the client's.
func connectAsking(t *testing.T, protocol string, p *answerer, adjust func(*Options), more ...func(*mcp.ClientOptions)) (*mcp.ClientSession, *fakeFavro, string) {
	t.Helper()
	fake := &fakeFavro{tagName: "Urgent", sharing: "users"}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, uploadFile), []byte("draft notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := Options{Destructive: true, UploadDir: dir, CredentialSource: "env", Version: "test"}
	if adjust != nil {
		adjust(&opts)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: ServerName, Version: "test"}, nil)
	Register(srv, favroFixture(t, fake), opts)
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	o := &mcp.ClientOptions{}
	if p != nil {
		o.ElicitationHandler = p.handle
	}
	for _, fn := range more {
		fn(o)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, o).
		Connect(context.Background(), ct, &mcp.ClientSessionOptions{ProtocolVersion: protocol})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, fake, dir
}

// askCase is a call that reaches its tool's write, the write as the
// fake records it, and words its question must carry.
type askCase struct {
	args  map[string]any
	write string
	shows []string
}

var askCases = map[string]askCase{
	deleteTagToolName: {
		args: map[string]any{"tag_id": "tg-1"}, write: "DELETE /tags/tg-1",
		shows: []string{"delete the tag `Urgent` from the whole organization", "no undo"},
	},
	deleteCollectionToolName: {
		args: map[string]any{"collection_id": "co-1"}, write: "DELETE /collections/co-1",
		shows: []string{"delete the collection `Roadmap`"},
	},
	deleteWidgetToolName: {
		args: map[string]any{"widget_common_id": "wi-1"}, write: "DELETE /widgets/wi-1",
		shows: []string{"delete the widget `Sprint board` from every collection", "cards on it are removed"},
	},
	deleteGroupToolName: {
		args: map[string]any{"group_id": "gr-1"}, write: "DELETE /groups/gr-1",
		shows: []string{"delete the group `Design`"},
	},
	deleteWebhookToolName: {
		args: map[string]any{"webhook_id": "wh-1"}, write: "DELETE /webhooks/wh-1",
		shows: []string{"delete the webhook `Deploys`", "`https[:]//hooks[.]example/in`"},
	},
	deleteCardToolName: {
		args: map[string]any{"card_id": "ca-1", "everywhere": true}, write: "DELETE /cards/ca-1",
		shows: []string{"delete the card `Ship the release` from every widget"},
	},
	uploadAttachmentToolName: {
		args: map[string]any{"card_id": "ca-1", "file_path": uploadFile}, write: "POST /cards/ca-1/attachment",
		shows: []string{"send the file `notes.txt` (11 bytes) from this computer", "the card `Ship the release`"},
	},
	uploadCommentAttachmentToolName: {
		args: map[string]any{"comment_id": "cm-1", "file_path": uploadFile}, write: "POST /comments/cm-1/attachment",
		shows: []string{"attached to a comment", "`see the attached draft`"},
	},
	createCollectionToolName: {
		args: map[string]any{"name": "Launch", "public_sharing": "public"}, write: "POST /collections",
		shows: []string{"create the collection `Launch` as public", "Anyone on the internet"},
	},
	updateCollectionToolName: {
		args: map[string]any{"collection_id": "co-1", "public_sharing": "public"}, write: "PUT /collections/co-1",
		shows: []string{"make the collection `Roadmap` public"},
	},
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func callTool(t *testing.T, cs *mcp.ClientSession, p *mcp.CallToolParams) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), p)
	if err != nil {
		t.Fatalf("calling %s: %v", p.Name, err)
	}
	return res
}

// Declined, nothing is written; accepted, the write is made once. On
// every protocol, for every tool that asks, and the question says what
// the write would do.
func TestEveryAskingWriteWaitsForThePerson(t *testing.T) {
	for name, c := range askCases {
		for _, protocol := range protocols {
			for _, action := range []string{"decline", "cancel", "accept"} {
				p := &answerer{action: action}
				cs, fake, _ := connectAsking(t, protocol, p, nil)
				res := callTool(t, cs, &mcp.CallToolParams{Name: name, Arguments: maps.Clone(c.args)})
				out := text(res)
				qs := p.asked()
				if len(qs) != 1 {
					t.Fatalf("%s %s %s: asked %d times: %s", name, protocol, action, len(qs), out)
				}
				for _, want := range c.shows {
					if !strings.Contains(qs[0].Message, want) {
						t.Errorf("%s: the question does not say %q:\n%s", name, want, qs[0].Message)
					}
				}
				n := fake.count(c.write)
				if action != "accept" {
					if !res.IsError || !strings.HasPrefix(out, "[blocked]") || !strings.Contains(out, "not confirmed by the person") || n != 0 {
						t.Errorf("%s %s %s: %d writes: %s", name, protocol, action, n, out)
					}
					continue
				}
				if res.IsError || n != 1 {
					t.Errorf("%s %s accepted: %d writes: %s", name, protocol, n, out)
				}
			}
		}
	}
}

// The tools that ask are found by asking each registered tool: a call
// that carries an answer is refused by the middleware for a tool that
// asks nothing, and by the tool itself for one that does. Every one of
// them has a case, and every case is one of them.
func TestEveryAskingToolHasACase(t *testing.T) {
	cs, fake, _ := connectAsking(t, "2026-07-28", nil, nil)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) < registeredToolCount {
		t.Fatalf("%d tools listed; the whole surface was not registered", len(res.Tools))
	}
	asking := map[string]bool{}
	for _, tool := range res.Tools {
		// The smoke row's arguments, so the call clears the schema and
		// reaches what refuses the answer.
		out := text(callTool(t, cs, &mcp.CallToolParams{
			Name:      tool.Name,
			Arguments: substituteSmokePlaceholders(smokeToolInputs[tool.Name], uploadFile), InputResponses: accepted,
		}))
		switch {
		case strings.Contains(out, "not a tool here that asks the person"):
		case strings.Contains(out, "answers to a question this server has not asked"):
			asking[tool.Name] = true
		default:
			t.Errorf("%s: an answer with no question was not refused: %s", tool.Name, out)
		}
	}
	if len(asking) < 10 {
		t.Fatalf("found %d asking tools", len(asking))
	}
	for name := range asking {
		if _, ok := askCases[name]; !ok {
			t.Errorf("%s asks the person and has no asking case", name)
		}
	}
	for name := range askCases {
		if !asking[name] {
			t.Errorf("%s has an asking case and does not ask", name)
		}
	}
	if n := fake.made(); n != 0 {
		t.Errorf("%d Favro requests from calls that carried an unasked answer", n)
	}
}

// A client that cannot ask gets no question, and the arguments are the
// guard; FAVRO_REQUIRE_PROMPT refuses the write instead.
func TestAClientThatCannotAsk(t *testing.T) {
	for _, require := range []bool{false, true} {
		cs, fake, _ := connectAsking(t, "", nil, func(o *Options) { o.RequirePrompt = require })
		c := askCases[deleteTagToolName]
		res := callTool(t, cs, &mcp.CallToolParams{Name: deleteTagToolName, Arguments: maps.Clone(c.args)})
		n := fake.count(c.write)
		switch {
		case require && (!res.IsError || !strings.Contains(text(res), "FAVRO_REQUIRE_PROMPT") || n != 0):
			t.Errorf("required: %d writes: %s", n, text(res))
		case !require && (res.IsError || n != 1 || fake.count("GET /tags/tg-1") != 0):
			t.Errorf("not required: %d writes, %d reads: %s", n, fake.count("GET /tags/tg-1"), text(res))
		}
	}
}

// A dry run, a write that widens nothing, and a delete of one widget's
// copy of a card ask nothing.
func TestWhatAsksNothing(t *testing.T) {
	p := &answerer{action: "decline"}
	cs, fake, _ := connectAsking(t, "2026-07-28", p, nil)
	calls := []*mcp.CallToolParams{
		{Name: createCollectionToolName, Arguments: map[string]any{"name": "Launch", "public_sharing": "organization"}},
		{Name: createCollectionToolName, Arguments: map[string]any{"name": "Launch"}},
		{Name: updateCollectionToolName, Arguments: map[string]any{"collection_id": "co-1", "name": "Renamed"}},
		{Name: deleteCardToolName, Arguments: map[string]any{"card_id": "ca-1"}},
	}
	for name, c := range askCases {
		args := maps.Clone(c.args)
		args["dry_run"] = true
		calls = append(calls, &mcp.CallToolParams{Name: name, Arguments: args})
	}
	for _, call := range calls {
		if res := callTool(t, cs, call); res.IsError {
			t.Errorf("%s %v: %s", call.Name, call.Arguments, text(res))
		}
	}
	if qs := p.asked(); len(qs) != 0 {
		t.Errorf("asked %d questions: %s", len(qs), qs[0].Message)
	}
	// The calls above that are not dry runs made one card delete, two
	// creates and one update; the dry runs made nothing more.
	made := map[string]int{"DELETE /cards/ca-1": 1, "POST /collections": 2, "PUT /collections/co-1": 1}
	for _, c := range askCases {
		if n := fake.count(c.write); n != made[c.write] {
			t.Errorf("%s made %d times, want %d", c.write, n, made[c.write])
		}
	}

	// A collection already public is not made public again.
	fake.mu.Lock()
	fake.sharing = "public"
	fake.mu.Unlock()
	c := askCases[updateCollectionToolName]
	if res := callTool(t, cs, &mcp.CallToolParams{Name: updateCollectionToolName, Arguments: maps.Clone(c.args)}); res.IsError {
		t.Errorf("already public: %s", text(res))
	}
	if qs := p.asked(); len(qs) != 0 {
		t.Errorf("already public, and asked: %s", qs[0].Message)
	}
}

// The binary's --dry-run asks nothing either.
func TestAForcedDryRunAsksNothing(t *testing.T) {
	p := &answerer{action: "decline"}
	fake := &fakeFavro{tagName: "Urgent"}
	client := favroFixture(t, fake)
	client.ForceDryRun = true
	if !client.DryRun(context.Background()) || favroFixture(t, fake).DryRun(context.Background()) ||
		!favroFixture(t, fake).DryRun(favroapi.WithDryRun(context.Background())) {
		t.Fatal("dryRun does not follow the client and the context")
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: ServerName, Version: "test"}, nil)
	Register(srv, client, Options{Destructive: true})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, &mcp.ClientOptions{ElicitationHandler: p.handle}).
		Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	res := callTool(t, cs, &mcp.CallToolParams{Name: deleteTagToolName, Arguments: map[string]any{"tag_id": "tg-1"}})
	if res.IsError || len(p.asked()) != 0 || fake.made() != 0 {
		t.Errorf("asked %d, %d requests: %s", len(p.asked()), fake.made(), text(res))
	}
}

// mrtr connects a 2026-07-28 client that hands each question back
// instead of answering it, so a test can answer by hand.
func mrtr(t *testing.T) (*mcp.ClientSession, *fakeFavro, string) {
	t.Helper()
	return connectAsking(t, "2026-07-28", &answerer{action: "accept"}, nil, func(o *mcp.ClientOptions) {
		o.MultiRoundTrip = &mcp.MultiRoundTripOptions{Disabled: true}
	})
}

var accepted = mcp.InputResponseMap{askKey: &mcp.ElicitResult{Action: "accept"}}

// The first round only asks. The answer counts once, only with the state
// it was asked with, only for that call, and only while fresh.
func TestTheAnswerIsBoundToItsQuestion(t *testing.T) {
	cs, fake, _ := mrtr(t)
	c := askCases[deleteTagToolName]
	first := callTool(t, cs, &mcp.CallToolParams{Name: deleteTagToolName, Arguments: maps.Clone(c.args)})
	q, ok := first.InputRequests[askKey].(*mcp.ElicitParams)
	if !first.NeedsInput() || !ok || q.Mode != "form" || first.RequestState == "" || fake.count(c.write) != 0 {
		t.Fatalf("first round %+v; %d writes", first, fake.count(c.write))
	}
	state := first.RequestState

	blocked := func(p *mcp.CallToolParams, want string) {
		t.Helper()
		res := callTool(t, cs, p)
		if out := text(res); !res.IsError || !strings.HasPrefix(out, "[blocked]") || !strings.Contains(out, want) {
			t.Errorf("%s", out)
		}
	}
	blocked(&mcp.CallToolParams{Name: deleteTagToolName, Arguments: c.args, InputResponses: accepted},
		"answers to a question this server has not asked")
	blocked(&mcp.CallToolParams{Name: deleteTagToolName, Arguments: c.args, InputResponses: accepted, RequestState: state + "x"},
		"did not ask")
	blocked(&mcp.CallToolParams{
		Name: deleteTagToolName, Arguments: c.args, InputResponses: accepted,
		RequestState: "e30." + strings.Split(state, ".")[1],
	}, "did not ask")
	blocked(&mcp.CallToolParams{
		Name: deleteTagToolName, Arguments: map[string]any{"tag_id": "tg-2"}, InputResponses: accepted,
		RequestState: state,
	}, "another call")
	blocked(&mcp.CallToolParams{
		Name: deleteGroupToolName, Arguments: map[string]any{"group_id": "gr-1"}, InputResponses: accepted,
		RequestState: state,
	}, "another call")
	blocked(&mcp.CallToolParams{
		Name: listTagsToolName, Arguments: map[string]any{}, InputResponses: accepted,
		RequestState: state,
	}, "not a tool here that asks the person")
	if fake.count(c.write) != 0 {
		t.Fatalf("%d writes before the answer", fake.count(c.write))
	}

	done := callTool(t, cs, &mcp.CallToolParams{Name: deleteTagToolName, Arguments: c.args, InputResponses: accepted, RequestState: state})
	if done.IsError || fake.count(c.write) != 1 {
		t.Fatalf("the verified retry: %s; %d writes", text(done), fake.count(c.write))
	}
	blocked(&mcp.CallToolParams{Name: deleteTagToolName, Arguments: c.args, InputResponses: accepted, RequestState: state}, "already used")
}

// Any answer but an accept is refused before the call reads anything.
func TestARefusalIsRefusedBeforeAnyRead(t *testing.T) {
	for _, action := range []string{"decline", "cancel", "maybe"} {
		cs, fake, _ := mrtr(t)
		c := askCases[uploadAttachmentToolName]
		first := callTool(t, cs, &mcp.CallToolParams{Name: uploadAttachmentToolName, Arguments: c.args})
		before := fake.made()
		res := callTool(t, cs, &mcp.CallToolParams{
			Name: uploadAttachmentToolName, Arguments: c.args, RequestState: first.RequestState,
			InputResponses: mcp.InputResponseMap{askKey: &mcp.ElicitResult{Action: action}},
		})
		if out := text(res); !res.IsError || !strings.HasPrefix(out, "[blocked]") || !strings.Contains(out, "not confirmed by the person") {
			t.Errorf("%s: %s", action, out)
		}
		if n := fake.made(); before == 0 || n != before {
			t.Errorf("%s: %d Favro calls after the answer (%d before)", action, n-before, before)
		}
	}
}

// What the person saw is what is written: a tag renamed, or a file
// changed, between the question and the answer is refused, and the next
// call asks again.
func TestAChangeAfterTheQuestionIsRefused(t *testing.T) {
	t.Run("renamed", func(t *testing.T) {
		cs, fake, _ := mrtr(t)
		c := askCases[deleteTagToolName]
		first := callTool(t, cs, &mcp.CallToolParams{Name: deleteTagToolName, Arguments: c.args})
		fake.mu.Lock()
		fake.tagName = "Someone else's tag"
		fake.mu.Unlock()
		res := callTool(t, cs, &mcp.CallToolParams{
			Name: deleteTagToolName, Arguments: c.args, InputResponses: accepted,
			RequestState: first.RequestState,
		})
		if out := text(res); !res.IsError || !strings.Contains(out, "changed after the person was asked") || fake.count(c.write) != 0 {
			t.Errorf("%s; %d writes", out, fake.count(c.write))
		}
	})
	t.Run("file changed", func(t *testing.T) {
		cs, fake, dir := mrtr(t)
		c := askCases[uploadAttachmentToolName]
		first := callTool(t, cs, &mcp.CallToolParams{Name: uploadAttachmentToolName, Arguments: c.args})
		if err := os.WriteFile(filepath.Join(dir, uploadFile), []byte("other notes"), 0o600); err != nil {
			t.Fatal(err)
		}
		res := callTool(t, cs, &mcp.CallToolParams{
			Name: uploadAttachmentToolName, Arguments: c.args, InputResponses: accepted,
			RequestState: first.RequestState,
		})
		if out := text(res); !res.IsError || !strings.Contains(out, "changed after the person was asked") || fake.count(c.write) != 0 {
			t.Errorf("%s; %d writes", out, fake.count(c.write))
		}
	})
}

// A state that travels through the client expires; one that stays in
// the process, before 2026-07-28, waits as long as the request does.
func TestALateAnswerIsRefusedOnlyWhenTheStateTravels(t *testing.T) {
	was := askTTL
	askTTL = -time.Minute
	t.Cleanup(func() { askTTL = was })
	cs, fake, _ := mrtr(t)
	c := askCases[deleteTagToolName]
	first := callTool(t, cs, &mcp.CallToolParams{Name: deleteTagToolName, Arguments: c.args})
	res := callTool(t, cs, &mcp.CallToolParams{
		Name: deleteTagToolName, Arguments: c.args, InputResponses: accepted,
		RequestState: first.RequestState,
	})
	if out := text(res); !res.IsError || !strings.Contains(out, "expired") || fake.count(c.write) != 0 {
		t.Fatalf("%s; %d writes", out, fake.count(c.write))
	}
	for _, protocol := range []string{"2025-06-18", "2025-11-25"} {
		cs, fake, _ := connectAsking(t, protocol, &answerer{action: "accept"}, nil)
		res := callTool(t, cs, &mcp.CallToolParams{Name: deleteTagToolName, Arguments: maps.Clone(c.args)})
		if res.IsError || fake.count(c.write) != 1 {
			t.Errorf("%s: a slow accept in the process was refused: %s", protocol, text(res))
		}
	}
}
