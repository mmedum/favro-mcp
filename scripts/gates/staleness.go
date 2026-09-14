package main

import (
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	// A tool named in a markdown table's first column.
	tabledTool = regexp.MustCompile("(?m)^\\| `(favro_[a-z_]+)` \\|")
	// A tool named anywhere in prose, for tools described rather than
	// tabulated.
	mentionedTool = regexp.MustCompile("`(favro_[a-z_]+)`")
	// "83 tools", "83 typed tools" — a number in prose is a claim about
	// the list beside it, and this is how that claim gets held.
	toolCount = regexp.MustCompile(`\b(\d+)\s+(?:typed\s+)?tools\b`)
	// Every environment variable the code reads, as a string literal.
	envName = regexp.MustCompile(`"(FAVRO_[A-Z_]+)"`)
	// A version heading, as Keep a Changelog writes it.
	versionHeading = regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`)
	// The status line at the top of the design document, which is the
	// first thing anyone reads and the last thing anyone updates.
	statusLine = regexp.MustCompile(`(?m)^\*\*Status,? [^.]*\.\*\* Released: v?(\d+\.\d+\.\d+)`)
	// Something shaped like a path into this repository.
	pathLike = regexp.MustCompile("`([A-Za-z0-9_.\\-]+/[A-Za-z0-9_./\\-]+)`")
	// "the fourteen delete-style tools", "14 destructive tools" — a
	// number in prose is a claim about a list the binary holds.
	//
	// The alternation is explicit rather than `[a-z]+` because the count
	// of matches is the floor: with any word accepted, "the delete-style
	// tools" matched, was discarded as unreadable, and still counted as
	// something read. Nine of ten matches were articles, so the floor
	// was satisfied by prose that held nothing — rule 14's failure
	// inside the checker written to prevent it.
	destructiveCount = regexp.MustCompile(`(?i)\b(\d+|` + numberWords + `)\s+(?:delete-style|destructive)\s+tools\b`)
)

// staleness fails when the documentation drifts from the code. Each
// check answers a question somebody asked once and then stopped asking:
// does the documentation list the tools that exist, does the number in
// the sentence match the list beside it, does the design document still
// claim the version it claimed five releases ago, does every path a
// document names still exist.
//
// Expect it to be narrower than it looks. Its scope is itself a
// hand-maintained list, which is the failure this whole programme keeps
// describing, applied to the checker instead of the code — so each rule
// below derives its expected set from the code rather than from a list
// typed here.
func staleness(w io.Writer, bin string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	dump, err := toolDump(bin)
	if err != nil {
		return err
	}
	registered := toolNames(dump)
	if len(registered) < 50 {
		return fmt.Errorf("the binary registered %d tools; the check is not looking at this server", len(registered))
	}

	// Read once. Two independent readers of one file is how a
	// normalisation added to one of them silently fails to apply to the
	// other.
	docs, err := readDocuments(root)
	if err != nil {
		return err
	}
	readme, tools := docs["README.md"], docs["docs/TOOLS.md"]
	changelog, arch := docs["CHANGELOG.md"], docs["docs/architecture.md"]

	var problems []string
	problems = append(problems, toolsDocumented(tools, readme, registered)...)
	problems = append(problems, countsMatch(readme, len(registered))...)
	problems = append(problems, envDocumented(root, readme, docs["docs/configuration.md"])...)
	problems = append(problems, gatesDocumented(docs["docs/development.md"])...)
	problems = append(problems, destructiveCountMatches(docs, dump)...)
	problems = append(problems, statusCurrent(arch, changelog)...)
	problems = append(problems, packageMapCurrent(root, arch)...)
	problems = append(problems, pathsExist(root, docs, deliveryPhases(arch))...)
	if err := changelogDocumentsTheChange(root, changelog); err != nil {
		problems = append(problems, err.Error())
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	gates := 0
	for _, c := range commands {
		if c.gate {
			gates++
		}
	}
	_, err = fmt.Fprintf(w,
		"staleness ok: %d tools and %d gates documented across %d documents, counts and paths current\n",
		len(registered), gates, len(docs))
	return err
}

// readDocuments loads every document any rule here reads, keyed by the
// path the failure messages name.
func readDocuments(root string) (map[string]string, error) {
	docs := map[string]string{}
	for _, name := range append([]string{"CHANGELOG.md"}, documentsWithPaths...) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		docs[name] = string(data)
	}
	return docs, nil
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

// countsMatch holds every "N tools" in the README against the list it
// describes. A number in prose is a copy of a fact, and a copy goes
// stale because it is a copy.
func countsMatch(readme string, registered int) []string {
	return proseCount(map[string]string{"README.md": readme}, toolCount, registered, "registers",
		"README.md states no tool count; this check has nothing to hold "+
			"(if the count was deliberately removed, remove this rule with it)")
}

// proseCount holds every number written in prose against the number the
// binary actually has.
//
// Both callers do the same four things — find the claims, read them as
// numbers, compare, and refuse to pass on having found none — and the
// fourth is the one that is easy to get subtly wrong. A match that is
// not a number must not count as having read something, which is why
// each regexp passed here matches only digits or a number word.
func proseCount(sources map[string]string, re *regexp.Regexp, want int, verb, missing string) []string {
	var problems []string
	checked := 0
	for _, name := range slices.Sorted(maps.Keys(sources)) {
		for _, m := range re.FindAllStringSubmatch(sources[name], -1) {
			n, ok := countWord(m[1])
			if !ok {
				// Unreachable while the regexp only matches numbers,
				// and cheap insurance if one later does not.
				continue
			}
			checked++
			if n != want {
				problems = append(problems, fmt.Sprintf(
					"%s says %q and the binary %s %d", name, strings.TrimSpace(m[0]), verb, want))
			}
		}
	}
	if checked == 0 {
		return []string{missing}
	}
	return problems
}

// envDocumented requires every FAVRO_* variable the code reads to be
// named in the README. The list comes from the source, so a variable
// added next week is documented or the gate fails on the commit that
// added it.
func envDocumented(root, readme, configuration string) []string {
	seen, err := sourceEnvNames(root)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	for _, name := range slices.Sorted(maps.Keys(seen)) {
		if !strings.Contains(readme, name) {
			problems = append(problems, "README.md does not document "+name)
		}
		// docs/configuration.md is the reference, so a setting missing
		// there is missing from the place someone goes to look it up —
		// the README's table is a summary that may reasonably lag.
		if !strings.Contains(configuration, name) {
			problems = append(problems, "docs/configuration.md does not document "+name)
		}
	}
	return problems
}

// gatesDocumented holds docs/development.md to the gate registry.
//
// The list is derived from the registry rather than compared against one
// typed here, which is the whole point: a gate added to `main.go` and to
// both CI lists is still invisible to anyone reading the documentation,
// and a table of gates is exactly the kind of prose that silently stops
// describing the program.
func gatesDocumented(development string) []string {
	var problems []string
	counted := 0
	for _, name := range slices.Sorted(maps.Keys(commands)) {
		if !commands[name].gate {
			continue
		}
		counted++
		if !strings.Contains(development, "`"+name+"`") {
			problems = append(problems, "docs/development.md does not name the "+name+" gate")
		}
	}
	if counted < 10 {
		return []string{fmt.Sprintf("the registry reports %d gates; this check is not reading it", counted)}
	}
	return problems
}

// destructiveCountMatches holds any prose count of the delete-style
// tools to the number the binary actually annotates.
//
// This is the §7b failure the design document describes, and it has now
// happened twice in this repository: the count was written as twelve
// while thirteen were annotated, and after that was corrected a later
// phase added a fourteenth and the sentence stayed at thirteen. A number
// in prose is a claim about a list, and the list is in the binary.
func destructiveCountMatches(docs map[string]string, dump *schemaDump) []string {
	want := 0
	for _, t := range dump.Tools {
		if t.Annotations.DestructiveHint {
			want++
		}
	}
	if want == 0 {
		return []string{"the dump annotates no destructive tools; this check is reading the wrong field"}
	}

	// CHANGELOG.md is excluded for the reason documentsWithPaths gives:
	// once an entry moves under a version heading it is history, and it
	// correctly describes what was true then. Holding a frozen sentence
	// to today's binary would fail the build on the commit that adds the
	// next delete tool, for a line nobody should edit.
	live := map[string]string{}
	for name, text := range docs {
		if name != "CHANGELOG.md" {
			live[name] = text
		}
	}
	return proseCount(live, destructiveCount, want, "annotates",
		fmt.Sprintf("no living document states how many delete-style tools there are, so this check "+
			"is reading nothing; the count it would hold is %d", want))
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

// statusCurrent compares the design document's status line with the
// newest released version. A status line claims something no tag can
// derive — which phase is current — so it cannot be replaced by a badge,
// and nothing else will catch it. A sibling's said v0.5.0 five releases
// after v0.5.0.
//
// The newest changelog heading is accepted as well as the newest tag,
// or this fails on the release commit that writes the new number before
// the tag exists.
func statusCurrent(arch, changelog string) []string {
	m := statusLine.FindStringSubmatch(arch)
	if m == nil {
		return []string{"docs/architecture.md has no `**Status, ...** Released: vX.Y.Z` line; " +
			"the check is reading the wrong file"}
	}
	headings := versionHeading.FindAllStringSubmatch(changelog, -1)
	if len(headings) == 0 {
		return []string{"CHANGELOG.md has no version headings; the check is reading the wrong file"}
	}
	newest := headings[0][1]
	if m[1] != newest {
		return []string{fmt.Sprintf(
			"docs/architecture.md says the released version is v%s; the newest CHANGELOG heading is [%s]", m[1], newest)}
	}
	return nil
}

// packageMapCurrent holds the module layout in the design document
// against `go list`. A "where things go" list drifts silently: a
// sibling's named 13 of 20 packages, and the seven missing included the
// one carrying a confidentiality guarantee.
//
// Only one direction is checked — every real package is named — because
// the map deliberately also names packages a later phase creates, and
// failing on those would make the document unable to describe its own
// plan.
func packageMapCurrent(root, arch string) []string {
	module, err := moduleName()
	if err != nil {
		return []string{err.Error()}
	}
	pkgs, err := internalPackages(module, root)
	if err != nil {
		return []string{err.Error()}
	}
	if len(pkgs) < 4 {
		return []string{fmt.Sprintf("go list found %d packages under internal/; the check is not reading them", len(pkgs))}
	}
	var problems []string
	for _, pkg := range pkgs {
		if !strings.Contains(arch, pkg+"/") {
			problems = append(problems, "docs/architecture.md §5 does not name "+pkg)
		}
	}
	return problems
}

// documentsWithPaths are the files whose prose is checked for paths that
// no longer exist. CHANGELOG.md is deliberately absent: its older
// entries are history, and they correctly describe what was true then.
var documentsWithPaths = []string{
	"README.md", "CLAUDE.md", "CONTRIBUTING.md", "SECURITY.md",
	"docs/architecture.md", "docs/TOOLS.md",
	"docs/configuration.md", "docs/development.md", "docs/security.md",
}

// pathsExist stats everything shaped like a repository path. A file
// moves and the sentence about it does not.
//
// Expect the naive version of this to be mostly false positives and
// budget for the triage rather than trusting the count: a sibling's
// first pass reported 13 broken references and every one was the
// extractor's fault. This one's first pass reported 17 and three were
// real. What the other fourteen taught is written into the two
// narrowings below, because a path check that has not been tuned tells
// you about your regexp rather than about your documentation.
//
// First: the path's top-level directory has to be something git tracks.
// `bin/favro-mcp.cmd` is a path inside the plugin bundle, not inside
// this repository, and it was reported only because a build had left a
// bin/ directory lying around — which means the check's answer depended
// on whether somebody had run make.
//
// Second: a path that does not exist is acceptable if a phase that has
// NOT yet been delivered names it. These documents are a plan as well as
// a description, and a plan that cannot name the package a later phase
// creates is not a plan. The plan is read once, from
// docs/architecture.md §16, and applies to every document — CLAUDE.md
// points at the same phases and must be able to say so.
//
// The "not yet delivered" half is what keeps this from being a loophole.
// Exempting anything §16 mentions would mean a stale path could be
// laundered by adding a line to the plan, and — worse — that the
// exemption would never expire: once a phase marked **Done.** has
// shipped, its promise would go on excusing a path a later rename
// deleted. So a phase's paths are exempt only while the phase is
// outstanding, and a delivered phase's paths are held to existing like
// everything else. A phase claiming completion for a package nobody
// created fails here.
func pathsExist(root string, docs map[string]string, outstanding string) []string {
	tracked, err := trackedTopLevel(root)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	examined := 0
	for _, doc := range documentsWithPaths {
		for _, m := range pathLike.FindAllStringSubmatch(docs[doc], -1) {
			p := strings.TrimSuffix(m[1], "/")
			if strings.Contains(p, "://") || strings.ContainsAny(p, "*{}$") ||
				strings.Contains(p, "...") || strings.Contains(p, "favro.com") {
				continue
			}
			first, _, _ := strings.Cut(p, "/")
			if !tracked[first] {
				continue
			}
			examined++
			if _, err := os.Stat(filepath.Join(root, p)); err == nil {
				continue
			}
			if outstanding != "" && strings.Contains(outstanding, p) {
				continue // promised by a phase that has not shipped yet
			}
			problems = append(problems, fmt.Sprintf(
				"%s names %s, which does not exist and no outstanding delivery phase promises", doc, p))
		}
	}
	// The floor. This is the one rule here reading prose rather than
	// code, so it is the one that can quietly stop matching: if the
	// documents move to links instead of code spans, every path check
	// passes forever and prints exactly what it prints now.
	if examined < 20 {
		problems = append(problems, fmt.Sprintf(
			"only %d repository paths found across %d documents; the extractor has stopped reading them",
			examined, len(documentsWithPaths)))
	}
	return problems
}

// deliveryPhases is the part of the design document that promises what
// phases still to come will build: §16, less every entry that says
// **Done.**
//
// A phase is one bullet, so the split is by bullet, and anything past
// the next heading is a different section rather than a promise.
func deliveryPhases(arch string) string {
	_, plan, ok := strings.Cut(arch, "## 16. Delivery phases")
	if !ok {
		return ""
	}
	if end := strings.Index(plan, "\n## "); end >= 0 {
		plan = plan[:end]
	}
	var outstanding []string
	for _, entry := range strings.Split(plan, "\n- ") {
		if !strings.Contains(entry, "Done.**") {
			outstanding = append(outstanding, entry)
		}
	}
	return strings.Join(outstanding, "\n")
}

// trackedTopLevel is the set of top-level names git tracks. Build output
// is not among them, which is what keeps a path inside a packaged bundle
// from being read as a path inside this repository.
func trackedTopLevel(root string) (map[string]bool, error) {
	files, err := gitLines(root, "ls-files", "-z")
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	tracked := map[string]bool{}
	for _, f := range files {
		first, _, _ := strings.Cut(f, "/")
		tracked[first] = true
	}
	if len(tracked) < 5 {
		return nil, fmt.Errorf("git tracks %d top-level entries; the check is not reading the repository", len(tracked))
	}
	return tracked, nil
}

// changelogDocumentsTheChange requires entries under [Unreleased] when
// Go source has changed since the last tag.
//
// The exception is the release commit itself, which moves the entries
// out of [Unreleased] and under a new heading — leaving the section
// legitimately empty. A sibling's version of this blocked its own
// release pull request, which is how the exception got written.
func changelogDocumentsTheChange(root, changelog string) error {
	tag := lastTag()
	if tag == "" {
		return nil
	}
	out, err := exec.Command("git", "-C", root, "diff", "--name-only", tag+"..HEAD").Output()
	if err != nil {
		// A shallow clone has no range to compare, which is a fact about
		// the checkout rather than about the changelog.
		return nil //nolint:nilerr // deliberate: nothing to compare is not a finding
	}
	changed := false
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}
	if !strings.Contains(changelog, "## [Unreleased]") {
		return fmt.Errorf("CHANGELOG.md has no [Unreleased] section")
	}
	// changelogSection rather than a second hand-rolled cut: it also
	// stops at the `[x.y.z]: http` link-reference block, which sits under
	// no heading. The copy this replaces did not, so a changelog whose
	// [Unreleased] section was followed by nothing but that block read as
	// documented when it documented nothing.
	if strings.TrimSpace(changelogSection(changelog, "Unreleased")) != "" {
		return nil
	}
	// Empty is correct on a release commit, where the newest heading is
	// newer than the last tag.
	if headings := versionHeading.FindAllStringSubmatch(changelog, -1); len(headings) > 0 &&
		"v"+headings[0][1] != tag {
		return nil
	}
	return fmt.Errorf("go source changed since %s and [Unreleased] is empty", tag)
}
