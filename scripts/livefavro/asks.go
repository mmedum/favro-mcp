package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/mmedum/favro-mcp/v3/internal/livecover"
	"github.com/mmedum/favro-mcp/v3/internal/redact"
)

// runAsks checks asking the person against a real organization, and
// deletes nothing this run did not create.
//
// On 2025-11-25 the SDK asks inside the call with elicitation/create. A
// probe tag this run creates is asked about and declined, which must
// leave it, then asked about and accepted, which must delete it.
//
// The uploads and making a collection public are asked about with real
// names and declined, so nothing is written. A collection that is
// already public is not asked about, and would be written to, so it is
// skipped. The 2026-07-28 round trip, where the call returns the
// question instead, is the in-memory tests': it is stateless, with
// server/discover and per-request _meta, which this client does not
// speak.
func runAsks(bin string, env []string, out *printer, red *redact.Redactor) error {
	c := &checks{out: out}
	if err := askTag(bin, env, c); err != nil {
		return err
	}
	if err := askDeclined(bin, env, c); err != nil {
		return err
	}

	out.line("")
	out.line("%d passed, %d failed, %d skipped; %d distinct values redacted",
		c.passed, c.failed, c.skipped, red.Count())
	if c.failed > 0 {
		return fmt.Errorf("%d checks failed", c.failed)
	}
	return nil
}

// checks prints and counts what the asking run found.
type checks struct {
	tally
	out *printer
}

func (c *checks) check(ok bool, what, detail string) {
	if ok {
		c.passed++
		c.out.line("ok   %s", what)
		return
	}
	c.failed++
	c.out.line("FAIL %s: %s", what, firstLine(detail))
}

func (c *checks) skip(tool, why string) {
	c.skipped++
	c.out.line("SKIP %-38s %s", tool, why)
}

// person answers the questions the server puts, with the answer set
// last, and keeps them.
type person struct {
	mu        sync.Mutex
	action    string
	questions []string
}

func (p *person) answer(message string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.questions = append(p.questions, message)
	return p.action
}

// next sets the answer to the next question and forgets the ones asked.
func (p *person) next(action string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.action, p.questions = action, nil
}

func (p *person) asked() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.questions...)
}

// succeeded is a call that came back as a result, and refused one that
// came back as an error of class.
// judge decides both, as it does for a step.
func succeeded(res *callResult, err error) bool { return judge(livecover.Step{}, res, err).ok }

func refused(res *callResult, err error, class string) bool {
	return judge(livecover.Step{ExpectError: class}, res, err).ok
}

// askTag runs the probe tag through a decline and an accept.
func askTag(bin string, env []string, c *checks) error {
	p := &person{}
	srv, err := startWith(bin, p.answer, env...)
	if err != nil {
		return err
	}
	defer srv.stop()

	id, name, err := createProbeTag(srv)
	if err != nil {
		return err
	}
	gone := false
	defer func() {
		if !gone {
			// A failed check must not leave the probe behind.
			p.next("accept")
			_, _ = srv.call("favro_delete_tag", map[string]any{"tag_id": id})
		}
	}()
	tag := map[string]any{"tag_id": id}

	p.next("decline")
	res, err := srv.call("favro_delete_tag", tag)
	qs := p.asked()
	c.check(refused(res, err, "blocked"), "favro_delete_tag declined is [blocked]", res.text())
	c.check(len(qs) == 1 && strings.Contains(qs[0], "delete the tag `"+name+"`"),
		"the question names the probe tag, read from Favro", strings.Join(qs, " | "))
	res, err = srv.call("favro_get_tag", tag)
	c.check(succeeded(res, err), "the declined tag is still there", res.text())

	p.next("accept")
	res, err = srv.call("favro_delete_tag", tag)
	c.check(succeeded(res, err) && len(p.asked()) == 1, "favro_delete_tag accepted deletes it", res.text())
	res, err = srv.call("favro_get_tag", tag)
	gone = refused(res, err, "not_found")
	c.check(gone, "the accepted tag is gone", res.text())
	return nil
}

