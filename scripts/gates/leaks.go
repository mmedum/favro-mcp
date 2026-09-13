package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// leaks is the confidentiality gate: nothing that identifies a Favro
// tenant may be in this repository.
//
// CLAUDE.md has carried that rule since phase 3 and nothing held it,
// which is the shape the shared standard warns about — a rule nobody can
// fail is a rule nobody is keeping. This is the thing that can fail.
//
// A leak gate cannot be patterns alone when the payload is ordinary
// words: an organization's name, a card title or a sentence out of a
// real card has no shape a scanner can match, and a pattern scan over
// that kind of payload reports clean because it can report nothing else.
// Two rules make this gate real rather than decorative, and they live
// outside it: fixtures are generated, never recorded from a live
// response, and the live driver (phase A6) reads only what it wrote. So
// what is scanned here is the residue those two rules cannot prevent —
// an id pasted into a comment, an address in a commit message, a URL
// copied out of the app.
//
// Every rule below is therefore anchored on a shape this server's own
// generated values cannot take: a hex id of exactly the length Favro
// mints, a keyword immediately before a value, an `@` with a
// dot-suffixed domain, a known URL prefix. Anchoring that way is what
// keeps a four-character value from colliding with a timestamp, which is
// how a sibling's scan failed one run in twenty-five and taught everyone
// to rerun it.

// skipFiles are files whose content is machine-generated in a shape that
// trips the id rules and carries nothing about anybody.
var skipFiles = map[string]bool{"go.sum": true}

