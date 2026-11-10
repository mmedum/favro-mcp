package main

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mmedum/favro-mcp/v4/internal/auth"
	"github.com/mmedum/favro-mcp/v4/internal/config"
	"github.com/mmedum/favro-mcp/v4/internal/favroapi"
	"github.com/mmedum/favro-mcp/v4/internal/server"
)

func TestToolsDocumentedReadsBothDirections(t *testing.T) {
	registered := []string{"favro_ping", "favro_list_cards"}
	toolsDoc := "| `favro_ping` | 1 | Liveness. |\n| `favro_list_cards` | 3 | Lists cards. |\n"

	if problems := toolsDocumented(toolsDoc, "", registered); len(problems) > 0 {
		t.Errorf("both tools are documented; got %v", problems)
	}

	// A registered tool nothing documents.
	problems := toolsDocumented("| `favro_ping` | 1 | Liveness. |\n", "", registered)
	if !strings.Contains(strings.Join(problems, "\n"), "favro_list_cards") {
		t.Errorf("an undocumented tool should be named, got %v", problems)
	}

	// Documented in the README's prose rather than the table is fine:
	// the table is the default surface, and a tool behind a flag is
	// described in the paragraph that explains the flag.
	if problems := toolsDocumented("| `favro_ping` | 1 | Liveness. |\n",
		"and `favro_list_cards` when enabled", registered); len(problems) > 0 {
		t.Errorf("prose counts as documentation; got %v", problems)
	}

	// A row naming something that is not a tool.
	problems = toolsDocumented(toolsDoc+"| `favro_teleport` | 9 | Invented. |\n", "", registered)
	if !strings.Contains(strings.Join(problems, "\n"), "favro_teleport") {
		t.Errorf("a table row for an unregistered tool should be reported, got %v", problems)
	}

	// The floor: a file with no rows is a check reading the wrong thing.
	problems = toolsDocumented("", "`favro_ping` `favro_list_cards`", registered)
	if !strings.Contains(strings.Join(problems, "\n"), "no tool table rows") {
		t.Errorf("an empty table should be reported as a broken check, got %v", problems)
	}
}

// A permission rule for a tool the binary does not register asks
// nobody, and a destructive tool without one is allowed by whoever
// copied the example, so the rules are held to the dump both ways. A
// README with none is a check reading nothing.
func TestPermissionRulesNameRegisteredTools(t *testing.T) {
	dump := &schemaDump{Tools: []schemaTool{{Name: "favro_ping"}, tool("favro_delete_tag", nil, "confirm")}}
	for _, tc := range []struct {
		readme string
		want   string
	}{
		{`"ask": ["mcp__favro__favro_delete_tag", "mcp__favro__favro_ping"]`, ""},
		{`"ask": ["mcp__favro__favro_delete_tag"]`, ""},
		{`"ask": ["mcp__favro__favro_delete_tag", "mcp__favro__favro_delete_teg"]`, "permission rule for favro_delete_teg"},
		{`"ask": ["mcp__favro__favro_ping"]`, "no permission rule for favro_delete_tag, which is destructive"},
		{"no rules here", "gives no `permissions.ask` rule"},
	} {
		got := strings.Join(permissionRulesRegistered(tc.readme, dump), "\n")
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.readme, got, tc.want)
		}
	}
}

// The gate against the repository, from its root as main runs it, over
// the surface made in process. A shallow clone, as CI's test job checks
// out, holds no tag for the CHANGELOG rule to measure from; every other
// check still runs, and passes.
func TestStalenessAgainstThisRepository(t *testing.T) {
	t.Chdir(parityRepoRoot)
	var out sink
	err := stalenessOf(&out, stalenessDump(t))
	if shallow, _ := git("rev-parse", "--is-shallow-repository"); errors.As(err, new(couldNotRun)) && strings.TrimSpace(shallow) == "true" {
		t.Skipf("a shallow clone: %v", err)
	}
	if err != nil {
		t.Fatalf("the documentation should match the code: %v", err)
	}
	// Each number the summary carries, because each is a separate thing
	// it could stop reading.
	out.mustSay(t, "tools documented")
	out.mustSay(t, "settings")
	out.mustSay(t, "documents")
}

