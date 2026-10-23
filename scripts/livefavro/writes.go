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
// to a probe tag and a probe collection this run creates, and deletes
// both.
//
// The client declares form elicitation, so a server that asked the
// person would be heard; the run fails if it asks anything. The probe
// collection is opened to the organization, never made public, and only
// while this run holds it.
func runWrites(bin string, env []string, out *printer, red *redact.Redactor) error {
	c := &checks{out: out}
	heard := &listener{}
	srv, err := startWith(bin, heard.ask, env...)
	if err != nil {
		return err
	}
	defer srv.stop()

	if err := probeTag(srv, c); err != nil {
		return err
	}
	if err := probeCollection(srv, c); err != nil {
		return err
	}
	qs := heard.asked()
	c.check(len(qs) == 0, "the server asked the person nothing", strings.Join(qs, " | "))

	out.line("")
	out.line("%d passed, %d failed, %d skipped; %d distinct values redacted",
		c.passed, c.failed, c.skipped, red.Count())
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

// listener keeps any question the server puts, and declines it.
type listener struct {
	mu        sync.Mutex
	questions []string
}

func (l *listener) ask(message string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.questions = append(l.questions, message)
	return "decline"
}

func (l *listener) asked() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.questions...)
}

// succeeded is a call that came back as a result, and refused one that
// came back as an error of class.
// judge decides both, as it does for a step.
func succeeded(res *callResult, err error) bool { return judge(livecover.Step{}, res, err).ok }

func refused(res *callResult, err error, class string) bool {
	return judge(livecover.Step{ExpectError: class}, res, err).ok
}

// probeName is a name nobody takes for theirs.
func probeName(kind string) string {
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	return "livefavro-" + kind + "-" + hex.EncodeToString(suffix)
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
func probeTag(srv *session, c *checks) error {
	res, err := srv.call("favro_create_tag", map[string]any{"name": probeName("tag")})
	id, err := createdID(res, err, "tagId")
	if err != nil {
		return fmt.Errorf("creating the probe tag: %w", err)
	}
	gone := false
	defer func() {
		if !gone {
			// A failed check must not leave the probe behind.
			_, _ = srv.call("favro_delete_tag", map[string]any{"tag_id": id, "confirm": true})
		}
	}()
	tag := map[string]any{"tag_id": id}

	res, err = srv.call("favro_delete_tag", tag)
	c.check(refused(res, err, "blocked"), "favro_delete_tag without confirm is [blocked]", res.text())
	res, err = srv.call("favro_get_tag", tag)
	c.check(succeeded(res, err), "the tag is still there", res.text())

	res, err = srv.call("favro_delete_tag", map[string]any{"tag_id": id, "confirm": true})
	c.check(succeeded(res, err), "favro_delete_tag with confirm deletes it", res.text())
	res, err = srv.call("favro_get_tag", tag)
	gone = refused(res, err, "not_found")
	c.check(gone, "the tag is gone", res.text())
	return nil
}

// probeCollection runs a probe collection through the sharing writes:
// public refused on create, a witness that is not its name refused,
// opened to the organization and read back, narrowed again, and deleted
// with confirm.
func probeCollection(srv *session, c *checks) error {
	refusedName := probeName("public")
	res, err := srv.call("favro_create_collection", map[string]any{"name": refusedName, "public_sharing": "public"})
	c.check(refused(res, err, "blocked"), "favro_create_collection with public_sharing public is [blocked]", res.text())
	res, err = srv.call("favro_resolve_collection", map[string]any{"name": refusedName, "force_refresh": true})
	c.check(succeeded(res, err) && strings.Contains(string(res.StructuredContent), `"candidates":[]`),
		"the refused collection was not created", res.text())

	name := probeName("collection")
	res, err = srv.call("favro_create_collection", map[string]any{"name": name})
	id, err := createdID(res, err, "collectionId")
	if err != nil {
		return fmt.Errorf("creating the probe collection: %w", err)
	}
	gone := false
	defer func() {
		if !gone {
			_, _ = srv.call("favro_delete_collection", map[string]any{"collection_id": id, "confirm": true})
		}
	}()
	col := map[string]any{"collection_id": id}
	sharing := func(mode string) bool {
		res, err := srv.call("favro_get_collection", col)
		return succeeded(res, err) && strings.Contains(string(res.StructuredContent), `"publicSharing":"`+mode+`"`)
	}

	res, err = srv.call("favro_widen_collection_sharing", map[string]any{
		"collection_id": id, "collection_name": name + "-not-it", "public_sharing": "organization", "confirm": true,
	})
	c.check(refused(res, err, "conflict"), "favro_widen_collection_sharing with another name is [conflict]", res.text())
	c.check(sharing("users"), "the collection is still shared with users only", "")

	res, err = srv.call("favro_widen_collection_sharing", map[string]any{
		"collection_id": id, "collection_name": name, "public_sharing": "organization", "confirm": true,
	})
	c.check(succeeded(res, err) && strings.Contains(res.text(), `verified by reading the collection back: public_sharing is "organization"`),
		"favro_widen_collection_sharing opens it to the organization and reads it back", res.text())
	c.check(sharing("organization"), "the collection reads back as organization", "")

	res, err = srv.call("favro_update_collection", map[string]any{"collection_id": id, "public_sharing": "users"})
	c.check(succeeded(res, err) && sharing("users"), "favro_update_collection narrows it to users again", res.text())

	res, err = srv.call("favro_delete_collection", col)
	c.check(refused(res, err, "blocked"), "favro_delete_collection without confirm is [blocked]", res.text())
	res, err = srv.call("favro_delete_collection", map[string]any{"collection_id": id, "confirm": true})
	c.check(succeeded(res, err), "favro_delete_collection with confirm deletes it", res.text())
	res, err = srv.call("favro_get_collection", col)
	gone = refused(res, err, "not_found")
	c.check(gone, "the collection is gone", res.text())
	return nil
}