// createProbeTag creates the tag askTag asks about, named so nobody
// takes it for theirs.
func createProbeTag(srv *session) (id, name string, err error) {
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	name = "livefavro-ask-" + hex.EncodeToString(suffix)
	res, err := srv.call("favro_create_tag", map[string]any{"name": name})
	if err != nil {
		return "", "", fmt.Errorf("creating the probe tag: %w", err)
	}
	if res.IsError {
		return "", "", fmt.Errorf("creating the probe tag: %s", firstLine(res.text()))
	}
	var created struct {
		Result struct {
			TagID string `json:"tagId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res.StructuredContent, &created); err != nil || created.Result.TagID == "" {
		return "", "", errors.New("the probe tag came back with no tagId")
	}
	return created.Result.TagID, name, nil
}

// declinedCase is a question askDeclined puts and declines, and words
// it must carry.
type declinedCase struct {
	tool  string
	args  map[string]any
	shows string
}

var declinedCases = []declinedCase{
	{
		"favro_upload_attachment",
		map[string]any{"card_id": livecover.AnyCardID, "file_path": livecover.UploadFile},
		"send the file `" + livecover.UploadFile + "` (16 bytes) from this computer to Favro, attached to the card `",
	},
	{
		"favro_upload_comment_attachment",
		map[string]any{"comment_id": livecover.AnyCommentID, "file_path": livecover.UploadFile},
		"attached to a comment?",
	},
	{
		"favro_create_collection",
		map[string]any{"name": "livefavro-ask-probe", "public_sharing": "public"},
		"create the collection `livefavro-ask-probe` as public?",
	},
	{
		"favro_update_collection",
		map[string]any{"collection_id": livecover.AnyCollectionID, "public_sharing": "public"},
		"make the collection `",
	},
}

// askDeclined puts the non-destructive questions with real names and
// declines each.
func askDeclined(bin string, env []string, c *checks) error {
	p := &person{action: "decline"}
	srv, err := startWith(bin, p.answer, env...)
	if err != nil {
		return err
	}
	defer srv.stop()

	pool, err := seed(srv, c.out)
	if err != nil {
		return err
	}
	for _, dc := range declinedCases {
		args, missing := pool.resolve(dc.args)
		switch {
		case missing != "":
			c.skip(dc.tool, "no "+missing+" seen yet in this organization")
			continue
		case dc.tool == "favro_update_collection" && !privateCollection(srv, args["collection_id"]):
			c.skip(dc.tool, "the collection is already public, or unreadable")
			continue
		}
		p.next("decline")
		res, err := srv.call(dc.tool, args)
		qs := p.asked()
		c.check(refused(res, err, "blocked") && len(qs) == 1 && strings.Contains(qs[0], dc.shows),
			dc.tool+" asks with real names, and declined writes nothing", fmt.Sprint(err, " ", len(qs), " ", res.text()))
	}

	// Read back rather than trust the refusal: no collection of the
	// probe's name exists.
	res, err := srv.call("favro_resolve_collection", map[string]any{"name": "livefavro-ask-probe", "force_refresh": true})
	c.check(succeeded(res, err) && strings.Contains(string(res.StructuredContent), `"candidates":[]`),
		"the declined collection was not created", res.text())
	return nil
}

// seed runs the seed steps, silently, for ids this organization has.
func seed(srv *session, out *printer) (*pool, error) {
	tools, err := srv.listTools()
	if err != nil {
		return nil, err
	}
	// The seeds' own lines are the main run's transcript, not this one's.
	quiet := &printer{red: out.red, w: bufio.NewWriter(io.Discard)}
	p := newPool()
	runSteps(srv, quiet, p, livecover.SeedSteps(), tools, false)
	return p, nil
}

// privateCollection reports whether a collection can be read and is not
// public: making one public that already is asks nothing, and writes.
func privateCollection(srv *session, id any) bool {
	res, err := srv.call("favro_get_collection", map[string]any{"collection_id": id})
	return succeeded(res, err) && !strings.Contains(string(res.StructuredContent), `"publicSharing":"public"`)
}
