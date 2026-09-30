package tools

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v3/internal/render"
)

// Asking the person (§9.2). A write that cannot be undone, or that sends
// something past the people who can already see it, is put to the person
// through the client, as an MCP form elicitation, when the client
// declares it can ask. The form has no fields: accepting it is the
// confirmation. The question goes out the multi-round-trip way: the
// first call does every read, stops before the write and returns the
// question with a signed requestState; the call comes back with the
// answer and that state, reads again, and writes only on an accept.
// Clients on protocols before 2026-07-28 get the same through the SDK,
// which asks with elicitation/create and calls the handler again within
// the same request.

// askTTL is how long a question may wait for its answer when its state
// travels through the client, from 2026-07-28. An answer after it is
// refused, and the call is made again to ask again.
var askTTL = 5 * time.Minute

// inProcessTTL bounds a state that never leaves the process, before
// 2026-07-28: the request's own context bounds the wait, and this only
// how long its nonce is remembered.
const inProcessTTL = 24 * time.Hour

// statelessProtocol is the first revision whose client carries the
// requestState: the SDK's own test for asking the multi-round-trip way.
const statelessProtocol = "2026-07-28"

// askKey names the one input request.
const askKey = "confirm"

// emptyForm is the question's form: no fields, so the client's accept
// is the answer. The specification types properties as an open map with
// no minimum (§18).
var emptyForm = json.RawMessage(`{"type":"object","properties":{}}`)

// asking signs and redeems the requestState of every question this
// process asks, and knows which tools ask. The key is drawn per process,
// so a state is good only in the process that issued it, and each is
// redeemed at most once.
type asking struct {
	key []byte
	lg  *slog.Logger

	// tools are the tools registered to ask; written only while
	// registering, before the server serves.
	tools map[string]bool

	mu   sync.Mutex
	used map[string]int64 // nonce → when it expires, in Unix seconds
}

func newAsking(lg *slog.Logger) *asking {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return &asking{key: key, lg: lg, used: map[string]int64{}, tools: map[string]bool{}}
}

// asks reports whether the tool was registered to ask.
func (a *asking) asks(tool string) bool { return a.tools[tool] }

// askState is what a requestState carries. It binds the answer to the
// tool, to the call's arguments, and to what the question binds
// (render.Question.Bind).
type askState struct {
	Tool     string `json:"t"`
	Args     string `json:"a"`
	Question string `json:"q"`
	Nonce    string `json:"n"`
	Expires  int64  `json:"e"`
}

// sign is st as base64url JSON, a dot, and its HMAC-SHA256.
func (a *asking) sign(st askState) string {
	payload, _ := json.Marshal(st)
	enc := base64.RawURLEncoding
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(a.mac(payload))
}

func (a *asking) mac(payload []byte) []byte {
	m := hmac.New(sha256.New, a.key)
	m.Write(payload)
	return m.Sum(nil)
}