// The gate reads the docs gate's view of the Markdown at the root and
// directly under docs/: what git would commit, untracked files included
// and ignored ones left out. A document a rule names is required.
func TestStalenessReadsWhatGitWouldCommit(t *testing.T) {
	isolateGit(t)
	dir := t.TempDir()
	mustGit(t, "-C", dir, "init", "-q")
	for _, rel := range []string{readmePath, toolsDocPath, configDocPath, changelogPath, "docs/new.md", "docs/sub/nested.md", "notes.txt"} {
		put(t, dir, rel, "# "+rel+"\r\n")
	}
	put(t, dir, ".gitignore", "scratch.md\n")
	put(t, dir, "scratch.md", "# ignored\n")
	mustGit(t, "-C", dir, "add", readmePath, toolsDocPath)

	documents, err := readDocuments(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{changelogPath, readmePath, configDocPath, toolsDocPath, "docs/new.md"}
	slices.Sort(want)
	if got := slices.Sorted(maps.Keys(documents)); !slices.Equal(got, want) {
		t.Errorf("read %q, want %q", got, want)
	}
	if got := documents[readmePath]; got != "# README.md\n" {
		t.Errorf("README.md read as %q, want its CRLF folded", got)
	}

	if err := os.Remove(filepath.Join(dir, filepath.FromSlash(configDocPath))); err != nil {
		t.Fatal(err)
	}
	if _, err := readDocuments(dir); err == nil || err.Error() != configDocPath+" is missing; staleness reads it" {
		t.Errorf("got %v, want %s reported missing", err, configDocPath)
	}
}

// A number in prose is a claim about a list, and the list is in the
// binary: a delete tool added later leaves the sentence behind.
func TestDestructiveCountMatchesTheBinary(t *testing.T) {
	dump := &schemaDump{Tools: []schemaTool{
		tool("favro_delete_one", nil, "confirm"), tool("favro_delete_two", nil, "confirm"), {Name: "favro_read"},
	}}

	if problems := destructiveCountMatches(map[string]string{
		"a.md": "the two delete-style tools are gated",
	}, dump); len(problems) > 0 {
		t.Errorf("a correct count was reported as wrong: %v", problems)
	}

	for _, prose := range []string{
		"the thirteen delete-style tools are gated",
		"the 13 destructive tools are gated",
	} {
		problems := destructiveCountMatches(map[string]string{"a.md": prose}, dump)
		if len(problems) == 0 {
			t.Errorf("%q was not caught against a binary gating 2", prose)
		}
	}

	// The floors. A dump with no tool taking confirm means the field moved;
	// a document set stating no count means the extractor stopped
	// matching, and both print the same "ok" otherwise.
	if problems := destructiveCountMatches(map[string]string{"a.md": "two delete-style tools"},
		&schemaDump{Tools: []schemaTool{{Name: "favro_read"}}}); len(problems) == 0 {
		t.Error("a dump with no tool taking confirm was not caught")
	}
	if problems := destructiveCountMatches(map[string]string{"a.md": "no claim here"}, dump); len(problems) == 0 {
		t.Error("a document set stating no count was not caught")
	}
}

// docs/configuration.md is the reference a person goes to when they want
// to know what a setting does, so a setting missing from it is missing
// from the place it is looked up — separately from the README's summary.
// Each name counts as a whole word, and the page names no variable the
// server does not read.
func TestEveryEnvNameReachesBothDocuments(t *testing.T) {
	read := func(doc string) string {
		data, err := os.ReadFile(filepath.Join(parityRepoRoot, doc))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	readme, configuration := read("README.md"), read(filepath.Join("docs", "configuration.md"))
	vars := config.Vars()
	if got := envDocumented(vars, readme, configuration); len(got) != 0 {
		t.Fatalf("the committed documents: %q", got)
	}
	if got := envDocumented(nil, readme, configuration); len(got) != 1 {
		t.Errorf("an empty registry gives %q, want the one floor problem", got)
	}
	for _, tc := range []struct {
		name, old, new string
		want           []string
	}{
		{
			"a variable left out", "`FAVRO_LOG_LEVEL`", "`the log level`",
			[]string{"docs/configuration.md does not document FAVRO_LOG_LEVEL"},
		},
		{
			"a removed variable left out", "`FAVRO_INTERACTION_HINT`", "`the hint setting`",
			[]string{"docs/configuration.md does not document FAVRO_INTERACTION_HINT"},
		},
		{"a name kept only inside a longer one", "`FAVRO_MCP_SKIP_VALIDATE`", "`FAVRO_MCP_SKIP_VALIDATE_X`", []string{
			"docs/configuration.md does not document FAVRO_MCP_SKIP_VALIDATE",
			"docs/configuration.md documents FAVRO_MCP_SKIP_VALIDATE_X, which the server does not read",
		}},
		{
			"a variable the server does not read", "`FAVRO_UPLOAD_DIR` |", "`FAVRO_UPLOAD_DIR` and `FAVRO_UPLOAD_DIRS` |",
			[]string{"docs/configuration.md documents FAVRO_UPLOAD_DIRS, which the server does not read"},
		},
		// Whole on both sides, case included: a letter of either case
		// before or after a name makes it another word.
		{
			"a name with a lowercase letter after it", "`FAVRO_LOG_LEVEL`", "`FAVRO_LOG_LEVELs`",
			[]string{"docs/configuration.md does not document FAVRO_LOG_LEVEL"},
		},
		{
			"a name with a lowercase letter before it", "`FAVRO_LOG_LEVEL`", "`xFAVRO_LOG_LEVEL`",
			[]string{"docs/configuration.md does not document FAVRO_LOG_LEVEL"},
		},
		// A name followed by * is a pattern, which documents no variable
		// and names none the server does not read.
		{"a pattern", "`FAVRO_UPLOAD_DIR` |", "`FAVRO_UPLOAD_DIR` and `FAVRO_UPLOAD_*` |", nil},
		{
			"a name given only as a pattern", "`FAVRO_LOG_LEVEL`", "`FAVRO_LOG_LEVEL*`",
			[]string{"docs/configuration.md does not document FAVRO_LOG_LEVEL"},
		},
	} {
		if strings.Count(configuration, tc.old) == 0 {
			t.Fatalf("%s: the page does not say %q", tc.name, tc.old)
		}
		if got := envDocumented(vars, readme, strings.ReplaceAll(configuration, tc.old, tc.new)); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	// The README too, as a whole word.
	if got := envDocumented(vars, strings.ReplaceAll(readme, "FAVRO_UPLOAD_DIR", "FAVRO_UPLOAD_DIRECTORY"),
		configuration); !slices.Equal(got, []string{"README.md does not document FAVRO_UPLOAD_DIR"}) {
		t.Errorf("a README naming only a longer name gives %q", got)
	}
	// A fenced example documents nothing, and a name in one is still a
	// name the page gives.
	const fence = "favro-mcp auth logout "
	if !strings.Contains(configuration, fence) {
		t.Fatalf("the page's fence does not say %q", fence)
	}
	fenced := strings.Replace(strings.ReplaceAll(configuration, "`FAVRO_LOG_LEVEL`", "`the log level`"),
		fence, "FAVRO_LOG_LEVEL=debug FAVRO_UPLOAD_DIRS=/x "+fence, 1)
	if got, want := envDocumented(vars, readme, fenced), []string{
		"docs/configuration.md does not document FAVRO_LOG_LEVEL",
		"docs/configuration.md documents FAVRO_UPLOAD_DIRS, which the server does not read",
	}; !slices.Equal(got, want) {
		t.Errorf("names only in a fenced example: %q, want %q", got, want)
	}
}

// The README's plugin section names every directory the plugin stages a
// binary under, and says which pair runs another's under emulation, from
// the tables the plugin gate reads. A README without the section is
// reported, not passed.
func TestThePluginSectionNamesWhatThePluginStages(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(parityRepoRoot, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := strings.ReplaceAll(string(data), "\r\n", "\n")
	if got := pluginPlatformsDocumented(readme); len(got) != 0 {
		t.Fatalf("the committed README: %q", got)
	}
	for _, tc := range []struct {
		name, old, new string
		want           []string
	}{
		{
			"a staged directory left out", "linux-arm64 and ", "and ",
			[]string{"README.md's plugin section does not name linux-arm64, which the plugin stages"},
		},
		{
			"no word of emulation", "under emulation", "natively",
			[]string{"README.md's plugin section does not say that windows-arm64 runs the windows-amd64 binary under emulation"},
		},
		{
			"no section", "### Cowork plugin", "### Plugin",
			[]string{"README.md has no ### Cowork plugin section, so nothing says what the plugin carries"},
		},
	} {
		if !strings.Contains(readme, tc.old) {
			t.Fatalf("%s: the README does not say %q", tc.name, tc.old)
		}
		if got := pluginPlatformsDocumented(strings.Replace(readme, tc.old, tc.new, 1)); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The changelog fixtures: a released v0.9.0, a v0.9.1 with and without
// entries, and an [Unreleased] section with and without them. This
// repository has no 0.9 tag, so a git call that reads the wrong checkout
// finds neither.
const (
	clReleased   = "## [0.9.0] - 2026-01-01\n\n### Added\n\n- The first tool.\n"
	clNext       = "## [0.9.1] - 2026-02-01\n\n### Added\n\n- A second tool.\n\n"
	clNextEmpty  = "## [0.9.1] - 2026-02-01\n\n"
	clUnreleased = "# Changelog\n\n## [Unreleased]\n\n"
	clWritten    = clUnreleased + "### Added\n\n- A second tool.\n\n"
	clEmpty      = "what users get changed and CHANGELOG.md [Unreleased] has no entry; " +
		"write one under [Unreleased], or under the version heading this release tags"
)

// The CHANGELOG rule: a change to what users get is written up under
// [Unreleased], or, on the release commit before its tag, under the
// version that commit releases, a prerelease's included. A heading in a
// fence is an example and cuts no release.
func TestTheChangelogRule(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		changelog     string
		sourceChanged bool
		// tagged are the versions that have a tag.
		tagged []string
		want   []string
	}{
		{"an undocumented source change", clUnreleased + clReleased, true, []string{"0.9.0"}, []string{clEmpty}},
		{"a documented source change", clWritten + clReleased, true, []string{"0.9.0"}, nil},
		{"the release commit before its tag", clUnreleased + clNext + clReleased, true, []string{"0.9.0"}, nil},
		{
			"a prerelease's commit before its tag", clUnreleased + strings.Replace(clNext, "0.9.1", "1.0.0-rc.1", 1) + clReleased,
			true,
			[]string{"0.9.0"},
			nil,
		},
		{
			"a release commit with nothing under its version", clUnreleased + clNextEmpty + clReleased, true,
			[]string{"0.9.0"},
			[]string{"what users get changed and CHANGELOG.md [0.9.1], the untagged version this commit " +
				"releases, has no entry; write one under [Unreleased], or under the version heading this release tags"},
		},
		{
			"an undocumented source change after the release is tagged", clUnreleased + clNext + clReleased, true,
			[]string{"0.9.0", "0.9.1"},
			[]string{clEmpty},
		},
		{
			"prose and an indented bullet, which are no entry", clUnreleased + "Some prose.\n  - an aside\n\n" + clReleased, true,
			[]string{"0.9.0"},
			[]string{clEmpty},
		},
		{"an [Unreleased] section followed only by the link footer", clUnreleased +
			"[Unreleased]: https://example.com/compare/v0.9.0...HEAD\n- not an entry\n", true, nil, []string{clEmpty}},
		{
			"a release cut only in a fence", clUnreleased + "```\n" + clNext + "```\n\n" + clReleased, true,
			[]string{"0.9.0"},
			[]string{clEmpty},
		},
		{"no [Unreleased] section", "# Changelog\n\n" + clReleased, true, []string{"0.9.0"}, []string{clEmpty}},
		{"no change to what users get", clUnreleased + clReleased, false, []string{"0.9.0"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := changelogRule(tc.changelog, tc.sourceChanged, func(v string) bool { return slices.Contains(tc.tagged, v) })
			if !slices.Equal(got, tc.want) {
				t.Errorf("changelogRule() = %q, want %q", got, tc.want)
			}
		})
	}
}

// stalenessRepo is a git checkout of a module whose binary, cmd/fixture,
// imports internal/a, which embeds a file, and not internal/unused, with a
// release tag v0.9.0 on its one commit. A case changes a stalenessCopy of
// it.
func stalenessRepo(t *testing.T) string {
	t.Helper()
	isolateGit(t)
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	putAll(t, dir, map[string]string{
		changelogPath: clUnreleased + clReleased,
		"go.mod": textLines("module example.com/fixture", "", "go 1.21", "",
			"require example.com/dep v1.0.0", "", "replace example.com/dep => ./dep"),
		"dep/go.mod": textLines("module example.com/dep", "", "go 1.21"),
		"dep/dep.go": textLines("package dep"),
		"cmd/fixture/main.go": textLines("package main", "", `import (`, `	_ "example.com/dep"`,
			`	_ "example.com/fixture/internal/a"`, ")", "", "func main() {}"),
		"tools.go":              textLines("package fixture"),
		"internal/a/a.go":       textLines("package a", "", `import _ "embed"`, "", "//go:embed data/x.txt", "var X string"),
		"internal/a/b.go":       textLines("package a"),
		"internal/a/data/x.txt": "x\n",
		"internal/a/notes.md":   "notes\n",
		"internal/unused/u.go":  textLines("package unused"),
		"internal/a/a_test.go":  textLines("package a"),
		"internal/a/testdata/x": "x\n",
		"scripts/gates/main.go": textLines("package main", "", "func main() {}"),
		"docs/x.md":             "x\n",
	})
	stalenessGit(t, dir, "init", "-q", "-b", "main")
	stalenessGit(t, dir, "add", "-A")
	stalenessGit(t, dir, "commit", "-q", "-m", "first")
	stalenessGit(t, dir, "tag", "v0.9.0")
	return dir
}

// stalenessCopy is a copy of the checkout at dir, its .git included, for
// one case to change on its own.
func stalenessCopy(t *testing.T, dir string) string {
	t.Helper()
	cp := t.TempDir()
	if err := os.CopyFS(cp, os.DirFS(dir)); err != nil {
		t.Fatal(err)
	}
	return cp
}

// stalenessGit runs git in dir, isolated as the test's isolateGit left it.
func stalenessGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	mustGit(t, append([]string{"-C", dir}, args...)...)
}

// stalenessWrite writes the files with their new contents, a file with no
// contents removed.
func stalenessWrite(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		if body == "" {
			stalenessGit(t, dir, "rm", "-q", name)
		} else {
			put(t, dir, name, body)
		}
	}
}

// stalenessChange writes the files as stalenessWrite does and commits them.
func stalenessChange(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	stalenessWrite(t, dir, files)
	stalenessGit(t, dir, "add", "-A")
	stalenessGit(t, dir, "commit", "-q", "-m", "change")
}

// stalenessChangelog runs the CHANGELOG rule over the checkout at dir, its
// CHANGELOG as committed.
func stalenessChangelog(t *testing.T, dir string) ([]string, error) {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(dir, changelogPath))
	if err != nil {
		t.Fatal(err)
	}
	return checkChangelog(dir, string(text))
}

// A committed change to what users get needs an entry: a package the
// binary imports, a file one embeds, a bundle's manifest, and go.mod but
// its require and tool lines. A test, a fixture, a package it does not
// import and a dependency bump do not, and nor does what is not
// committed, which CI never sees.
func TestTheChangelogRuleCountsWhatShips(t *testing.T) { //nolint:tparallel // its fixture sets the git isolation env, which a parallel test may not
	const goMod = "go.mod"
	base := stalenessRepo(t)
	raw, err := os.ReadFile(filepath.Join(base, goMod))
	if err != nil {
		t.Fatal(err)
	}
	gomod := string(raw)
	bump := strings.Replace(gomod, "require example.com/dep v1.0.0", "require (\n\texample.com/dep v1.1.0 // a bump\n)", 1)
	for _, tc := range []struct {
		name string
		// files are committed, and after is written after the commit.
		files, after map[string]string
		ships        bool
	}{
		{"a package the binary imports", map[string]string{"internal/a/b.go": "package a\n\n// changed\n"}, nil, true},
		{"a file added to one", map[string]string{"internal/a/c.go": "package a\n"}, nil, true},
		{"a file removed from one", map[string]string{"internal/a/b.go": ""}, nil, true},
		{"a file moved out of one", map[string]string{"internal/a/b.go": "", "docs/b.go": "package a\n"}, nil, true},
		{"the command", map[string]string{"cmd/fixture/main.go": "package main\n\nfunc main() { println() }\n"}, nil, true},
		{"a file a package embeds", map[string]string{"internal/a/data/x.txt": "y\n"}, nil, true},
		{"go.mod's go line", map[string]string{goMod: strings.Replace(gomod, "go 1.21", "go 1.22", 1)}, nil, true},
		{"go.mod's toolchain", map[string]string{goMod: gomod + "\ntoolchain go1.21.0\n"}, nil, true},
		{"go.mod's replace", map[string]string{goMod: strings.Replace(gomod, "dep =>", "dep v1.0.0 =>", 1)}, nil, true},
		{"a dependency bump", map[string]string{goMod: bump}, nil, false},
		// The go.mod grammar reads ( as a token of its own (https://go.dev/ref/mod#go-mod-file-lexical).
		{"a dependency bump in a require( block", map[string]string{goMod: strings.Replace(gomod,
			"require example.com/dep v1.0.0", "require(\n\texample.com/dep v1.1.0\n)", 1)}, nil, false},
		// A tool directive names a tool `go tool` runs, which the binary does not
		// build (https://go.dev/ref/mod#go-mod-file-tool).
		{"a tool directive", map[string]string{goMod: gomod + "\ntool example.com/dep\n"}, nil, false},
		{"a tool( block", map[string]string{goMod: gomod + "\ntool(\n\texample.com/dep\n)\n"}, nil, false},
		{"a test", map[string]string{"internal/a/a_test.go": "package a\n\n// changed\n"}, nil, false},
		{"a fixture", map[string]string{"internal/a/testdata/x": "y\n"}, nil, false},
		// The go command builds a package from assembly, a .syso and C as
		// well as Go, so any file of a package directory but a test counts.
		{"a package's assembly", map[string]string{"internal/a/b_amd64.s": "TEXT ·f(SB),$0\n"}, nil, true},
		{"a package's .syso", map[string]string{"internal/a/res.syso": "x\n"}, nil, true},
		{"a file beside a package that it does not embed", map[string]string{"internal/a/notes.md": "more\n"}, nil, true},
		{"a file under a package that is no package", map[string]string{"internal/a/notes/x.md": "more\n"}, nil, false},
		{"a fixture's assembly", map[string]string{"internal/a/testdata/b_amd64.s": "x\n"}, nil, false},
		// A client reads a bundle's manifest to run the server.
		{"the .mcpb's manifest", map[string]string{mcpbManifestPath: "{}\n"}, nil, true},
		{"the .plugin's manifest", map[string]string{manifestPath: "{}\n"}, nil, true},
		{"the .plugin's server config", map[string]string{mcpPath: "{}\n"}, nil, true},
		{"the registry entry, which the release writes", map[string]string{"packaging/registry/server.json": "{}\n"}, nil, false},
		{"a package the binary does not import", map[string]string{"internal/unused/u.go": "package unused\n\n// changed\n"}, nil, false},
		// A dependency's package sits at a path of its own module's, never this one's.
		{"a package at the path of a dependency's", map[string]string{"tools.go": "package fixture\n\n// changed\n"}, nil, false},
		{"a script", map[string]string{"scripts/gates/main.go": "package main\n\nfunc main() { println() }\n"}, nil, false},
		{"a doc", map[string]string{"docs/x.md": "y\n"}, nil, false},
		{"a change not committed", nil, map[string]string{"internal/a/b.go": "package a\n\n// changed\n"}, false},
		{
			"go.mod as committed, its go line changed after",
			map[string]string{goMod: bump},
			map[string]string{goMod: strings.Replace(bump, "go 1.21", "go 1.22", 1)},
			false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := stalenessCopy(t, base)
			if tc.files != nil {
				stalenessChange(t, dir, tc.files)
			}
			stalenessWrite(t, dir, tc.after)
			got, err := stalenessChangelog(t, dir)
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			if tc.ships {
				want = []string{clEmpty}
			}
			if !slices.Equal(got, want) {
				t.Errorf("checkChangelog() = %q, want %q", got, want)
			}
		})
	}
}

// The base is the newest release tag by semver, a prerelease included: a
// change two commits after it still counts, and a release tagged after
// the change takes it out of the next release's notes.
func TestTheChangelogRuleMeasuresFromTheNewestRelease(t *testing.T) { //nolint:tparallel // its fixture sets the git isolation env, which a parallel test may not
	base := stalenessRepo(t)
	for _, tc := range []struct {
		name string
		// then runs in the checkout after the shipped change is committed.
		then func(t *testing.T, dir string)
		want []string
	}{
		{"a change two commits after the tag", func(t *testing.T, dir string) {
			stalenessChange(t, dir, map[string]string{"docs/x.md": "later\n"})
		}, []string{clEmpty}},
		{"a change a release then tags", func(t *testing.T, dir string) { stalenessGit(t, dir, "tag", "v0.9.1") }, nil},
		{"a change a prerelease then tags", func(t *testing.T, dir string) { stalenessGit(t, dir, "tag", "v1.0.0-rc.1") }, nil},
		{"a change older than the newest tag by date, not by version", func(t *testing.T, dir string) {
			stalenessGit(t, dir, "tag", "v0.8.9")
		}, []string{clEmpty}},
		// A tag that names no release is no base: git describe would take it.
		{"a change a tag that is not a release then tags", func(t *testing.T, dir string) {
			stalenessGit(t, dir, "tag", "-a", "nightly", "-m", "nightly")
		}, []string{clEmpty}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := stalenessCopy(t, base)
			stalenessChange(t, dir, map[string]string{"internal/a/b.go": "package a\n\n// changed\n"})
			tc.then(t, dir)
			got, err := stalenessChangelog(t, dir)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("checkChangelog() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A maintenance branch measures from its own release, not from a newer
// one on another line of history: what only the newer release changed is
// no change of the branch's, and a fix the branch picks up from it is.
func TestTheChangelogRuleMeasuresFromTheReleaseHEADReaches(t *testing.T) { //nolint:tparallel // its fixture sets the git isolation env, which a parallel test may not
	const fix = "package a\n\n// fixed in 1.0.0\n"
	base := stalenessRepo(t)
	stalenessChange(t, base, map[string]string{"internal/a/b.go": fix})
	stalenessGit(t, base, "tag", "v1.0.0")
	for _, tc := range []struct {
		name string
		// change is what the maintenance branch commits after v0.9.0.
		change map[string]string
		want   []string
	}{
		{"a maintenance branch that changes only the docs", map[string]string{"docs/x.md": "later\n"}, nil},
		// With the docs beside it, the commit is not v1.0.0's own.
		{
			"a maintenance branch that picks up the newer release's fix",
			map[string]string{"internal/a/b.go": fix, "docs/x.md": "back-ported\n"},
			[]string{clEmpty},
		},
		// A version a tag names on any line is released, so its notes are no
		// release commit's.
		{"the newer release's fix and notes, squashed onto the branch", map[string]string{
			"internal/a/b.go": fix,
			changelogPath:     clUnreleased + strings.Replace(clNext, "0.9.1", "1.0.0", 1) + clReleased,
		}, []string{clEmpty}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := stalenessCopy(t, base)
			stalenessGit(t, dir, "switch", "-q", "-c", "release-0.9", "v0.9.0")
			stalenessChange(t, dir, tc.change)
			got, err := stalenessChangelog(t, dir)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("checkChangelog() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A listing that names no command is not reading the binary.
func TestTheChangelogRuleNeedsTheBinary(t *testing.T) {
	dir := stalenessRepo(t)
	stalenessChange(t, dir, map[string]string{"cmd/fixture/main.go": "", "cmd/README.md": "moved\n"})
	_, err := stalenessChangelog(t, dir)
	want := "could not run: the CHANGELOG rule: go list -deps ./cmd/... named no package of this module, so it is not reading the binary"
	if !errors.As(err, new(couldNotRun)) || err == nil || err.Error() != want {
		t.Errorf("checkChangelog() = %v, want %q", err, want)
	}
}

// Outside a git checkout the CHANGELOG rule has no tag to measure from.
// It reached no verdict, so it says so and exits 2, as the docs gate does,
// and a git that cannot answer is never "what users get changed".
func TestTheChangelogRuleNeedsAGitCheckout(t *testing.T) {
	isolateGit(t)
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	_, err := checkChangelog(dir, clUnreleased+clReleased)
	if !errors.As(err, new(couldNotRun)) || exitCode(err) != 2 ||
		!strings.HasPrefix(err.Error(), "could not run: the CHANGELOG rule: git -C "+dir+" tag") {
		t.Errorf("outside git: %v; want a git tag failure that could not run", err)
	}
	// A configuration git diff cannot read, and git tag does not.
	dir = stalenessRepo(t)
	stalenessGit(t, dir, "config", "diff.renames", "bogus")
	_, err = stalenessChangelog(t, dir)
	if !errors.As(err, new(couldNotRun)) || err == nil ||
		!strings.HasPrefix(err.Error(), "could not run: the CHANGELOG rule: git -C "+dir+" diff v0.9.0..HEAD") {
		t.Errorf("a diff git cannot make: %v; want a git diff failure that could not run", err)
	}
}

// With no release tag there is nothing to measure from, so the rule says
// why rather than measure from some other commit. A shallow clone fetches
// no tag, and the rule names it.
func TestTheChangelogRuleNeedsARelease(t *testing.T) {
	const noTag = "could not run: the CHANGELOG rule: HEAD reaches no release tag (vX.Y.Z) to measure from"
	base := stalenessRepo(t)
	stalenessChange(t, base, map[string]string{"internal/a/b.go": "package a\n\n// changed\n"})
	untagged := stalenessCopy(t, base)
	stalenessGit(t, untagged, "tag", "-d", "v0.9.0")
	shallow := t.TempDir()
	from := filepath.ToSlash(base)
	if !strings.HasPrefix(from, "/") {
		from = "/" + from
	}
	stalenessGit(t, shallow, "clone", "-q", "--depth=1", "--no-tags", "file://"+from, ".")
	for dir, want := range map[string]string{
		untagged: noTag,
		shallow:  noTag + "; the checkout is a shallow clone: check it out with fetch-depth: 0",
	} {
		_, err := stalenessChangelog(t, dir)
		if !errors.As(err, new(couldNotRun)) || err == nil || err.Error() != want {
			t.Errorf("checkChangelog() = %v, want %q", err, want)
		}
	}
}

// The CHANGELOG rule not running holds back no other check: the gate
// still holds the README, docs/TOOLS.md and the configuration page, a real
// finding still fails it, and with none it reports only that the rule
// could not run. A fence holds an example, which documents nothing.
func TestStalenessRunsEveryOtherCheckWithoutARelease(t *testing.T) {
	isolateGit(t)
	dump := stalenessDump(t)
	files := map[string]string{}
	for _, name := range []string{readmePath, toolsDocPath, configDocPath, changelogPath} {
		files[name] = strings.ReplaceAll(string(repoFile(t, name)), "\r\n", "\n")
	}
	const unrun = "could not run: the CHANGELOG rule: HEAD reaches no release tag (vX.Y.Z) to measure from"
	const ping = "`favro_ping`"
	for _, tc := range []struct {
		name string
		edit func(t *testing.T, files map[string]string)
		err  string
		code int
	}{
		{"nothing else wrong", func(*testing.T, map[string]string) {}, unrun, 2},
		{"a table row for a tool the binary does not register", func(t *testing.T, files map[string]string) {
			row := "| " + ping + " |"
			if !strings.Contains(files[toolsDocPath], row) {
				t.Fatalf("%s has no row starting %q", toolsDocPath, row)
			}
			files[toolsDocPath] = strings.Replace(files[toolsDocPath], row, "| `favro_ghost` | x |\n"+row, 1)
		}, "docs/TOOLS.md names a tool that is not registered: favro_ghost\n" + unrun, 1},
		{"a tool and a setting named only in a fenced example", func(t *testing.T, files map[string]string) {
			for _, f := range []string{readmePath, toolsDocPath} {
				if !strings.Contains(files[f], ping) {
					t.Fatalf("%s does not say %s", f, ping)
				}
				files[f] = strings.ReplaceAll(files[f], ping, "that one")
			}
			if !strings.Contains(files[readmePath], "FAVRO_LOG_LEVEL") {
				t.Fatalf("%s does not say FAVRO_LOG_LEVEL", readmePath)
			}
			files[readmePath] = strings.ReplaceAll(files[readmePath], "FAVRO_LOG_LEVEL", "the log level") +
				"\n```text\n" + ping + " FAVRO_LOG_LEVEL\n```\n"
		}, "docs/TOOLS.md and README.md document neither of: favro_ping\n" +
			"README.md does not document FAVRO_LOG_LEVEL\n" + unrun, 1},
		{"a table row in a fenced example", func(_ *testing.T, files map[string]string) {
			files[toolsDocPath] += "\n```text\n| `favro_ghost` | x |\n```\n"
		}, unrun, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edited := maps.Clone(files)
			tc.edit(t, edited)
			dir := t.TempDir()
			putAll(t, dir, edited)
			stalenessGit(t, dir, "init", "-q", "-b", "main")
			stalenessGit(t, dir, "add", "-A")
			stalenessGit(t, dir, "commit", "-q", "-m", "first")
			t.Chdir(dir)
			err := stalenessOf(&sink{}, dump)
			if err == nil || err.Error() != tc.err || exitCode(err) != tc.code {
				t.Errorf("staleness() = %v (exit %d); want %q (exit %d)", err, exitCode(err), tc.err, tc.code)
			}
		})
	}
}

// stalenessDumpBytes is the surface the binary dumps, made once in process
// with the options its --dump-schemas passes: every tool, the destructive
// and upload ones included.
var stalenessDumpBytes = sync.OnceValues(func() ([]byte, error) {
	var out bytes.Buffer
	srv := server.New(favroapi.NewClient(auth.Token{}), server.Options{
		Destructive: true, UploadDir: os.TempDir(), CredentialSource: "none", Version: "test",
	})
	err := server.DumpSchemas(context.Background(), srv, &out, "test")
	return out.Bytes(), err
})

// stalenessDump is stalenessDumpBytes parsed, a dump of its own for each
// call, which a test may change.
func stalenessDump(t *testing.T) *schemaDump {
	t.Helper()
	raw, err := stalenessDumpBytes()
	if err != nil {
		t.Fatal(err)
	}
	dump, err := parseSchemas(raw)
	if err != nil {
		t.Fatal(err)
	}
	return dump
}
