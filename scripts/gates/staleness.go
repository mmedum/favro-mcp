package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
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
	changelog, unrun := checkChangelog(".", documents[changelogPath])
	if unrun != nil && !errors.As(unrun, new(couldNotRun)) {
		return unrun
	}
	// The tools, settings and plugin section are read from the prose, its
	// fences blanked: an example documents nothing. The permission rules
	// sit in a fence of their own.
	readme := documents[readmePath]
	prose := strings.Join(docsProse(readme), "\n")

	problems := slices.Clone(changelog)
	problems = append(problems, toolsDocumented(strings.Join(docsProse(documents[toolsDocPath]), "\n"), prose, registered)...)
	problems = append(problems, permissionRulesRegistered(readme, dump)...)
	problems = append(problems, envDocumented(config.Vars(), prose, documents[configDocPath])...)
	problems = append(problems, pluginPlatformsDocumented(prose)...)
	problems = append(problems, destructiveCountMatches(documents, dump)...)

	if len(problems) > 0 {
		if unrun != nil {
			problems = append(problems, unrun.Error())
		}
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	if unrun != nil {
		return unrun
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
// binary under, and every pair releaseMatrix runs under emulation, with
// the word. The section is the one under "### Cowork plugin", up to the
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
	for _, pair := range slices.Sorted(maps.Keys(emulated())) {
		if !strings.Contains(section, pair) || !strings.Contains(section, "emulation") {
			problems = append(problems, fmt.Sprintf("README.md's plugin section does not say that %s runs the %s "+
				"binary under emulation", pair, emulated()[pair]))
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

// checkChangelog holds the checkout at dir, whose CHANGELOG is text, to
// changelogRule, measured from the newest release HEAD reaches, as
// lastTagOn finds it for schema-diff. One tag listing serves both. No such
// tag, or a git or go call that cannot answer, is couldNotRun, never
// "source changed".
func checkChangelog(dir, text string) ([]string, error) {
	tags, err := releaseTags(dir)
	if err != nil {
		return nil, changelogUnrun(dir, err)
	}
	last, err := newestReleaseOn(dir, "HEAD", tags)
	if err != nil {
		return nil, changelogUnrun(dir, err)
	}
	if last == "" {
		return nil, changelogUnrun(dir, errors.New("HEAD reaches no release tag (vX.Y.Z) to measure from"))
	}
	changed, err := shippedChangedSince(dir, last)
	if err != nil {
		return nil, changelogUnrun(dir, err)
	}
	// A version is released once a tag names it on any line of history.
	tagged := func(version string) bool { return slices.Contains(tags, "v"+version) }
	return changelogRule(text, changed, tagged), nil
}

// changelogUnrun is why the CHANGELOG rule could not run in the checkout at
// dir. A shallow clone fetches no tag and too little history, so the
// reason names one.
func changelogUnrun(dir string, err error) error {
	if shallow, _ := git("-C", dir, "rev-parse", "--is-shallow-repository"); shallow == "true" {
		err = fmt.Errorf("%w; the checkout is a shallow clone: check it out with fetch-depth: 0", err)
	}
	return couldNotRun{fmt.Errorf("the CHANGELOG rule: %w", err)}
}

// changelogRule requires a change to the binary's source to be written
// up: an entry under [Unreleased], or, on a release commit, under the
// newest version heading while that version has no tag. A release commit
// moves the entries under the version it is about to tag, and CI runs on
// it before the tag exists.
func changelogRule(text string, sourceChanged bool, tagged func(version string) bool) []string {
	if !sourceChanged {
		return nil
	}
	doc := readChangelog(text)
	if doc.entries("Unreleased") > 0 {
		return nil
	}
	where := "[Unreleased]"
	if v := doc.versions(); len(v) > 0 && !tagged(v[0].label) {
		if doc.entries(v[0].label) > 0 {
			return nil
		}
		where = "[" + v[0].label + "], the untagged version this commit releases,"
	}
	return []string{"the binary's source changed and " + changelogPath + " " + where + " has no entry; " +
		"write one under [Unreleased], or under the version heading this release tags"}
}

// shippedChangedSince reports whether the commits since..HEAD of the
// checkout at dir change the binary's source: a Go file other than a test
// in a package of this module the binary imports, a file such a package
// embeds, or go.mod outside its require and tool lines. A package counts
// by its directory, so a file another platform builds counts, and so does
// a file removed from it. Commits are compared, as CI sees them.
func shippedChangedSince(dir, since string) (bool, error) {
	out, err := git("-C", dir, "diff", since+"..HEAD", "--name-only", "--no-renames", "-z")
	if err != nil {
		return false, err
	}
	var shipped *shippedSource
	for _, file := range strings.Split(out, "\x00") {
		switch file {
		case "":
		case "go.mod":
			if changed, err := goModChangedSince(dir, since); err != nil || changed {
				return changed, err
			}
		default:
			if shipped == nil {
				if shipped, err = listShipped(dir); err != nil {
					return false, err
				}
			}
			if shipped.has(file) {
				return true, nil
			}
		}
	}
	return false, nil
}

// shippedSource is what the binary is built from in this module, on this
// platform: its package directories and the files they embed, each
// slash-separated from the module root.
type shippedSource struct{ dirs, embeds map[string]bool }

func (s *shippedSource) has(file string) bool {
	return s.embeds[file] || strings.HasSuffix(file, ".go") && !strings.HasSuffix(file, "_test.go") && s.dirs[path.Dir(file)]
}

// listShipped asks the go command what ./cmd/... is built from in the
// module at dir. A package of another module is a dependency, which
// go.mod's require lines name.
func listShipped(dir string) (*shippedSource, error) {
	const what = "go list -deps ./cmd/..."
	cmd := exec.Command("go", "list", "-deps", "-json=Dir,EmbedFiles,Module", "./cmd/...")
	cmd.Dir = dir
	out, err := cmd.Output()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return nil, fmt.Errorf("%s: %s", what, strings.TrimSpace(string(exit.Stderr)))
	case err != nil:
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	s := &shippedSource{dirs: map[string]bool{}, embeds: map[string]bool{}}
	for dec := json.NewDecoder(bytes.NewReader(out)); dec.More(); {
		var p struct {
			Dir        string
			EmbedFiles []string
			Module     *struct {
				Main bool
				Dir  string
			}
		}
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		if p.Module == nil || !p.Module.Main {
			continue
		}
		rel, err := filepath.Rel(p.Module.Dir, p.Dir)
		if err != nil {
			return nil, err
		}
		pkg := filepath.ToSlash(rel)
		s.dirs[pkg] = true
		for _, f := range p.EmbedFiles {
			s.embeds[path.Join(pkg, f)] = true
		}
	}
	// An empty listing is not reading the binary, and would excuse every
	// change.
	if len(s.dirs) == 0 {
		return nil, fmt.Errorf("%s named no package of this module, so it is not reading the binary", what)
	}
	return s, nil
}

// goModChangedSince reports whether go.mod at HEAD in the checkout at dir
// differs from since's outside its require and tool lines: a go, toolchain
// or replace line changes what the binary is built with.
func goModChangedSince(dir, since string) (bool, error) {
	before, err := git("-C", dir, "show", since+":go.mod")
	if err != nil {
		return false, err
	}
	after, err := git("-C", dir, "show", "HEAD:go.mod")
	if err != nil {
		return false, err
	}
	return goModBuildLines(before) != goModBuildLines(after), nil
}

// goModBuildLines is a go.mod's lines, each trimmed and the blank ones
// dropped, without its require and tool directives, a block's included
// (https://go.dev/ref/mod#go-mod-file-require). A tool directive adds a
// package `go tool` runs (https://go.dev/ref/mod#go-mod-file-tool). ( is a
// token of its own, so `require(` opens a block as `require (` does
// (https://go.dev/ref/mod#go-mod-file-lexical).
func goModBuildLines(text string) string {
	var kept []string
	inBlock := false
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		verb, paren := "", false
		if len(fields) > 0 {
			verb, _, paren = strings.Cut(fields[0], "(")
		}
		switch {
		case inBlock:
			inBlock = len(fields) == 0 || !strings.HasPrefix(fields[0], ")")
		case len(fields) == 0:
		case verb == "require" || verb == "tool":
			inBlock = paren || len(fields) > 1 && strings.HasPrefix(fields[1], "(")
		default:
			kept = append(kept, strings.Join(fields, " "))
		}
	}
	return strings.Join(kept, "\n")
}
