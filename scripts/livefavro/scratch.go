package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// caller sends one tools/call. A *session is one; the tests use a fake.
type caller interface {
	call(tool string, args map[string]any) (*callResult, error)
}

// probeKind is how the run finds a probe it created, by the name it
// gave it, and deletes it.
type probeKind struct {
	noun    string
	idKey   string // the id's key in the create's result
	resolve string // the read that finds it by name
	found   string // the id's key in that read's candidates
	delete  string
	idArg   string // the delete's id argument
}

// probeKinds are the only creates the -writes run may send. Each says
// how to find what it made, so the run creates nothing it cannot clean
// up after.
var probeKinds = map[string]probeKind{
	"favro_create_tag": {
		noun: "tag", idKey: "tagId",
		resolve: "favro_resolve_tag", found: "tag_id",
		delete: "favro_delete_tag", idArg: "tag_id",
	},
	"favro_create_collection": {
		noun: "collection", idKey: "collectionId",
		resolve: "favro_resolve_collection", found: "collection_id",
		delete: "favro_delete_collection", idArg: "collection_id",
	},
}

// probe is one create this run sent: the tool, and the name it gave.
type probe struct{ tool, name string }

// scratch is the only way the -writes run reaches the server, and it
// sends a write only to an object this run created.
//
// A read goes through as it is. Any other call must name at least one
// object, and every argument ending in _id or _ids must name one that a
// create in this run returned. A create goes through create, which
// records the probe's name before the call is sent, so a probe whose
// answer was lost is still found and deleted. A tool is read-only by
// its own readOnlyHint; a tool tools/list does not mark is a write.
type scratch struct {
	srv   caller
	tools map[string]tool

	mu     sync.Mutex
	made   map[string]bool
	ledger []probe
}

func newScratch(srv caller, tools map[string]tool) *scratch {
	return &scratch{srv: srv, tools: tools, made: map[string]bool{}}
}

// call sends a read, or a write to objects this run created.
func (s *scratch) call(name string, args map[string]any) (*callResult, error) {
	if !s.tools[name].readOnly {
		named, err := s.ours(name, args)
		if err != nil {
			return nil, err
		}
		if named == 0 {
			return nil, fmt.Errorf("%s names no object, so it is not sent; a create goes through create", name)
		}
	}
	return s.srv.call(name, args)
}

// create sends one of probeKinds, after recording the name it gives.
func (s *scratch) create(name string, args map[string]any) (*callResult, error) {
	kind, ok := probeKinds[name]
	if !ok {
		return nil, fmt.Errorf("%s is not a create the run can clean up after, so it is not sent", name)
	}
	probeName, _ := args["name"].(string)
	if !strings.HasPrefix(probeName, probePrefix) {
		return nil, fmt.Errorf("%s: a probe's name starts %q, so it is not sent", name, probePrefix)
	}
	if _, err := s.ours(name, args); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.ledger = append(s.ledger, probe{tool: name, name: probeName})
	s.mu.Unlock()

	res, err := s.srv.call(name, args)
	if id, idErr := createdID(res, err, kind.idKey); idErr == nil {
		s.mu.Lock()
		s.made[id] = true
		s.mu.Unlock()
	}
	return res, err
}

// ours counts the objects a call names, and refuses one this run did not
// create.
func (s *scratch) ours(tool string, args map[string]any) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	named := 0
	for key, v := range args {
		var ids []any
		switch {
		case strings.HasSuffix(key, "_id"):
			ids = []any{v}
		case strings.HasSuffix(key, "_ids"):
			ids, _ = v.([]any)
		default:
			continue
		}
		if len(ids) == 0 {
			return 0, fmt.Errorf("%s: %s names nothing this run created, so it is not sent", tool, key)
		}
		for _, id := range ids {
			if str, _ := id.(string); !s.made[str] {
				return 0, fmt.Errorf("%s: %s names an object this run did not create, so it is not sent", tool, key)
			}
			named++
		}
	}
	return named, nil
}

// probes is every create this run sent.
func (s *scratch) probes() []probe {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]probe(nil), s.ledger...)
}

// sweepWith runs sweep on a server of its own.
func sweepWith(bin string, env []string, probes []probe, out *printer) error {
	if len(probes) == 0 {
		return nil
	}
	srv, err := start(bin, nil, nil, env...)
	if err != nil {
		out.line("cleanup could not start a server, so %d probes may still be there: %s", len(probes), names(probes))
		return fmt.Errorf("cleanup: %w", err)
	}
	defer srv.stop()
	return sweep(srv, probes, out)
}

// sweep looks for every probe by its name, deletes each one it finds,
// and reads that it is gone. A probe it cannot delete is printed loudly
// with its name, for a person to delete by hand.
func sweep(c caller, probes []probe, out *printer) error {
	var left []string
	for _, p := range probes {
		deleted, err := sweepOne(c, p)
		kind := probeKinds[p.tool]
		switch {
		case err != nil:
			left = append(left, p.name)
			out.line("A PROBE %s IS STILL THERE: %s (%v); delete it by hand", strings.ToUpper(kind.noun), p.name, err)
		case deleted > 0:
			out.line("cleanup: deleted the %s %s, which the run had left behind", kind.noun, p.name)
		}
	}
	if len(left) > 0 {
		return fmt.Errorf("%d probes are still there: %s", len(left), strings.Join(left, ", "))
	}
	out.line("cleanup: looked for %d probes by name; none is left", len(probes))
	return nil
}

// sweepOne deletes every object named exactly p.name and reads that
// none is left.
func sweepOne(c caller, p probe) (deleted int, err error) {
	kind := probeKinds[p.tool]
	ids, err := findByName(c, kind, p.name)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		res, err := c.call(kind.delete, map[string]any{kind.idArg: id, "confirm": true})
		if !succeeded(res, err) {
			return deleted, fmt.Errorf("the delete failed: %s", firstLine(detail(res, err)))
		}
		deleted++
	}
	ids, err = findByName(c, kind, p.name)
	if err != nil {
		return deleted, err
	}
	if len(ids) > 0 {
		return deleted, errors.New("it reads back after the delete")
	}
	return deleted, nil
}

// findByName returns the id of every candidate named exactly name.
func findByName(c caller, kind probeKind, name string) ([]string, error) {
	res, err := c.call(kind.resolve, map[string]any{"name": name, "force_refresh": true})
	if !succeeded(res, err) {
		return nil, fmt.Errorf("looking for it failed: %s", firstLine(detail(res, err)))
	}
	var out struct {
		Candidates []map[string]any `json:"candidates"`
	}
	if err := json.Unmarshal(res.StructuredContent, &out); err != nil {
		return nil, fmt.Errorf("looking for it: %w", err)
	}
	var ids []string
	for _, cand := range out.Candidates {
		if cand["name"] != name {
			continue
		}
		if id, _ := cand[kind.found].(string); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func names(probes []probe) string {
	out := make([]string, len(probes))
	for i, p := range probes {
		out[i] = p.name
	}
	return strings.Join(out, ", ")
}
