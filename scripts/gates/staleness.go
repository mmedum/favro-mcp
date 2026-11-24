package main

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mmedum/favro-mcp/v4/internal/config"
)

var (
	// A tool named in a markdown table's first column.
	tabledTool = regexp.MustCompile("(?m)^\\| `(favro_[a-z_]+)` \\|")
	// A tool named anywhere in prose, for tools described rather than
	// tabulated.
	mentionedTool = regexp.MustCompile("`(favro_[a-z_]+)`")
	// A word: a run of letters of either case, digits and _, with the *
	// after it when there is one. A setting's name is a whole word, so a
	// longer one that contains it, such as FAVRO_UPLOAD_DIRS,
	// FAVRO_UPLOAD_DIRs or xFAVRO_UPLOAD_DIR, is another word, and
	// FAVRO_UPLOAD_* is a pattern rather than a name.
	settingWord = regexp.MustCompile(`[A-Za-z0-9_]+\*?`)
	// "the fourteen delete-style tools", "14 destructive tools" — a
	// number in prose is a claim about a list the binary holds.
	//
	// The alternation is explicit rather than `[a-z]+` because the count
	// of matches is the floor: with any word accepted, "the delete-style
	// tools" matches, is discarded as unreadable, and still counts as
	// something read, so prose that holds no number satisfies the floor.
	destructiveCount = regexp.MustCompile(`(?i)\b(\d+|` + numberWords + `)\s+(?:delete-style|destructive)\s+tools\b`)
)

// The documents the rules here name. Every other Markdown file at the
// root and under docs/ is read for the destructive count.
const (
	readmePath    = "README.md"
	toolsDocPath  = "docs/TOOLS.md"
	configDocPath = "docs/configuration.md"
	docsSubdir    = "docs"
)

// staleness fails when the documentation drifts from the code. Each
// check answers a question nobody keeps asking by hand: does the
// documentation list the tools that exist, does the README give a
// permission rule for each destructive one and name what the plugin
// carries, does configuration.md name every setting, does a count in
// prose match the binary, and does the CHANGELOG say what changed. What
// the docs cite, link and name as a path is the docs gate's.
//
// Each rule derives its expected set from the code rather than from a
// list typed here. A CHANGELOG rule that cannot run holds back no other
// check, and says why it did not run.
func staleness(w io.Writer, bin string) error {
	dump, err := toolDump(bin)
	if err != nil {
		return err
	}
	return stalenessOf(w, dump)
}

// stalenessOf holds the checkout in the working directory to the surface
// dump, which a test makes in process.
func stalenessOf(w io.Writer, dump *schemaDump) error {
	registered := toolNames(dump)
	if len(registered) < 50 {
		return fmt.Errorf("the binary registered %d tools; the check is not looking at this server", len(registered))
	}

	// Read once. Two independent readers of one file is how a
	// normalization added to one of them silently fails to apply to the
	// other.
	documents, err := readDocuments(".")
	if err != nil {
		return err
	}
	// The tools, settings and plugin section are read from the prose, its
	// fences blanked: an example documents nothing. The permission rules
	// sit in a fence of their own.
	readme := documents[readmePath]
	prose := strings.Join(docsProse(readme), "\n")

	problems := toolsDocumented(strings.Join(docsProse(documents[toolsDocPath]), "\n"), prose, registered)
	problems = append(problems, permissionRulesRegistered(readme, dump)...)
	problems = append(problems, envDocumented(config.Vars(), prose, documents[configDocPath])...)
	problems = append(problems, pluginPlatformsDocumented(prose)...)
	problems = append(problems, desktopEmulationDocumented(prose)...)
	problems = append(problems, destructiveCountMatches(documents, dump)...)

	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	_, err = fmt.Fprintf(w, "staleness ok: %d tools documented and %d settings, across %d documents\n",
		len(registered), len(config.Vars()), len(documents))
	return err
}

// readDocuments loads every Markdown file at the root and directly
// under docs/ that the docs gate reads, which is what git would commit,
// keyed by the path the failure messages name. A document a rule names
// is required, so a moved one fails here rather than as a rule reading
// nothing. A checkout git cannot list is a gate that could not run.
func readDocuments(root string) (map[string]string, error) {
	files, err := docsFiles(root)
	if err != nil {
		return nil, couldNotRun{err}
	}
	documents := map[string]string{}
	for _, rel := range files {
		if dir := path.Dir(rel); path.Ext(rel) != ".md" || (dir != "." && dir != docsSubdir) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		documents[rel] = strings.ReplaceAll(string(data), "\r\n", "\n")
	}
	for _, name := range []string{readmePath, toolsDocPath, configDocPath, changelogPath} {
		if _, ok := documents[name]; !ok {
			return nil, fmt.Errorf("%s is missing; staleness reads it", name)
		}
	}
	return documents, nil
}

