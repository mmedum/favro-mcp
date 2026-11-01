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
	if err := sameSurface(w, committed, current); err != nil {
		return err
	}

	last := lastTag()
	if last == "" {
		_, err := fmt.Fprintln(w, "no previous tag; schemas.json is current")
		return err
	}
	old, err := schemasAtTag(last)
	if errors.Is(err, errNoDumpAtTag) {
		_, err := fmt.Fprintf(w, "schemas.json is current; the build at %s predates --dump-schemas, "+
			"so there is nothing to compare against\n", last)
		return err
	}
	if err != nil {
		return err
	}
	was, err := readToolSurface(old)
	if err != nil {
		return fmt.Errorf("%s: %w", last, err)
	}
	now, err := readToolSurface(current)
	if err != nil {
		return err
	}
	c := compareSurfaces(was, now)
	c.write(w, last)
	if len(c.breaking) > 0 {
		return fmt.Errorf("the tool surface changed in a way that breaks a caller since %s", last)
	}
	return nil
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
func sameSurface(w io.Writer, committed, current []byte) error {
	was, err := readToolSurface(committed)
	if err != nil {
		return fmt.Errorf("schemas.json: %w — run `make schemas` and commit the result", err)
	}
	now, err := readToolSurface(current)
	if err != nil {
		return err
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
// its schemas. The worktree is removed whether or not the build
// succeeds: leaving one behind makes every later `git worktree add` fail
// with a message about the path already existing, which is a confusing
// way to learn that a gate crashed an hour ago.
func buildSchemasAtTag(tag string) (out []byte, err error) {
	tmp, err := os.MkdirTemp("", "gates-schema-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	src := filepath.Join(tmp, "src")
	if err := runCmd("git", "worktree", "add", "-q", "--detach", src, tag); err != nil {
		return nil, fmt.Errorf("worktree for %s: %w", tag, err)
	}
	defer func() {
		if rmErr := runCmd("git", "worktree", "remove", "-f", src); rmErr != nil && err == nil {
			err = fmt.Errorf("remove worktree: %w", rmErr)
		}
	}()

	// A tag from before --dump-schemas existed cannot answer this, and
	// building it to watch the flag be rejected would report a broken
	// gate instead of a missing feature. The condition is the source at
	// that tag rather than the failure, so this stops applying by itself
	// once a tagged release carries the flag.
	supported, err := tagSupportsDump(src)
	if err != nil {
		return nil, err
	}
	if !supported {
		return nil, errNoDumpAtTag
	}

	oldBin := filepath.Join(tmp, "old")
	build := exec.Command("go", "build", "-o", oldBin, "./cmd/favro-mcp")
	build.Dir = src
	if b, buildErr := build.CombinedOutput(); buildErr != nil {
		return nil, fmt.Errorf("build %s: %w: %s", tag, buildErr, b)
	}
	return dumpSchemas(oldBin)
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

// tagSupportsDump reports whether the checked-out source registers the
// flag this gate depends on.
func tagSupportsDump(src string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(src, "cmd", "favro-mcp", "main.go"))
	if err != nil {
		return false, fmt.Errorf("read the tagged main.go: %w", err)
	}
	return strings.Contains(string(data), "dump-schemas"), nil
}

func lastTag() string {
	out, err := exec.Command("git", "describe", "--tags", "--abbrev=0").Output()
	if err != nil {
		return "" // no tags yet, or not a checkout
	}
	return strings.TrimSpace(string(out))
}

func runCmd(name string, args ...string) error {
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, out)
	}
	return nil
}
