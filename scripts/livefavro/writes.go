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
// person would be heard; the run fails if it asks anything (D-31). The probe
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
	c.check(len(qs) == 0, livecover.CheckNothingAsked, strings.Join(qs, " | "))

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

// check prints and counts one check, by the name a code comment cites.
func (c *checks) check(ok bool, what livecover.Check, detail string) {
	if ok {
		c.passed++
		c.out.line("ok   %s", what.Name())
		return
	}
	c.failed++
	c.out.line("FAIL %s: %s", what.Name(), firstLine(detail))
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
	c.check(refused(res, err, "blocked"), livecover.CheckTagDeleteWithoutConfirm, detail(res, err))
	res, err = sc.call("favro_get_tag", tag)
	c.check(succeeded(res, err), livecover.CheckTagStillThere, detail(res, err))

	deleteAndReadBack(sc, c, probeKinds["favro_create_tag"], id)
	return nil
}

// deleteAndReadBack deletes a probe with confirm and reads it back by
// id, which must find it gone, and checks each under the kind's names.
//
// Gone is the read refused after a delete that succeeded. The check
// "the collection is gone" holds that the read is refused, and accepts
// either not_found or forbidden as the refusal. Any other answer fails,
// and so does either refusal after a delete that failed.
// https://favro.com/developer/#get-a-collection
func deleteAndReadBack(sc *scratch, c *checks, kind probeKind, id string) {
	res, err := sc.call(kind.delete, kind.deleteCall(id))
	deleted := succeeded(res, err)
	c.check(deleted, kind.deleted, detail(res, err))

	res, err = sc.call(kind.read, map[string]any{kind.idArg: id})
	gone := refused(res, err, "not_found") || refused(res, err, "forbidden")
	c.check(deleted && gone, kind.gone, detail(res, err))
}

// probeCollection runs a probe collection through the sharing writes:
// public refused on create, a witness that is not its name refused,
// opened to the organization and read back, narrowed again, and deleted
// with confirm.
func probeCollection(sc *scratch, c *checks) error {
	refusedName := probeName("public")
	res, err := sc.create("favro_create_collection", map[string]any{"name": refusedName, "public_sharing": "public"})
	wasRefused := refused(res, err, "blocked")
	c.check(wasRefused, livecover.CheckPublicCollectionCreate, detail(res, err))
	if !wasRefused {
		// A create that went through opened a collection to anyone with
		// its link: take it down before anything else.
		if id, err := createdID(res, err, "collectionId"); err == nil {
			_, _ = sc.call("favro_delete_collection", map[string]any{"collection_id": id, "confirm": true})
		}
	}
	res, err = sc.call("favro_resolve_collection", map[string]any{"name": refusedName, "force_refresh": true})
	c.check(succeeded(res, err) && strings.Contains(string(res.StructuredContent), `"candidates":[]`),
		livecover.CheckRefusedCollectionNotCreated, detail(res, err))

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
	c.check(refused(res, err, "conflict"), livecover.CheckWidenWithAnotherName, detail(res, err))
	c.check(sharing("users"), livecover.CheckCollectionStillUsers, "")

	res, err = sc.call("favro_widen_collection_sharing", map[string]any{
		"collection_id": id, "collection_name": name, "public_sharing": "organization", "confirm": true,
	})
	c.check(succeeded(res, err) && strings.Contains(res.text(), `verified by reading the collection back: public_sharing is "organization"`),
		livecover.CheckWidenOpensAndReadsBack, detail(res, err))
	c.check(sharing("organization"), livecover.CheckCollectionReadsOrganization, "")

	res, err = sc.call("favro_update_collection", map[string]any{"collection_id": id, "public_sharing": "users"})
	c.check(succeeded(res, err) && sharing("users"), livecover.CheckUpdateNarrowsToUsers, detail(res, err))

	if err := probeCard(sc, c, id); err != nil {
		return err
	}

	res, err = sc.call("favro_delete_collection", col)
	c.check(refused(res, err, "blocked"), livecover.CheckCollectionDeleteWithoutConfirm, detail(res, err))
	deleteAndReadBack(sc, c, probeKinds["favro_create_collection"], id)
	return nil
}