// redeem checks a requestState a client echoed and spends it. Every
// refusal is [blocked]: whatever the answer was, nothing is written on
// it.
func (a *asking) redeem(state, tool, args string, now time.Time) (askState, error) {
	st, ok := a.verify(state)
	if !ok {
		return askState{}, render.Errorf(render.ClassBlocked,
			"the call came back with an answer to a question this server did not ask; nothing was changed. Call it again without one")
	}
	if st.Tool != tool || st.Args != args {
		return askState{}, render.Errorf(render.ClassBlocked,
			"the answer came back with another call than the one the person was asked about; nothing was changed. Call it again to ask again")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for n, exp := range a.used {
		if now.Unix() > exp {
			delete(a.used, n)
		}
	}
	if now.Unix() > st.Expires {
		return askState{}, render.Errorf(render.ClassBlocked,
			"the question to the person expired before the answer came back; nothing was changed. Call it again to ask again")
	}
	if _, spent := a.used[st.Nonce]; spent {
		return askState{}, render.Errorf(render.ClassBlocked, "that answer was already used once; nothing was changed")
	}
	a.used[st.Nonce] = st.Expires
	return st, nil
}

// verify reads a requestState this process signed, and reports false
// for anything else.
func (a *asking) verify(state string) (askState, bool) {
	var st askState
	enc := base64.RawURLEncoding
	p, s, ok := strings.Cut(state, ".")
	payload, err1 := enc.DecodeString(p)
	sum, err2 := enc.DecodeString(s)
	if !ok || err1 != nil || err2 != nil || !hmac.Equal(sum, a.mac(payload)) || json.Unmarshal(payload, &st) != nil {
		return askState{}, false
	}
	return st, true
}

// person is one call's way to the person: an asking handler asks it
// before its write.
type person struct {
	a       *asking
	tool    string
	in      any
	canAsk  bool
	require bool
	// travels is whether the requestState goes through the client,
	// which is when askTTL applies.
	travels bool
	now     time.Time
	// argSum is the arguments' hash, computed when first needed.
	argSum string

	// answer is set when the call came back with a question this
	// process asked, verified and accepted.
	answer *answer
	// asked is set when the handler asked and the question is to go out.
	asked *render.Question
}

type answer struct {
	question string
	spent    bool
}

// errAsking stops the handler before its write while the question goes
// out. It becomes the input request and never reaches a caller.
var errAsking = render.Sentinel(render.ClassBlocked, "the person has not answered yet; nothing was changed")

// args is the hash of the call's arguments, as the handler decoded them.
func (p *person) args() string {
	if p.argSum == "" {
		raw, _ := json.Marshal(p.in)
		p.argSum = render.Sum(string(raw))
	}
	return p.argSum
}

// personFor sets up one call. A call that carries answers must also
// carry the requestState they belong to, which must be one this process
// issued for this tool and these arguments, unexpired and unspent: a
// client could otherwise answer a question before it was asked. Any
// answer but accept is refused here, before anything runs.
func (a *asking) personFor(req *mcp.CallToolRequest, tool string, in any, require bool) (*person, error) {
	p := &person{a: a, tool: tool, in: in, require: require, now: time.Now(), travels: true}
	if req == nil {
		return p, nil
	}
	p.canAsk, p.travels = clientAsks(req)
	var state string
	var responses mcp.InputResponseMap
	if req.Params != nil {
		state, responses = req.Params.RequestState, req.Params.InputResponses
	}
	switch {
	case state == "" && len(responses) == 0:
		return p, nil
	case state == "":
		return nil, render.Errorf(render.ClassBlocked,
			"the call came with answers to a question this server has not asked; nothing was changed. Call it again without them")
	}
	st, err := a.redeem(state, tool, p.args(), p.now)
	if err != nil {
		return nil, err
	}
	action := answered(responses)
	a.lg.Info("person_answered", "tool", tool, "answer", action)
	if action != "accept" {
		// Refused here, before anything runs: a write whose question
		// depends on what Favro holds may not reach its question on this
		// round, and a decline must stop it all the same.
		return nil, render.Errorf(render.ClassBlocked, "%s was not confirmed by the person: the client answered %s. "+
			"Nothing was changed. Do not call it again unless the person asks for it", tool, action)
	}
	p.answer = &answer{question: st.Question}
	return p, nil
}

// clientAsks reports whether the client can show a form, and whether a
// requestState travels through it, which it does from 2026-07-28.
func clientAsks(req *mcp.CallToolRequest) (canAsk, travels bool) {
	travels = true
	if c := req.ClientCapabilities(); c != nil && c.Elicitation != nil {
		// Form is what an empty elicitation capability declares; only a
		// client that declares URL alone cannot show a form.
		canAsk = c.Elicitation.Form != nil || c.Elicitation.URL == nil
	}
	if req.Session != nil {
		if ip := req.Session.InitializeParams(); ip != nil {
			travels = ip.ProtocolVersion >= statelessProtocol
		}
	}
	return canAsk, travels
}

// answered is the client's answer to the question, as one word for the
// log and the refusal: accept, decline, cancel, other, or none.
func answered(responses mcp.InputResponseMap) string {
	r, ok := responses[askKey].(*mcp.ElicitResult)
	if !ok || r == nil {
		return "none"
	}
	switch r.Action {
	case "accept", "decline", "cancel":
		return r.Action
	}
	return "other"
}

// asks reports whether ask would put a question or refuse, rather than
// let the write go ahead unasked: the client can ask, or the
// configuration requires it.
func (p *person) asks() bool { return p.answer != nil || p.canAsk || p.require }

// ask puts q to the person, or checks the answer the call came back
// with. It returns nil when the write may go ahead.
func (p *person) ask(q render.Question) error {
	if ans := p.answer; ans != nil {
		switch {
		case ans.spent:
			return render.Errorf(render.ClassBlocked, "%s asked the person twice in one call, which is a defect "+
				"in this server; nothing more was changed", p.tool)
		case ans.question != questionSum(q):
			ans.spent = true
			return render.Errorf(render.ClassBlocked, "what %s would do changed after the person was asked, so what "+
				"they saw is not what would be written; nothing was changed. Call it again to ask again", p.tool)
		}
		ans.spent = true
		return nil
	}
	if !p.canAsk {
		if p.require {
			return render.Errorf(render.ClassBlocked, "%s needs the person to confirm it, and this client cannot "+
				"ask them; FAVRO_REQUIRE_PROMPT is set, so nothing was changed. Use a client that supports MCP "+
				"elicitation", p.tool)
		}
		return nil
	}
	p.asked = &q
	return errAsking
}

// inputRequest is the question as the result that asks it.
func (p *person) inputRequest() *mcp.CallToolResult {
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	ttl := askTTL
	if !p.travels {
		ttl = inProcessTTL
	}
	st := askState{
		Tool: p.tool, Args: p.args(), Question: questionSum(*p.asked), Nonce: hex.EncodeToString(nonce),
		Expires: p.now.Add(ttl).Unix(),
	}
	p.a.lg.Info("person_asked", "tool", p.tool)
	return &mcp.CallToolResult{
		InputRequests: mcp.InputRequestMap{askKey: &mcp.ElicitParams{
			Mode: "form", Message: p.asked.Text, RequestedSchema: emptyForm,
		}},
		RequestState: p.a.sign(st),
	}
}

// questionSum binds a state to what the question binds.
func questionSum(q render.Question) string { return render.Sum(q.Bind) }

type personKey struct{}

// confirmFirst is the last step before an asking write, after every
// other check: it puts the question question builds to the person,
// unless the write is a dry run or the person would not be asked. The
// question is built only when it is used, so a call that asks nobody
// pays for none of the reads that name what it acts on. A question with
// no text is nothing to ask: what the builder read showed the write
// widens nothing. A write with no person on its context is refused:
// only a tool registered through addAsking may make one.
func confirmFirst(ctx context.Context, dry bool, question func() (render.Question, error)) error {
	if dry {
		return nil
	}
	p, ok := ctx.Value(personKey{}).(*person)
	if !ok {
		return render.Errorf(render.ClassBlocked, "this write has no way to ask the person, which is a defect in "+
			"this server; nothing was changed")
	}
	if !p.asks() {
		return nil
	}
	q, err := question()
	if err != nil || q.Text == "" {
		return err
	}
	return p.ask(q)
}

// addAsking registers a tool whose handler asks the person before its
// write, through confirmFirst. It is the only way to put a person on a
// handler's context, so a handler that asks, registered any other way,
// is refused as a defect rather than run unasked.
func addAsking[In, Out any](reg *registry, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	if !reg.admits(t) {
		return
	}
	name := t.Name
	reg.asking.tools[name] = true
	addTool(reg, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		p, err := reg.asking.personFor(req, name, in, reg.requirePrompt)
		if err != nil {
			return nil, zero, err
		}
		ctx = context.WithValue(ctx, personKey{}, p)
		if p.answer != nil {
			// The person accepted: from here the write may happen whether
			// or not this round reaches its question again, so a failure
			// to reply is no longer "nothing was changed".
			setStage(ctx, stageWriting)
		}
		res, out, err := h(ctx, req, in)
		if p.asked != nil {
			// The handler stopped before its write; the question goes out.
			setStage(ctx, stageWaiting)
			return p.inputRequest(), zero, nil
		}
		if err == nil && stageOf(ctx) == stageWriting {
			setStage(ctx, stageWritten)
		}
		return res, out, err
	})
}