func toolDump(bin string) (*schemaDump, error) {
	out, err := dumpSchemas(bin)
	if err != nil {
		return nil, err
	}
	// parseSchemas rather than a second json.Unmarshal: its "no tools in
	// the dump" guard is the floor that tells a malformed dump from an
	// empty server, and a copy of the decode would not carry it.
	dump, err := parseSchemas(out)
	if err != nil {
		return nil, fmt.Errorf("parse --dump-schemas: %w", err)
	}
	return dump, nil
}

func toolNames(dump *schemaDump) []string {
	names := make([]string, 0, len(dump.Tools))
	for _, t := range dump.Tools {
		names = append(names, t.Name)
	}
	slices.Sort(names)
	return names
}

// toolsDocumented reads two directions, because they ask different
// things. Every registered tool must be documented SOMEWHERE — the
// reference table, or the prose that explains a flag — so the missing
// check reads the whole of both files. A row naming something that is
// not a tool is a different error, and only the table is read for it:
// an inline code span is as likely to be a field name.
func toolsDocumented(toolsDoc, readme string, registered []string) []string {
	mentioned := map[string]bool{}
	for _, m := range mentionedTool.FindAllStringSubmatch(toolsDoc+readme, -1) {
		mentioned[m[1]] = true
	}
	var missing []string
	for _, name := range registered {
		if !mentioned[name] {
			missing = append(missing, name)
		}
	}
	var problems []string
	if len(missing) > 0 {
		problems = append(problems, "docs/TOOLS.md and README.md document neither of: "+strings.Join(missing, ", "))
	}
	var tabled []string
	for _, m := range tabledTool.FindAllStringSubmatch(toolsDoc, -1) {
		tabled = append(tabled, m[1])
	}
	if len(tabled) == 0 {
		problems = append(problems, "docs/TOOLS.md has no tool table rows; the check is reading the wrong file")
	}
	for _, name := range tabled {
		if !slices.Contains(registered, name) {
			problems = append(problems, "docs/TOOLS.md names a tool that is not registered: "+name)
		}
	}
	return problems
}

// permissionRule is a Claude Code permission rule for one of this
// server's tools, under the server name the README configures.
var permissionRule = regexp.MustCompile(`"mcp__favro__([a-z0-9_]+)"`)

// permissionRulesRegistered holds the README's permission rules to the
// tools the binary registers, both ways. A rule for a tool that does not
// exist matches nothing, so the person it was written to ask is never
// asked; a destructive tool with no rule is allowed by whoever copied
// the example.
func permissionRulesRegistered(readme string, dump *schemaDump) []string {
	rules := permissionRule.FindAllStringSubmatch(readme, -1)
	if len(rules) == 0 {
		return []string{"README.md gives no `permissions.ask` rule for this server's tools; " +
			"the check has nothing to hold (if the example was removed on purpose, remove this rule with it)"}
	}
	ruled := make([]string, len(rules))
	for i, m := range rules {
		ruled[i] = m[1]
	}
	registered := toolNames(dump)
	var problems []string
	for _, name := range ruled {
		if !slices.Contains(registered, name) {
			problems = append(problems, "README.md gives a permission rule for "+name+", which the binary does not register")
		}
	}
	for _, t := range dump.Tools {
		if t.takesConfirm() && !slices.Contains(ruled, t.Name) {
			problems = append(problems, "README.md gives no permission rule for "+t.Name+", which is destructive")
		}
	}
	return problems
}

