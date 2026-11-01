// Command gates runs this repository's own checks.
//
// They are Go rather than shell, and they live here rather than under
// internal/, for the reasons the shared standard gives: a shell script
// is held to no gofmt, vet, lint or test; `make check` runs on the
// Windows runner, where bash is a dependency rather than a given; and a
// script that parses JSON with `sed` is how a quote ends up inside a
// string. This program replaced two such scripts. internal/ is the
// server's own implementation, and a thing that only ever runs on a
// maintainer's machine does not belong there. GoReleaser builds only
// ./cmd/..., so none of this ships.
//
// Every gate here has a test, and every gate asserts a floor on how much
// it read. "Found nothing" and "looked at nothing" print the same
// sentence otherwise, and a gate nobody has watched fail is not yet a
// gate.
//
//	go run ./scripts/gates coverage coverage.out 80
//	go run ./scripts/gates leaks [history]
//	go run ./scripts/gates smoke ./bin/favro-mcp
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// command is one thing this program can do.
type command struct {
	run  func(io.Writer, []string) error
	args string
	doc  string
	// runs says where the command has to run. The parity check reads
	// it, which is the whole reason the registry exists: the two lists
	// it compares are a Makefile and a workflow, with nothing else tying
	// them together.
	runs where
}

// where is the pipeline a command has to run in. Three states, because a
// gate that only a pull request can run is neither a gate `make check`
// runs nor a convenience nothing does.
type where int

const (
	// manual is a maintainer convenience or a release step; nothing
	// holds it to a pipeline.
	manual where = iota
	// inCheck has to run in BOTH `make check` and the CI workflow.
	inCheck
	// inPR has to run in CI on a pull request and NOT in `make check`:
	// it measures from the merge-base, which only a pull request has.
	inPR
)

// commandsIn are the commands that have to run in w, sorted.
func commandsIn(w where) []string {
	var out []string
	for name, c := range commands {
		if c.runs == w {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// commands is the one list of what this program does — the usage text,
// the dispatch and the parity gate all read it, so they cannot drift.
var commands map[string]command

// init populates the registry rather than a var literal, because parity
// reads it and Go will not let a map initializer refer to a function
// that refers back to the map.
func init() {
	commands = map[string]command{
		"coverage": {
			run: coverage, args: "PROFILE MIN", runs: inCheck,
			doc: "enforce the per-package statement floor",
		},
		"leaks": {
			run: leaks, args: "[history]", runs: inCheck,
			doc: "nothing identifying a tenant may be in the repository",
		},
		"pins": {
			run: pins, args: "", runs: inCheck,
			doc: "every action is a SHA and every tool version is exact",
		},
		"api-diff": {
			run: apiDiff, args: "",
			doc: "refetch the Favro API surface snapshot (network; maintainer only)",
		},
		"api-coverage": {
			run: apiCoverage, args: "", runs: inCheck,
			doc: "every documented Favro endpoint has a verdict, and every verdict an endpoint",
		},
		"api-fields": {
			run: apiFields, args: "", runs: inCheck,
			doc: "every documented field is modeled by a wire type, or waived with a reason",
		},
		"transcript": {
			run: transcript, args: "", runs: inCheck,
			doc: "the live driver prints only through the redactor",
		},
		"outcomes": {
			run: outcomes, args: "", runs: inCheck,
			doc: "no tool states an outcome the response did not carry",
		},
		"registry": {
			run: registry, args: "", runs: inCheck,
			doc: "the committed MCP registry entry obeys the rules that registry enforces in code",
		},
		"registry-publish": {
			run: registryPublish, args: "VERSION [DIST]",
			doc: "print the registry entry to publish, with the bundle's hash from the signed checksums",
		},
		"live-cover": {
			run: liveCover, args: "", runs: inCheck,
			doc: "the live driver exercises every tool and option, or waives it",
		},
		"classes": {
			run: classes, args: "", runs: inCheck,
			doc: "the closed error vocabulary and the document that names it",
		},
		"parity": {
			run: parity, args: "", runs: inCheck,
			doc: "`make check` and CI run the same gates",
		},
		"schema-diff": {
			run: schemaDiffCmd, args: "[BIN]", runs: inCheck,
			doc: "diff the tool schemas against the last tag",
		},
		"schema-ack": {
			run: schemaAck, args: "BASE HEAD", runs: inPR,
			doc: "a pull request's commits name each item of the tool surface it changes",
		},
		"smoke": {
			run: smoke, args: "[BIN]", runs: inCheck,
			doc: "drive the binary over stdio, twice",
		},
		"staleness": {
			run: stalenessCmd, args: "[BIN]", runs: inCheck,
			doc: "the documentation must match the code",
		},
		"plugin": {
			run: pluginGate, args: "", runs: inCheck,
			doc: "the committed plugin manifest against the files the packer stages",
		},
		"rule8": {
			run: rule8, args: "[BIN]", runs: inCheck,
			doc: "no tool input takes an organization_id; the server is single-org",
		},
		"mcpb": {
			run: mcpbGate, args: "", runs: inCheck,
			doc: "the committed Claude Desktop manifest against the files the packer stages",
		},
		"mcpb-pack": {
			run: mcpbPack, args: "VERSION [DIST]",
			doc: "assemble the .mcpb from a goreleaser dist (release only)",
		},
		"plugin-pack": {
			run: pluginPack, args: "[DIST]",
			doc: "assemble favro-mcp.plugin from a goreleaser dist (release only)",
		},
		"release-notes": {
			run: releaseNotes, args: "VERSION",
			doc: "print one version's CHANGELOG section (release only)",
		},
		"fmt-check": {
			run: fmtCheck, args: "",
			doc: "every Go file is gofmt'd",
		},
		"precommit": {
			run: precommit, args: "",
			doc: "what the git hook runs: the fast checks over what is about to be committed",
		},
	}
}

func main() {
	if len(os.Args) < 2 {
		fail("%s", usage())
	}
	c, ok := commands[os.Args[1]]
	if !ok {
		fail("unknown command %q\n\n%s", os.Args[1], usage())
	}
	if err := c.run(os.Stdout, os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "gates: %v\n", err)
		os.Exit(exitCode(err))
	}
}

// couldNotRun is an error that stopped schema-ack before it could judge:
// a build or a git call that failed. Its message says so, which is what
// tells it from a refusal in CI, since `go run` exits 1 whatever the
// program's own status was.
type couldNotRun struct{ error }

func (e couldNotRun) Error() string { return "could not run: " + e.error.Error() }

func (e couldNotRun) Unwrap() error { return e.error }

// exitCode is 2 for a gate that could not run, as for a usage error, and
// 1 for a gate that ran and refused. A built binary exits with it.
func exitCode(err error) int {
	if errors.As(err, new(couldNotRun)) {
		return 2
	}
	return 1
}

func usage() string {
	var b strings.Builder
	b.WriteString("usage: gates COMMAND [ARGS]\n\n")
	for _, name := range slices.Sorted(maps.Keys(commands)) {
		c := commands[name]
		mark := map[where]string{manual: " ", inCheck: "*", inPR: "+"}[c.runs]
		fmt.Fprintf(&b, "  %s %-14s %-14s %s\n", mark, name, c.args, c.doc)
	}
	b.WriteString("\n  * runs in both `make check` and CI; + runs in CI on a pull request only; the parity gate checks both.\n")
	return b.String()
}

// defaultBinary is where `make build` puts the server. Every gate that
// drives a binary takes one as an optional argument and falls back here.
func defaultBinary() string {
	return filepath.Join("bin", "favro-mcp"+exeSuffix())
}

// exeSuffix is what the platform appends to an executable's name.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func binOr(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return defaultBinary()
}

// schemaDump is the shape of `favro-mcp --dump-schemas`.
type schemaDump struct {
	Tools []schemaTool `json:"tools"`
}

// schemaTool is the part of a dumped tool the gates read by name. The
// schema diff compares every field, through surface.go, not this.
type schemaTool struct {
	Name        string `json:"name"`
	Annotations struct {
		ReadOnlyHint bool `json:"readOnlyHint"`
	} `json:"annotations"`
	InputSchema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	} `json:"inputSchema"`
}