// The stages of one tools/call that asked, for askFailures: what a
// failure to reply means depends on how far the call got.
const (
	stageNone    int32 = iota
	stageWaiting       // the question is out; nothing is written before the answer
	stageWriting       // the answer confirmed the write, which may have happened
	stageWritten       // the handler returned after writing
)

type stageKey struct{}

func setStage(ctx context.Context, s int32) {
	if v, ok := ctx.Value(stageKey{}).(*atomic.Int32); ok {
		v.Store(s)
	}
}

func stageOf(ctx context.Context) int32 {
	if v, ok := ctx.Value(stageKey{}).(*atomic.Int32); ok {
		return v.Load()
	}
	return stageNone
}

// askFailures is receiving middleware for every tools/call. A call to a
// tool that asks nothing, carrying an answer, is refused before it runs.
// A call that asked the person and then failed as a JSON-RPC error
// rather than a tool result becomes a tool error: on protocols before
// 2026-07-28 the SDK asks with elicitation/create itself, and a client
// that answers with an error, or an answer that does not fit the form,
// fails the whole call that way. That call wrote nothing, since its
// write waits for the answer, and it becomes [blocked]. A failure after
// the answer confirmed the write — the reply could not be built or sent,
// the request was canceled — may follow a write, so it becomes
// [ambiguous_outcome] and is never "nothing was changed". The client's
// error text is not repeated.
func askFailures(a *asking) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			call, ok := req.(*mcp.CallToolRequest)
			if !ok {
				return next(ctx, method, req)
			}
			name := "the call"
			if call.Params != nil && toolName(call.Params.Name) {
				name = call.Params.Name
			}
			if p := call.Params; p != nil && (p.RequestState != "" || len(p.InputResponses) > 0) && !a.asks(p.Name) {
				return errorResult(render.Errorf(render.ClassBlocked, "%s is not a tool here that asks the person, "+
					"and the call came with an answer; nothing was done. Call it again without one", name)), nil
			}
			stage := &atomic.Int32{}
			res, err := next(context.WithValue(ctx, stageKey{}, stage), method, req)
			if err == nil || stage.Load() == stageNone {
				return res, err
			}
			return errorResult(failedAfterAsking(stage.Load(), name)), nil
		}
	}
}

