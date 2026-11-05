package main

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/v4/internal/config"
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

// The gate against the repository.
func TestStalenessAgainstThisRepository(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	bin := root + "/bin/favro-mcp"
	if !built(bin) {
		t.Skip("no binary; `make build` first (CI builds before it runs this gate)")
	}
	var out sink
	if err := staleness(&out, bin); err != nil {
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
	t.Parallel()
	dir := t.TempDir()
	gitIn(t, "-C", dir, "init", "-q")
	for _, rel := range []string{readmePath, toolsDocPath, configDocPath, changelogPath, "docs/new.md", "docs/sub/nested.md", "notes.txt"} {
		put(t, dir, rel, "# "+rel+"\r\n")
	}
	put(t, dir, ".gitignore", "scratch.md\n")
	put(t, dir, "scratch.md", "# ignored\n")
	gitIn(t, "-C", dir, "add", readmePath, toolsDocPath)

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
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	read := func(doc string) string {
		data, err := os.ReadFile(filepath.Join(root, doc))
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
}

// The README's plugin section names every directory the plugin stages a
// binary under, and says which pair runs another's under emulation, from
// the tables the plugin gate reads. A README without the section is
// reported, not passed.
func TestThePluginSectionNamesWhatThePluginStages(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "README.md"))
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
// entries, and an [Unreleased] section with and without them.
const (
	clReleased   = "## [0.9.0] - 2026-01-01\n\n### Added\n\n- The first tool.\n"
	clNext       = "## [0.9.1] - 2026-02-01\n\n### Added\n\n- A second tool.\n\n"
	clNextEmpty  = "## [0.9.1] - 2026-02-01\n\n"
	clUnreleased = "# Changelog\n\n## [Unreleased]\n\n"
	clWritten    = clUnreleased + "### Added\n\n- A second tool.\n\n"
	clEmpty      = "Go source changed and CHANGELOG.md [Unreleased] has no entry; " +
		"write one under [Unreleased], or under the version heading this release tags"
)

// The CHANGELOG rule: a change to Go source is written up under
// [Unreleased], or, on the release commit before its tag, under the
// version that commit releases.
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
			"a release commit with nothing under its version", clUnreleased + clNextEmpty + clReleased, true,
			[]string{"0.9.0"},
			[]string{"Go source changed and CHANGELOG.md [0.9.1], the untagged version this commit " +
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
		{"no [Unreleased] section", "# Changelog\n\n" + clReleased, true, []string{"0.9.0"}, []string{clEmpty}},
		{"no change to Go source", clUnreleased + clReleased, false, []string{"0.9.0"}, nil},
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

// What changelogRule is told comes from git: whether Go source outside
// the tests differs from the last tag, which is not always the commit
// before HEAD, and whether a version is tagged. Each row is a repository
// whose v0.9.0 tag is followed by one more commit.
func TestTheChangelogRuleReadsGit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		changelog string
		// source, test and readme say which files the commit after v0.9.0
		// changes, and later commits a change to the README alone after it.
		source, test, readme, later bool
		// tag tags that commit, and edit then changes Go source uncommitted.
		tag, edit bool
		want      []string
	}{
		{
			name: "an undocumented source change after the release is tagged", changelog: clUnreleased + clNext + clReleased,
			source: true, tag: true, edit: true, want: []string{clEmpty},
		},
		{
			name: "an undocumented source change two commits after the tag", changelog: clUnreleased + clReleased,
			source: true, later: true, want: []string{clEmpty},
		},
		{name: "the release commit before its tag", changelog: clUnreleased + clNext + clReleased, source: true},
		{name: "a change to a test alone", changelog: clUnreleased + clReleased, test: true},
		{name: "a change to no Go source", changelog: clUnreleased + clReleased, readme: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gitIn(t, "-C", dir, "init", "-q")
			put(t, dir, "main.go", "package main\n")
			put(t, dir, "main_test.go", "package main\n")
			put(t, dir, "README.md", "# Fixture\n")
			put(t, dir, changelogPath, clUnreleased+clReleased)
			gitIn(t, "-C", dir, "add", "-A")
			gitIn(t, "-C", dir, "commit", "-q", "-m", "Release 0.9.0")
			gitIn(t, "-C", dir, "tag", "-a", "v0.9.0", "-m", "v0.9.0")

			if tc.source {
				put(t, dir, "main.go", "package main\n\nfunc main() {}\n")
			}
			if tc.test {
				put(t, dir, "main_test.go", "package main\n\nfunc helper() {}\n")
			}
			if tc.readme {
				put(t, dir, "README.md", "# Fixture\n\nMore words.\n")
			}
			put(t, dir, changelogPath, tc.changelog)
			gitIn(t, "-C", dir, "add", "-A")
			gitIn(t, "-C", dir, "commit", "-q", "-m", "The next change")
			if tc.later {
				put(t, dir, "README.md", "# Fixture\n\nA later change.\n")
				gitIn(t, "-C", dir, "commit", "-q", "-am", "A later change")
			}
			if tc.tag {
				gitIn(t, "-C", dir, "tag", "-a", "v0.9.1", "-m", "v0.9.1")
			}
			if tc.edit {
				put(t, dir, "main.go", "package main\n\nfunc main() { main() }\n")
			}

			got, err := checkChangelog(dir, tc.changelog)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("checkChangelog() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Outside a git checkout the CHANGELOG rule has no tag to measure from.
// It reached no verdict, so it says so and exits 2, as the docs gate does.
func TestTheChangelogRuleNeedsAGitCheckout(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	_, err := checkChangelog(dir, clUnreleased+clReleased)
	if !errors.As(err, new(couldNotRun)) || exitCode(err) != 2 {
		t.Errorf("got %v; want a git failure that could not run", err)
	}
}
