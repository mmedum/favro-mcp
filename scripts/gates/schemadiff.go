package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// schemaDiff holds the tool surface two ways: the committed schemas.json
// must be what this build dumps, and the change since the last tag must
// not break a caller — a removed tool, a removed input, or an input that
// became required (CONTRIBUTING, Versioning). Every other change is
// printed for a reviewer to read.
//
// **It verifies rather than writes.** The first version regenerated
// schemas.json on every run, which made the claim in the changelog —
// that the committed dump is where a reviewer sees a wire change — true
// only for people who ran `make check` before pushing. Everyone else got
// a green build and a pull request whose diff showed nothing. Writing
// also left the working tree dirty in the middle of `make check`, which
// is its own small betrayal. `make schemas` is the writer; this is the
// check, and it names that target when it fails.
func schemaDiff(w io.Writer, bin string) error {
	current, err := dumpSchemas(bin)
	if err != nil {
		return err
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	committed, err := os.ReadFile(filepath.Join(root, "schemas.json"))
	if err != nil {
		return fmt.Errorf("%w — run `make schemas` and commit the result", err)
	}
	now, err := readToolSurface(current)
	if err != nil {
		return err
	}
	if err := sameSurface(w, committed, now); err != nil {
		return err
	}

	was, last, err := releaseSurface("HEAD")
	switch {
	case errors.Is(err, errNoDumpAtTag):
		_, err := fmt.Fprintf(w, "schemas.json is current; the build at %s predates --dump-schemas, "+
			"so there is nothing to compare against\n", last)
		return err
	case err != nil:
		return err
	case last == "":
		_, err := fmt.Fprintln(w, "no previous tag; schemas.json is current")
		return err
	}
	c := compareSurfaces(was, now)
	c.write(w, last)
	if len(c.breaking) > 0 {
		return fmt.Errorf("the tool surface changed in a way that breaks a caller since %s", last)
	}
	return nil
}

// releaseSurface is the surface of the newest tag rev reaches, read from
// that tag's schemas.json, and the tag: "" when the repository has no
// tag yet. A tag older than --dump-schemas gives errNoDumpAtTag beside
// its name.
func releaseSurface(rev string) (toolSurface, string, error) {
	tag, err := lastTagOn(rev)
	if err != nil || tag == "" {
		return toolSurface{}, "", err
	}
	data, err := schemasAtTag(tag)
	if err != nil {
		return toolSurface{}, tag, err
	}
	s, err := readToolSurface(data)
	if err != nil {
		return toolSurface{}, tag, fmt.Errorf("schemas.json at %s: %w", tag, err)
	}
	return s, tag, nil
}

// lastTagOn is the newest tag rev reaches, or "" when the repository has
// no tag at all. Any other failure is an error: a tag that could not be
// read is not a release that does not exist.
func lastTagOn(rev string) (string, error) {
	tags, err := git("tag", "--list")
	if err != nil || tags == "" {
		return "", err
	}
	return git("describe", "--tags", "--abbrev=0", rev)
}

// sameSurface fails unless the committed schemas.json is the surface this
// build dumps, and prints what differs. Everything but the build stamp
// is compared: an annotation, a title, an output schema or a _meta mark
// that differs is a wire change the pull request has to show.
//
// Not the bytes: the stamp differs between a maintainer's `make schemas`
// and CI's build, and comparing it would fail this gate on every commit
// for a reason that has nothing to do with the tool surface — which is
// the shape of a check people learn to re-run until it passes.
func sameSurface(w io.Writer, committed []byte, now toolSurface) error {
	was, err := readToolSurface(committed)
	if err != nil {
		return fmt.Errorf("schemas.json: %w — run `make schemas` and commit the result", err)
	}
	if c := compareSurfaces(was, now); !c.empty() {
		c.write(w, "the committed schemas.json")
		return fmt.Errorf("schemas.json is not the surface this build dumps; run `make schemas` and " +
			"commit the result, so the wire change shows up in the pull request rather than only here")
	}
	return nil
}

// schemasAtTag returns the tool surface as it was at a tag.
//
// The cheap path is the committed schemas.json at that tag: this gate
// keeps that file current, so from the first tag that carries one there
// is nothing to build. The expensive path — a worktree and a full build
// of the old module graph — is the fallback for tags that predate it,
// and it produces the same bytes for the same tag every time it runs.
func schemasAtTag(tag string) ([]byte, error) {
	if out, err := exec.Command("git", "show", tag+":schemas.json").Output(); err == nil {
		if _, parseErr := parseSchemas(out); parseErr == nil {
			return out, nil
		}
	}
	return buildSchemasAtTag(tag)
}

// buildSchemasAtTag builds the binary as it was at a tag and asks it for
// its schemas.
//
// A tag from before --dump-schemas existed cannot answer this, and
// building it to watch the flag be rejected would report a broken gate
// instead of a missing feature. The condition is the source at that tag
// rather than the failure, so this stops applying by itself once a
// tagged release carries the flag.
func buildSchemasAtTag(tag string) ([]byte, error) {
	main, err := git("show", tag+":cmd/favro-mcp/main.go")
	if err != nil {
		return nil, fmt.Errorf("read the tagged main.go: %w", err)
	}
	if !strings.Contains(main, "dump-schemas") {
		return nil, errNoDumpAtTag
	}
	return dumpAt(tag)
}

// dumpSchemas asks a build for its schemas. The binary decides what that
// means: --dump-schemas emits the whole surface, including any tool that
// registers only behind a flag. That belongs in the binary rather than
// here — a gate setting an environment variable fixes only the gate,
// while every other reader of --dump-schemas keeps the partial answer.
func dumpSchemas(bin string) ([]byte, error) {
	abs, err := filepath.Abs(bin)
	if err != nil {
		return nil, err
	}
	out, err := exec.Command(abs, "--dump-schemas").Output()
	if err != nil {
		return nil, fmt.Errorf("%s --dump-schemas: %w", bin, err)
	}
	return out, nil
}

// errNoDumpAtTag says the comparison could not be made rather than that
// it failed.
var errNoDumpAtTag = errors.New("the build at that tag has no --dump-schemas")

func runCmd(name string, args ...string) error {
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, out)
	}
	return nil
}
