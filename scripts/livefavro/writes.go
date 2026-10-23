package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/mmedum/favro-mcp/v4/internal/livecover"
	"github.com/mmedum/favro-mcp/v4/internal/redact"
)

// runWrites checks, against a real organization, the writes the main
// steps can only preview: what confirm: true, the sharing refusals and
// favro_widen_collection_sharing do when they are sent. It writes only
// to a probe tag and a probe collection this run creates, through a
// scratch that refuses any other write, and deletes both.
//
// The client declares form elicitation, so a server that asked the
// person would be heard; the run fails if it asks anything. The probe
// collection is opened to the organization, never made public, and only
// while this run holds it.
func runWrites(lc *lifecycle, bin string, env []string, out *printer, red *redact.Redactor) error {
	c := &checks{out: out}
	heard := &listener{}
	srv, err := lc.launch(bin, nil, heard.ask, env...)
	if err != nil {
		return err
	}
	defer srv.stop()
	tools, err := srv.listTools()
	if err != nil {
		return err
	}

	sc := newScratch(srv, tools)
	// Registered before the first create, so that every probe this run
	// asked for is looked for by name however the run ends. The sweep
	// starts its own server: after a Ctrl-C this one is gone.
	lc.atExit(func() error { return sweepWith(bin, env, sc.probes(), out) })

	if err := probeTag(sc, c); err != nil {
		return err
	}
	if err := probeCollection(sc, c); err != nil {
		return err
	}
	qs := heard.asked()
	c.check(len(qs) == 0, "the server asked the person nothing", strings.Join(qs, " | "))

	out.line("")
	out.line("%d passed, %d failed; %d distinct values redacted", c.passed, c.failed, red.Count())
	if c.failed > 0 {
		return fmt.Errorf("%d checks failed", c.failed)
	}
	return nil
}

// checks prints and counts what the run found.
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

// listener keeps any question the server puts; the session declines it.
type listener struct {
	mu        sync.Mutex
	questions []string
}

func (l *listener) ask(message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.questions = append(l.questions, message)
}

func (l *listener) asked() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.questions...)
}

// detail is what a check prints when it fails: the call's error, or
// the result's text.
func detail(res *callResult, err error) string {
	if err != nil {
		return err.Error()
	}
	return res.text()
}

// succeeded is a call that came back as a result, and refused one that
// came back as an error of class.
// judge decides both, as it does for a step.
func succeeded(res *callResult, err error) bool { return judge(livecover.Step{}, res, err).ok }

func refused(res *callResult, err error, class string) bool {
	return judge(livecover.Step{ExpectError: class}, res, err).ok
}

// probePrefix starts every probe's name.
const probePrefix = "livefavro-"

// probeName is a name nobody takes for theirs, and the one the sweep
// finds the probe by.
func probeName(kind string) string {
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	return probePrefix + kind + "-" + hex.EncodeToString(suffix)
}

// createdID is the id a create returned under key.
func createdID(res *callResult, err error, key string) (string, error) {
	if err != nil {
		return "", err
	}
	if res.IsError {
		return "", errors.New(firstLine(res.text()))
	}
	var out struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(res.StructuredContent, &out); err != nil {
		return "", err
	}
	id, _ := out.Result[key].(string)
	if id == "" {
		return "", fmt.Errorf("the create came back with no %s", key)
	}
	return id, nil
}

// probeTag runs a probe tag through a delete without confirm, which
// must leave it, and one with confirm, which must delete it.
func probeTag(sc *scratch, c *checks) error {
	res, err := sc.create("favro_create_tag", map[string]any{"name": probeName("tag")})
	id, err := createdID(res, err, "tagId")
	if err != nil {
		return fmt.Errorf("creating the probe tag: %w", err)
	}
	tag := map[string]any{"tag_id": id}

	res, err = sc.call("favro_delete_tag", tag)
	c.check(refused(res, err, "blocked"), "favro_delete_tag without confirm is [blocked]", detail(res, err))
	res, err = sc.call("favro_get_tag", tag)
	c.check(succeeded(res, err), "the tag is still there", detail(res, err))

	res, err = sc.call("favro_delete_tag", map[string]any{"tag_id": id, "confirm": true})
	c.check(succeeded(res, err), "favro_delete_tag with confirm deletes it", detail(res, err))
	res, err = sc.call("favro_get_tag", tag)
	c.check(refused(res, err, "not_found"), "the tag is gone", detail(res, err))
	return nil
}

// probeCollection runs a probe collection through the sharing writes:
// public refused on create, a witness that is not its name refused,
// opened to the organization and read back, narrowed again, and deleted
// with confirm.
func probeCollection(sc *scratch, c *checks) error {
	refusedName := probeName("public")
	res, err := sc.create("favro_create_collection", map[string]any{"name": refusedName, "public_sharing": "public"})
	wasRefused := refused(res, err, "blocked")
	c.check(wasRefused, "favro_create_collection with public_sharing public is [blocked]", detail(res, err))
	if !wasRefused {
		// A create that went through opened a collection to anyone with
		// its link: take it down before anything else.
		if id, err := createdID(res, err, "collectionId"); err == nil {
			_, _ = sc.call("favro_delete_collection", map[string]any{"collection_id": id, "confirm": true})
		}
	}
	res, err = sc.call("favro_resolve_collection", map[string]any{"name": refusedName, "force_refresh": true})
	c.check(succeeded(res, err) && strings.Contains(string(res.StructuredContent), `"candidates":[]`),
		"the refused collection was not created", detail(res, err))

	name := probeName("collection")
	res, err = sc.create("favro_create_collection", map[string]any{"name": name})
	id, err := createdID(res, err, "collectionId")
	if err != nil {
		return fmt.Errorf("creating the probe collection: %w", err)
	}
	col := map[string]any{"collection_id": id}
	sharing := func(mode string) bool {
		res, err := sc.call("favro_get_collection", col)
		return succeeded(res, err) && strings.Contains(string(res.StructuredContent), `"publicSharing":"`+mode+`"`)
	}

	res, err = sc.call("favro_widen_collection_sharing", map[string]any{
		"collection_id": id, "collection_name": name + "-not-it", "public_sharing": "organization", "confirm": true,
	})
	c.check(refused(res, err, "conflict"), "favro_widen_collection_sharing with another name is [conflict]", detail(res, err))
	c.check(sharing("users"), "the collection is still shared with users only", "")

	res, err = sc.call("favro_widen_collection_sharing", map[string]any{
		"collection_id": id, "collection_name": name, "public_sharing": "organization", "confirm": true,
	})
	c.check(succeeded(res, err) && strings.Contains(res.text(), `verified by reading the collection back: public_sharing is "organization"`),
		"favro_widen_collection_sharing opens it to the organization and reads it back", detail(res, err))
	c.check(sharing("organization"), "the collection reads back as organization", "")

	res, err = sc.call("favro_update_collection", map[string]any{"collection_id": id, "public_sharing": "users"})
	c.check(succeeded(res, err) && sharing("users"), "favro_update_collection narrows it to users again", detail(res, err))

	res, err = sc.call("favro_delete_collection", col)
	c.check(refused(res, err, "blocked"), "favro_delete_collection without confirm is [blocked]", detail(res, err))
	res, err = sc.call("favro_delete_collection", map[string]any{"collection_id": id, "confirm": true})
	c.check(succeeded(res, err), "favro_delete_collection with confirm deletes it", detail(res, err))
	res, err = sc.call("favro_get_collection", col)
	c.check(refused(res, err, "not_found"), "the collection is gone", detail(res, err))
	return nil
}