// failedAfterAsking is what a call that asked the person and then
// failed outside a tool result means, by how far it got.
func failedAfterAsking(stage int32, name string) error {
	switch stage {
	case stageWaiting:
		return render.Errorf(render.ClassBlocked, "%s was not confirmed by the person: the client could not "+
			"put the question to them, or its answer could not be read. Nothing was changed", name)
	case stageWritten:
		return render.Errorf(render.ClassAmbiguousOutcome, "the person confirmed %s, and it was written "+
			"(verdict: written), but its result could not be returned. Do not make the call again", name)
	}
	return render.Errorf(render.ClassAmbiguousOutcome, "the person confirmed %s, and the server went on "+
		"to write, but the call ended before its result (verdict: unknown). Do not make the call again; "+
		"read what it acted on to see whether it took effect", name)
}

// errorResult is err as a tool result, the way addTool makes one from a
// handler's error.
func errorResult(err error) *mcp.CallToolResult {
	res := &mcp.CallToolResult{}
	res.SetError(render.Error(err))
	return res
}

// toolName is true for a name shaped like this server's tools, which a
// message may repeat; anything else came from the client.
func toolName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// asksFirst is the sentence every asking tool's description ends with,
// and asksFirstWhen the same for a tool that asks only when cond holds.
var asksFirst = asksFirstWhen("")

func asksFirstWhen(cond string) string {
	lead := " Before the write"
	if cond != "" {
		lead = " " + cond
	}
	return lead + ", the server asks the person through the client when it can, showing what would go; " +
		"a call they did not confirm is [blocked], and is not made again unless they ask."
}