var (
	// An address at a domain someone could actually own. RFC 2606 and
	// RFC 6761 reserve the rest for documentation and tests.
	email        = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@([A-Za-z0-9.\-]+\.[A-Za-z]{2,})`)
	safeDomains  = map[string]bool{"example.com": true, "example.org": true, "example.net": true}
	safeSuffixes = []string{".test", ".invalid", ".localhost", ".example"}

	// A Favro resource id: 24 hex characters, which is what Favro mints
	// for cards, widgets, columns, tags, users, groups and custom
	// fields. A 40-character git SHA does not match — the boundaries
	// are anchored — and this repository's own fixtures are short
	// hyphenated names, so anything of this exact shape came from a
	// tenant unless it says otherwise in the value itself.
	favroID = regexp.MustCompile(`\b[0-9a-f]{24}\b`)

	// An organization id is base62 and mixed-case, which is too ordinary
	// a shape to match on its own — so it is matched by the keyword in
	// front of it instead. That is the construction the standard
	// recommends wherever the payload has no shape: a literal keyword
	// before an id cannot collide with anything the server generates.
	//
	// The value must be QUOTED, or the assignment must be an environment
	// one. Without that, `{"organization_id": smokeOrgID}` reads as a
	// leak when the "value" is a Go identifier naming a constant — which
	// is code, not data, and reporting it is how a gate teaches people
	// to skim past its output.
	keyedID = regexp.MustCompile(`(?i)\b(organization[_-]?id|organizationId)\b["']?\s*[:=]\s*"([A-Za-z0-9_\-]{8,})"`)
	envID   = regexp.MustCompile(`\bFAVRO_ORGANIZATION_ID\s*=\s*["']?([A-Za-z0-9_\-]{8,})`)

	// A credential, by the same construction.
	keyedToken = regexp.MustCompile(`(?i)\b(FAVRO_API_TOKEN|api[_-]?token|favro[_-]?token)\b["']?\s*[:=]\s*"([A-Za-z0-9._\-]{16,})"`)
	envToken   = regexp.MustCompile(`\bFAVRO_API_TOKEN\s*=\s*["']?([A-Za-z0-9._\-]{16,})`)

	// A link out of the Favro app, which carries ids in its path. The
	// bare host is allowed and has to be: the client's base URL names
	// it, and so does every link in the README.
	appURL = regexp.MustCompile(`\bfavro\.com/(?:organization|widget|collection|card|w|c)/[A-Za-z0-9_\-]{6,}`)

	// A human card reference — the "BSC-123" form people paste out of
	// the UI. Its shape is shared with every standards-body citation in
	// the repository, which is what allowedRefPrefix is for.
	cardRef = regexp.MustCompile(`\b([A-Z]{2,5})-[0-9]{1,6}\b`)

	// What an invented value looks like: a marker word, or a run of one
	// character that no minted id has. RE2 has no backreferences, so the
	// run is counted rather than matched.
	inventedWord = regexp.MustCompile(`(?i)synthetic|fixture|nosuch|unknown|example|scratch|placeholder|redacted`)
)

// allowedRefPrefixes are the prefixes that make a card-reference-shaped
// token — `XXX-123` — a citation instead. Each names why, and each is // leakcheck:allow
// asserted by TestEveryAllowedPrefixIsUsed to actually occur in the
// repository — an exemption that exempts nothing reads as a considered
// decision and is really just a hole somebody forgot to close.
var allowedRefPrefixes = map[string]string{
	"GO":  "Go vulnerability database ids (GO-2026-4971), cited in the changelog",
	"ISO": "ISO-8601, cited wherever a date format is described",
	"BSD": "SPDX licence identifiers (BSD-2-Clause, BSD-3-Clause) in the licence allowlist",
	"MIT": "the SPDX identifier MIT-0, named where the licence check ignores a module",
	"UTF": "UTF-8, cited where internal/render explains why it clips on a rune boundary",
	"BSC": "the example card reference in tool descriptions, fixtures and the search tokenizer's comment. Confirmed with the maintainer on 2026-09-13 that it belongs to no real board, which is the only thing that makes this entry an exemption rather than a hole — it has to stay invented",
}

func leaks(w io.Writer, args []string) error {
	if len(args) == 1 && args[0] == "history" {
		return leakHistory(w)
	}
	if len(args) > 0 {
		return fmt.Errorf("usage: gates leaks [history]")
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	// --others --exclude-standard adds files that are not committed yet
	// but would be by the next `git add -A`. Scanning only the index
	// would let a brand-new file carrying an id pass until somebody
	// staged it, which is the wrong way round: the point is to catch it
	// before it reaches the history at all.
	out, err := exec.Command("git", "-C", root, "ls-files", "-z",
		"--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		// Outside a git checkout there is nothing to enumerate and
		// nothing to be wrong. Any other failure is a broken check, and
		// a broken check must not pass.
		var ee *exec.ExitError
		if errors.As(err, &ee) && strings.Contains(string(ee.Stderr), "not a git repository") {
			_, err := fmt.Fprintln(w, "leaks: not a git checkout; nothing to enumerate")
			return err
		}
		return fmt.Errorf("git ls-files: %w", err)
	}
	files := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	if len(files) < 20 {
		return fmt.Errorf("only %d files; the scan is not seeing the repository", len(files))
	}

	var problems []string
	scanned := 0
	for _, name := range files {
		if skipFiles[filepath.Base(name)] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue // deleted or unreadable is not this gate's business
		}
		if bytes.IndexByte(data, 0) >= 0 {
			// Binary, so none of the rules below can read it — which is
			// how a sibling's 8 MB build artifact passed a leak gate,
			// `make check` and eight green checks while carrying 66
			// absolute paths from a maintainer's machine. What can still
			// be said about a binary is whether it belongs in a
			// repository of source at all.
			if why := artifact(data); why != "" {
				problems = append(problems, name+": "+why)
			}
			continue
		}
		scanned++
		for _, found := range findLeaks(allowed(string(data))) {
			problems = append(problems, name+": "+found)
		}
	}
	if scanned < 20 {
		return fmt.Errorf("only %d text files scanned; the scan is not reading the repository", scanned)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	_, err = fmt.Fprintf(w, "leaks ok: %d files scanned\n", scanned)
	return err
}

// leakHistory runs the same rules over every blob and every commit
// message the repository has ever had. The working tree is what a
// contributor sees; the history is what a reader clones. A value deleted
// from the tip is still in the log, so this is the one that has to be
// clean before a repository goes public — and this one already is
// public, which makes it a regression check rather than a release gate.
func leakHistory(w io.Writer) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	messages, err := exec.Command("git", "-C", root, "log", "--format=%H%n%B").Output()
	if err != nil {
		return fmt.Errorf("git log: %w", err)
	}
	var problems []string
	for _, found := range findLeaks(allowed(string(messages))) {
		problems = append(problems, "a commit message: "+found)
	}

	// Every blob the history holds, by hash, deduplicated by git itself.
	blobs, err := exec.Command("git", "-C", root, "rev-list", "--objects", "--all").Output()
	if err != nil {
		return fmt.Errorf("git rev-list: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(blobs)), "\n")
	if len(lines) < 50 {
		return fmt.Errorf("only %d objects; the history scan is not reading the repository", len(lines))
	}
	scanned := 0
	for _, line := range lines {
		hash, path, ok := strings.Cut(line, " ")
		if !ok || path == "" || skipFiles[filepath.Base(path)] {
			continue
		}
		data, err := exec.Command("git", "-C", root, "cat-file", "-p", hash).Output()
		if err != nil || bytes.IndexByte(data, 0) >= 0 {
			continue // a tree, a tag, or a binary: not this rule's business
		}
		scanned++
		for _, found := range findLeaks(allowed(string(data))) {
			problems = append(problems, path+" (blob "+hash[:8]+"): "+found)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	_, err = fmt.Fprintf(w, "leaks history ok: %d blobs and every commit message scanned\n", scanned)
	return err
}

// allowed drops the lines carrying the marker. The planted strings in
// this gate's own tests are the case it exists for — a scanner has to
// contain the shapes it catches — and the marker is per line, so nothing
// else in a file is excused by one use of it.
func allowed(text string) string {
	// Almost no file carries one, and rebuilding every line through a
	// Builder to discover that is this scan's largest avoidable cost.
	if !strings.Contains(text, "leakcheck:"+"allow") {
		return text
	}
	var b strings.Builder
	for line := range strings.Lines(text) {
		if strings.Contains(line, "leakcheck:"+"allow") {
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}

// findLeaks reports everything in text that looks like it identifies a
// tenant. Separate from the walk so the rules can be tested against
// planted strings: a scanner nobody has watched fail might match
// nothing at all.
func findLeaks(text string) []string {
	var out []string
	for _, m := range email.FindAllStringSubmatch(text, -1) {
		if !safeDomain(m[1]) {
			out = append(out, "an address at a real domain: "+m[0])
		}
	}
	for _, m := range favroID.FindAllString(text, -1) {
		if !invented(m) {
			out = append(out, "a 24-character id of the shape Favro mints: "+m+
				" (if it is synthetic, say so in the value: Synthetic, NoSuch, or a run of aaaa)")
		}
	}
	for _, m := range keyedID.FindAllStringSubmatch(text, -1) {
		if !inventedID(m[2]) {
			out = append(out, "an organization id: "+m[1]+" = "+clipValue(m[2]))
		}
	}
	for _, m := range envID.FindAllStringSubmatch(text, -1) {
		if !inventedID(m[1]) {
			out = append(out, "an organization id: FAVRO_ORGANIZATION_ID = "+clipValue(m[1]))
		}
	}
	for _, m := range keyedToken.FindAllStringSubmatch(text, -1) {
		if !invented(m[2]) {
			out = append(out, "an API token: "+m[1]+" = "+clipValue(m[2]))
		}
	}
	for _, m := range envToken.FindAllStringSubmatch(text, -1) {
		if !invented(m[1]) {
			out = append(out, "an API token: FAVRO_API_TOKEN = "+clipValue(m[1]))
		}
	}
	if m := appURL.FindString(text); m != "" {
		out = append(out, "a link into the Favro app, which carries ids in its path: "+m)
	}
	for _, m := range cardRef.FindAllStringSubmatch(text, -1) {
		if _, ok := allowedRefPrefixes[m[1]]; !ok {
			out = append(out, "a card reference: "+m[0]+
				" (a tenant's board prefix; if this is a citation, add the prefix to allowedRefPrefixes with a reason)") // leakcheck:allow
		}
	}
	return out
}

func safeDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	if safeDomains[domain] {
		return true
	}
	for _, s := range safeSuffixes {
		if strings.HasSuffix(domain, s) {
			return true
		}
	}
	return false
}

func invented(id string) bool { return inventedWord.MatchString(id) || hasRun(id, 4) }

// inventedID is the same question asked of a value that is supposed to
// be a Favro id, where one more thing can be said: Favro mints base62,
// so a value carrying a hyphen or an underscore is not one of its ids
// whatever else it looks like. That covers the fixture names this
// repository actually uses — org-1, org-stored — without a list of them.
//
// It is deliberately NOT used for tokens. A credential's character set
// is Favro's business rather than ours, and a rule that waved through
// any token containing a hyphen would be a hole in the one check here
// that guards a secret.
func inventedID(id string) bool {
	return strings.ContainsAny(id, "-_") || invented(id)
}

// hasRun reports whether s repeats one character n times in a row.
func hasRun(s string, n int) bool {
	run := 1
	for i := 1; i < len(s); i++ {
		if s[i] == s[i-1] {
			run++
			if run >= n {
				return true
			}
			continue
		}
		run = 1
	}
	return false
}

// clipValue keeps the value out of the failure message that reports it.
// Naming the variable is enough to find it; printing the value would put
// it in a CI log, which is the place this gate exists to keep clean —
// and that applies to an organization id as much as to a token, which is
// an asymmetry this gate had until a security review pointed at it.
func clipValue(s string) string {
	if len(s) <= 4 {
		return "[redacted]"
	}
	return s[:4] + "… [redacted]"
}

// Magic numbers of a linked executable. `go build ./scripts/gates` drops
// one beside the source under whatever name the directory has, and
// `git add -A` sweeps it in.
var executableMagic = [][]byte{
	[]byte("\x7fELF"),                                  // Linux, BSD
	[]byte("MZ"),                                       // Windows PE
	{0xfe, 0xed, 0xfa, 0xce}, {0xce, 0xfa, 0xed, 0xfe}, // Mach-O, 32-bit
	{0xfe, 0xed, 0xfa, 0xcf}, {0xcf, 0xfa, 0xed, 0xfe}, // Mach-O, 64-bit
	{0xca, 0xfe, 0xba, 0xbe}, // Mach-O universal
}

// maxBinary is what a binary file may weigh before it has to justify
// itself. Nothing binary is tracked in this repository, so this is a
// floor on what may be added rather than a description of what is here.
const maxBinary = 1 << 20

// artifact reports why a binary file does not belong in the repository,
// or "" if it may stay. Deliberately not overridable by a marker
// comment: a binary cannot carry one, and the way past this rule should
// be an edit somebody reviews.
func artifact(data []byte) string {
	for _, magic := range executableMagic {
		if bytes.HasPrefix(data, magic) {
			return fmt.Sprintf("a compiled executable, %d bytes. Nothing built belongs in the tree; "+
				"add it to .gitignore", len(data))
		}
	}
	if len(data) > maxBinary {
		return fmt.Sprintf("%d bytes of binary, past the %d-byte limit. No rule here can read it, "+
			"so it is carried on trust", len(data), maxBinary)
	}
	return ""
}