// probeCard runs a probe card, on a probe widget in the probe collection,
// through the writes a dry run cannot show: an attachment uploaded and
// removed by its fileURL, which Favro answers 200 for whether or not the
// URL matched, and a reply in a comment thread. Both are read back.
// https://favro.com/developer/#update-a-card
// https://favro.com/developer/#create-a-comment
func probeCard(sc *scratch, c *checks, collectionID string) error {
	res, err := sc.create("favro_create_widget", map[string]any{
		"collection_id": collectionID, "name": probeName("widget"), "type": "backlog",
	})
	widgetID, err := createdID(res, err, "widgetCommonId")
	if err != nil {
		return fmt.Errorf("creating the probe widget: %w", err)
	}
	res, err = sc.create("favro_create_card", map[string]any{"name": probeName("card"), "widget_common_id": widgetID})
	cardID, err := createdID(res, err, "cardId")
	if err != nil {
		return fmt.Errorf("creating the probe card: %w", err)
	}
	cardCommonID, err := createdID(res, err, "cardCommonId")
	if err != nil {
		return fmt.Errorf("creating the probe card: %w", err)
	}

	probeAttachment(sc, c, cardID)
	probeThread(sc, c, cardCommonID)

	deleteAndReadBack(sc, c, probeKinds["favro_create_card"], cardID)
	deleteAndReadBack(sc, c, probeKinds["favro_create_widget"], widgetID)
	return nil
}

// probeAttachment uploads a file to the card, removes it by the fileURL
// the card reads back with, and reads the card again.
func probeAttachment(sc *scratch, c *checks, cardID string) {
	card := map[string]any{"card_id": cardID}
	res, err := sc.call("favro_upload_attachment", map[string]any{"card_id": cardID, "file_path": livecover.UploadFile})
	urls := attachmentURLs(sc, card)
	c.check(succeeded(res, err) && len(urls) == 1, livecover.CheckAttachmentUploaded, detail(res, err))
	if len(urls) == 0 {
		return
	}
	res, err = sc.call("favro_remove_attachment", map[string]any{"card_id": cardID, "file_urls": urls, "confirm": true})
	c.check(succeeded(res, err), livecover.CheckAttachmentRemoved, detail(res, err))
	left := attachmentURLs(sc, card)
	c.check(len(left) == 0, livecover.CheckAttachmentGone, fmt.Sprintf("%d attachment(s) left", len(left)))
}

// attachmentURLs reads the card and returns each attachment's fileURL.
func attachmentURLs(sc *scratch, card map[string]any) []any {
	res, err := sc.call("favro_get_card", card)
	if !succeeded(res, err) {
		return nil
	}
	var out struct {
		Attachments []struct {
			FileURL string `json:"fileURL"`
		} `json:"attachments"`
	}
	if json.Unmarshal(res.StructuredContent, &out) != nil {
		return nil
	}
	var urls []any
	for _, a := range out.Attachments {
		urls = append(urls, a.FileURL)
	}
	return urls
}

// probeThread starts a thread on the card, replies in it by thread_id,
// and lists the thread.
func probeThread(sc *scratch, c *checks, cardCommonID string) {
	first, err := sc.call("favro_create_comment", map[string]any{"card_common_id": cardCommonID, "comment": "livefavro probe"})
	thread, err := createdID(first, err, "threadId")
	if err != nil {
		c.check(false, livecover.CheckReplyInThread, "the first comment came back with no thread: "+err.Error())
		return
	}
	sc.adopt(thread)
	res, err := sc.call("favro_create_comment", map[string]any{
		"card_common_id": cardCommonID, "comment": "livefavro probe reply", "thread_id": thread,
	})
	replyThread, replyErr := createdID(res, err, "threadId")
	c.check(replyErr == nil && replyThread == thread, livecover.CheckReplyInThread, detail(res, err))

	res, err = sc.call("favro_list_comments", map[string]any{"card_common_id": cardCommonID, "thread_id": thread})
	var listed struct {
		Items []struct {
			Comment string `json:"comment"`
		} `json:"items"`
	}
	ok := succeeded(res, err) && json.Unmarshal(res.StructuredContent, &listed) == nil &&
		len(listed.Items) == 2 && listed.Items[0].Comment == "livefavro probe"
	c.check(ok, livecover.CheckThreadListed, detail(res, err))
}