// takesConfirm reports whether the tool takes confirm, which is what
// makes a tool destructive here: registered only with
// FAVRO_ENABLE_DESTRUCTIVE.
func (t schemaTool) takesConfirm() bool {
	_, ok := t.InputSchema.Properties["confirm"]
	return ok
}

func parseSchemas(b []byte) (*schemaDump, error) {
	var d schemaDump
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	if len(d.Tools) == 0 {
		return nil, fmt.Errorf("no tools in the dump; this is not a schema document")
	}
	return &d, nil
}

// fmtCheck is the gofmt rule, once. It had three spellings — a Makefile
// recipe, a workflow step and a Go function in the hook — which is two
// more than the number of places a rule should live, and `gates parity`
// holds only the Makefile and the workflow to each other.
func fmtCheck(w io.Writer, _ []string) error {
	out, err := exec.Command("gofmt", "-l", ".").Output()
	if err != nil {
		return fmt.Errorf("gofmt: %w", err)
	}
	if files := strings.TrimSpace(string(out)); files != "" {
		return fmt.Errorf("these files are not gofmt'd:\n%s", files)
	}
	_, err = fmt.Fprintln(w, "gofmt ok")
	return err
}

func coverage(w io.Writer, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: gates coverage PROFILE MIN")
	}
	minimum, err := strconv.ParseFloat(args[1], 64)
	if err != nil {
		return fmt.Errorf("coverage: %w", err)
	}
	return coverageFloor(w, args[0], minimum)
}

func stalenessCmd(w io.Writer, args []string) error {
	return staleness(w, binOr(args))
}

func schemaDiffCmd(w io.Writer, args []string) error {
	return schemaDiff(w, binOr(args))
}

// moduleRoot walks up from the working directory to the directory
// holding go.mod. Every gate that reads the repository needs it.
// gitLines runs a git command that writes NUL-separated paths and
// returns them. The splitting idiom is the detail that is easy to get
// subtly wrong — a trailing NUL yields a phantom empty entry, and an
// empty entry passes a floor check — so it lives here rather than in
// each caller. Each caller keeps its own flags and its own floor, which
// is the part that legitimately differs.
func gitLines(root string, args ...string) ([]string, error) {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gates: "+format+"\n", args...)
	os.Exit(2)
}