// envDocumented requires every variable the server reads, vars, to be
// named in the README's prose and in docs/configuration.md's, each as a
// whole word, and the configuration page to name no other anywhere. A
// fenced example documents nothing, and still names what it names. The
// list is config's registry, so a variable added next week is documented
// or the gate fails on the commit that added it.
func envDocumented(vars []config.Var, readme, configuration string) []string {
	if len(vars) == 0 {
		return []string{"config.Vars lists no variable; this check is reading nothing"}
	}
	words := func(text string) map[string]bool {
		out := map[string]bool{}
		for _, word := range settingWord.FindAllString(text, -1) {
			if rest, ok := strings.CutPrefix(word, config.EnvPrefix); ok && rest != "" &&
				strings.Trim(rest, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") == "" {
				out[word] = true
			}
		}
		return out
	}
	inReadme, inConfiguration := words(readme), words(strings.Join(docsProse(configuration), "\n"))
	var problems []string
	known := map[string]bool{}
	for _, v := range vars {
		known[v.Name] = true
		if !inReadme[v.Name] {
			problems = append(problems, "README.md does not document "+v.Name)
		}
		// docs/configuration.md is the reference, so a setting missing
		// there is missing from the place someone goes to look it up —
		// the README's table is a summary that may reasonably lag.
		if !inConfiguration[v.Name] {
			problems = append(problems, "docs/configuration.md does not document "+v.Name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(words(configuration))) {
		if !known[name] {
			problems = append(problems, "docs/configuration.md documents "+name+", which the server does not read")
		}
	}
	return problems
}

// pluginPlatformsDocumented holds the README's plugin section to the
// plugin's staging table: it names every directory the plugin stages a
// binary under. The section is the one under "### Cowork plugin", up to the
// next heading.
func pluginPlatformsDocumented(readme string) []string {
	const heading = "### Cowork plugin"
	_, section, found := strings.Cut(readme, heading)
	if !found {
		return []string{"README.md has no " + heading + " section, so nothing says what the plugin carries"}
	}
	if end := strings.Index(section, "\n#"); end >= 0 {
		section = section[:end]
	}
	var problems []string
	for _, p := range platforms {
		if !strings.Contains(section, p.dir()) {
			problems = append(problems, "README.md's plugin section does not name "+p.dir()+", which the plugin stages")
		}
	}
	return problems
}

// desktopEmulationDocumented holds the README's Desktop bundle section to
// releaseMatrix: it names every pair the bundle runs another pair's
// binary on, with the word emulation. The section is the one under
// "### Claude Desktop bundle", up to the next heading.
func desktopEmulationDocumented(readme string) []string {
	const heading = "### Claude Desktop bundle"
	_, section, found := strings.Cut(readme, heading)
	if !found {
		return []string{"README.md has no " + heading + " section, so nothing says what the Desktop bundle runs"}
	}
	if end := strings.Index(section, "\n#"); end >= 0 {
		section = section[:end]
	}
	var problems []string
	for _, p := range releaseMatrix {
		if p.runs != "" && (!strings.Contains(section, p.pair) || !strings.Contains(section, "emulation")) {
			problems = append(problems, fmt.Sprintf("README.md's Desktop bundle section does not say that %s runs the %s "+
				"binary under emulation", p.pair, p.runs))
		}
	}
	return problems
}

// destructiveCountMatches holds any prose count of the delete-style
// tools to the number the binary actually gates: the tools that take
// confirm. A number in prose is a claim about a list, the list is in the
// binary, and a tool added later leaves the sentence behind.
//
// A match that is not a number must not count as having read
// something, or prose holding no number passes; destructiveCount
// matches only digits or a number word.
func destructiveCountMatches(documents map[string]string, dump *schemaDump) []string {
	want := 0
	for _, t := range dump.Tools {
		if t.takesConfirm() {
			want++
		}
	}
	if want == 0 {
		return []string{"the dump has no tool that takes confirm; this check is reading the wrong field"}
	}

	var problems []string
	checked := 0
	for _, name := range slices.Sorted(maps.Keys(documents)) {
		// CHANGELOG.md is excluded: once an entry moves under a version
		// heading it is history, and it correctly describes what was true
		// then. Holding a frozen sentence to the binary would fail the
		// build on the commit that adds the next delete tool, for a line
		// nobody should edit.
		if name == changelogPath {
			continue
		}
		for _, m := range destructiveCount.FindAllStringSubmatch(documents[name], -1) {
			n, ok := countWord(m[1])
			if !ok {
				// Unreachable while the regexp only matches numbers,
				// and cheap insurance if one later does not.
				continue
			}
			checked++
			if n != want {
				problems = append(problems, fmt.Sprintf(
					"%s says %q and the binary gates %d", name, strings.TrimSpace(m[0]), want))
			}
		}
	}
	if checked == 0 {
		return []string{fmt.Sprintf("no living document states how many delete-style tools there are, so this "+
			"check is reading nothing; the count it would hold is %d", want)}
	}
	return problems
}

// numberWords are the spellings prose actually uses for a count this
// size, as a regexp alternation. Written once and consumed by the
// pattern and the reader both, so a spelling one accepts is a spelling
// the other resolves.
// It runs from one rather than from ten because a count can fall as well
// as rise, and a spelling outside the list is not a match at all — so
// the list's own range is the checker's blind spot.
const numberWords = "one|two|three|four|five|six|seven|eight|nine|ten|" +
	"eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty"

// countWord reads a match of that alternation, or a run of digits.
func countWord(s string) (int, bool) {
	for i, word := range strings.Split(numberWords, "|") {
		if strings.EqualFold(s, word) {
			return i + 1, true
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// bundleManifests are the files the packers stage that tell a client how
// to run the server, so a change to one changes what users get.
// Not counted: README.md, LICENSE, NOTICE, the registry entry, and the launchers scripts/gates generates.
var bundleManifests = map[string]bool{mcpbManifestPath: true, manifestPath: true, mcpPath: true}
