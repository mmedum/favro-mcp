package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Every rule, watched failing against a planted value. A scanner that
// has never matched anything might match nothing at all, and its output
// would look the same either way.
func TestFindLeaksCatchesEachShape(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"an address at a real domain", "contact: someone@a-real-company.com", "address at a real domain"},               // leakcheck:allow
		{"a 24-hex Favro id", `{"cardId": "67973f72db34592d8fc96c48"}`, "24-character id"},                               // leakcheck:allow
		{"an organization id by keyword", `"organizationId": "zk4CJpg5uozhL4R2W"`, "organization id"},                    // leakcheck:allow
		{"an organization id in the environment", "FAVRO_ORGANIZATION_ID=zk4CJpg5uozhL4R2W", "organization id"},          // leakcheck:allow
		{"a token by keyword", `"favro_api_token": "b2c3d4e5f6a7b8c9d0e1f2a3b4c5"`, "API token"},                         // leakcheck:allow gitleaks:allow
		{"a token in the environment", "FAVRO_API_TOKEN=b2c3d4e5f6a7b8c9d0e1f2a3b4c5", "API token"},                      // leakcheck:allow gitleaks:allow
		{"a link into the app", "see https://favro.com/organization/zk4CJpg5uozhL4R2W/board", "link into the Favro app"}, // leakcheck:allow
		{"a card reference", "fixed in ZZQ-4182 yesterday", "card reference"},                                            // leakcheck:allow
		{"a reserved name under a real country code", "someone@example.dk", "address at a real domain"},                  // leakcheck:allow
		{"a reserved label inside a real domain", "someone@corp.test.com", "address at a real domain"},                   // leakcheck:allow
		{"a real domain ending in a reserved name", "someone@notexample.com", "address at a real domain"},                // leakcheck:allow
		{"another address at Anthropic's domain", "ask support@anthropic.com", "address at a real domain"},               // leakcheck:allow
		{"another address at GitHub's domain", "mail someone@github.com", "address at a real domain"},                    // leakcheck:allow
		{"a historic fixture address in the working tree", `Email: "u@e.com"`, "address at a real domain"},               // leakcheck:allow
	} {
		t.Run(tc.name, func(t *testing.T) {
			found := findLeaks(tc.text)
			if len(found) == 0 {
				t.Fatalf("findLeaks(%q) found nothing; the rule for %s is not firing", tc.text, tc.want)
			}
			if !strings.Contains(strings.Join(found, "\n"), tc.want) {
				t.Errorf("findLeaks(%q) = %v, want something mentioning %q", tc.text, found, tc.want)
			}
		})
	}
}

// The other direction, which is what stops the gate being reverted by
// the first person it annoys. Each of these was a real finding from the
// first run over this repository.
func TestFindLeaksLeavesTheRepositoryAlone(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"a reserved documentation domain", "user@example.test and user@example.com"},                          // leakcheck:allow
		{"a Go identifier used as a value", `{"organization_id": smokeOrgID}`},                                 // leakcheck:allow
		{"a hyphenated fixture id", `OrganizationID: "org-stored"`},                                            // leakcheck:allow
		{"a synthetic id that says so", `"cardId": "synthetic0000000000000001"`},                               // leakcheck:allow
		{"a standards citation", "ISO-8601 and GO-2026-4971 and BSD-3-Clause"},                                 // leakcheck:allow
		{"a git SHA", "pinned at 3d3c42e5aac5ba805825da76410c181273ba90b1"},                                    // leakcheck:allow
		{"the bare API host", "https://favro.com/api/v1/cards"},                                                // leakcheck:allow
		{"a marked line", "FAVRO_API_TOKEN=b2c3d4e5f6a7b8c9d0e1f2a3b4c5 // " + "leakcheck:allow"},              // gitleaks:allow
		{"reserved names, any case and depth", "a@mail.invalid b@host.localhost c@corp.example D@EXAMPLE.ORG"}, // leakcheck:allow
		{"a synthetic id made of a run", `"cardId": "aaaa0000000000000000bbbb"`},                               // leakcheck:allow
		{"an invented organization id in the environment", "FAVRO_ORGANIZATION_ID=org-fixture-1"},              // leakcheck:allow
		{"an invented token by keyword", `"api_token": "fixture-token-0123456789"`},                            // leakcheck:allow
		{"an invented token in the environment", "FAVRO_API_TOKEN=placeholder-0123456789"},                     // leakcheck:allow
		{"the co-author trailer", "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"},
		{"Dependabot's sign-off", "Signed-off-by: dependabot[bot] <support@github.com>"},
		{"a checksum algorithm", "`checksums.txt` (SHA-256)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if found := findLeaks(allowed(tc.text)); len(found) > 0 {
				t.Errorf("findLeaks(%q) = %v; a false positive here is how a gate gets ignored", tc.text, found)
			}
		})
	}
}

// An exemption naming something the repository does not contain reads as
// a considered decision and exempts nothing. This is what deletes a
// stale one.
func TestEveryAllowedPrefixIsUsed(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	// Without `\b`, and matched in Go afterwards. git's -E is POSIX ERE
	// on macOS, where `\b` is a GNU extension that matches nothing — so
	// this found no files, git exited 1 for "no matches", and the test
	// reported the repository as unreadable. It passed on Linux.
	out, err := exec.Command("git", "-C", root, "grep", "-l", "-E",
		`(`+strings.Join(prefixes(), "|")+`)-[0-9]`).Output()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Fatalf("git grep: %v", err)
		}
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		t.Fatal("git grep found no citations at all; this check is not reading the repository")
	}
	for prefix := range allowedRefPrefixes {
		// No `\b`: POSIX ERE has no such escape, so on macOS this matched
		// nothing and every prefix looked unused.
		if err := exec.Command("git", "-C", root, "grep", "-q", "-E",
			prefix+`-[0-9]`).Run(); err != nil {
			t.Errorf("allowedRefPrefixes has %q (%s) and nothing in the repository uses it; "+
				"delete the entry rather than leaving a hole open",
				prefix, allowedRefPrefixes[prefix])
		}
	}
}

// The history scan reads a tag from its message down, and a blob whole.
func TestHistoryReadsATagFromItsMessage(t *testing.T) {
	header := "object " + strings.Repeat("ab", 20) + "\ntype commit\ntag v1.0.0\n" +
		"tagger Someone <someone@a-real-company.com> 1792876206 +0200\n\n" // leakcheck:allow
	for _, tc := range []struct {
		name, kind, text string
		want             int
	}{
		{"the tagger header", "tag", header + "v1.0.0\n", 0},
		{"an address in the tag message", "tag", header + "for someone@a-real-company.com\n", 1}, // leakcheck:allow
		{"the same bytes as a blob", "blob", header + "v1.0.0\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := objectFindings(tc.kind, "v1.0.0", []byte(tc.text)); len(got) != tc.want {
				t.Errorf("objectFindings(%q, %q) = %v, want %d finding(s)", tc.kind, tc.text, got, tc.want)
			}
		})
	}
}

// The old auth tests' fixture addresses pass in a blob of an auth test in
// the history and nowhere else; the tree's case is in
// TestFindLeaksCatchesEachShape.
func TestHistoricFixtureAddressesStayInTheirFiles(t *testing.T) {
	for _, tc := range []struct {
		name, path, text string
		want             int
	}{
		{"the fixtures in an old auth test", "internal/auth/token_test.go", `Email: "u@e.com", Bad: "u\r@e.com"`, 0}, // leakcheck:allow
		{"another address at the fixture domain", "internal/auth/token_test.go", `Email: "x@e.com"`, 1},              // leakcheck:allow
		{"a fixture outside the auth tests", "internal/service/card_test.go", `Email: "u@e.com"`, 1},                 // leakcheck:allow
		{"a fixture in auth code that is not a test", "internal/auth/token.go", `Email: "u@e.com"`, 1},               // leakcheck:allow
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := objectFindings("blob", tc.path, []byte(tc.text)); len(got) != tc.want {
				t.Errorf("objectFindings(blob, %q, %q) = %v, want %d finding(s)", tc.path, tc.text, got, tc.want)
			}
		})
	}
}

// A known finding is accepted in the commit it is pinned to, by count,
// and nowhere else.
func TestKnownFindingIsPinnedToItsCommit(t *testing.T) {
	const pinned = "af7cecf1fe538390910d0417a1d9827d6b4c652f"
	const other = "1234567890abcdef1234567890abcdef12345678"
	trailer := "Co-Authored-By: Someone <someone@a-real-company.com>\n" // leakcheck:allow
	for _, tc := range []struct {
		name, hash, message string
		want                int
	}{
		{"the pinned commit", pinned, "Fix it\n\n" + trailer, 0},
		{"another commit with the same trailer", other, "Fix it\n\n" + trailer, 1},
		{"the pinned commit with one more than it carries", pinned, "Fix it\n\n" + trailer + trailer, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := commitFindings(tc.hash, tc.message); len(got) != tc.want {
				t.Errorf("commitFindings(%s, %q) = %v, want %d finding(s)", tc.hash[:8], tc.message, got, tc.want)
			}
		})
	}
	if got := objectFindings("blob", "docs/notes.md", []byte(trailer)); len(got) != 1 {
		t.Errorf("the same trailer in a blob: %v, want 1 finding", got)
	}
}

// A pin that accepts nothing is a hole somebody forgot to close. Each is
// held to the commit it names, by count, so a failure prints no value.
func TestEveryKnownFindingIsInTheHistory(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range knownFindings {
		out, err := exec.Command("git", "-C", root, "log", "-1", "--format=%B", k.commit).Output()
		if err != nil {
			// CI's test job checks out one commit.
			shallow, _ := exec.Command("git", "-C", root, "rev-parse", "--is-shallow-repository").Output()
			if strings.TrimSpace(string(shallow)) == "true" {
				t.Skipf("a shallow clone does not hold %s", k.commit[:8])
			}
			t.Fatalf("knownFindings pins %s, which the history does not hold: %v", k.commit[:8], err)
		}
		n := 0
		for _, f := range findLeaks(allowed(string(out))) {
			if strings.HasPrefix(f, k.rule+": ") {
				n++
			}
		}
		if n != k.count {
			t.Errorf("%s carries %d finding(s) of %q and the pin says %d", k.commit[:8], n, k.rule, k.count)
		}
		if left := len(commitFindings(k.commit, string(out))); left != 0 {
			t.Errorf("%s still reports %d finding(s) with its pin applied", k.commit[:8], left)
		}
	}
}

func prefixes() []string {
	var out []string
	for p := range allowedRefPrefixes {
		out = append(out, p)
	}
	return out
}

func TestArtifactRejectsWhatDoesNotBelong(t *testing.T) {
	if why := artifact([]byte("\x7fELF\x02\x01\x01")); why == "" {
		t.Error("an ELF binary should never be in a repository of source")
	} else if !strings.Contains(why, "gitignore") {
		t.Errorf("the message should say what to do about it, got %q", why)
	}
	big := make([]byte, maxBinary+1)
	if why := artifact(big); why == "" {
		t.Error("a binary past the size limit is carried on trust and should be refused")
	}
	if why := artifact([]byte{0x00, 0x01, 0x02}); why != "" {
		t.Errorf("a small binary fixture may stay, got %q", why)
	}
}

// The floor. A scan that reads nothing reports exactly what a clean
// repository reports.
func TestLeaksReadsTheRepository(t *testing.T) {
	var out strings.Builder
	if err := leaks(&out, nil); err != nil {
		t.Fatalf("the repository should be clean: %v", err)
	}
	var scanned int
	if _, err := fmt.Sscanf(out.String(), "leaks ok: %d files scanned", &scanned); err != nil {
		t.Fatalf("the gate must say how much it read, got %q: %v", out.String(), err)
	}
	// The repository held 298 text files on 2026-10-01; the floor sits
	// well below that so it fails on a scan that stopped reading, not
	// on a cleanup.
	if scanned < 200 {
		t.Errorf("leaks scanned %d files, want at least 200", scanned)
	}
}

func TestLeaksRejectsAnArgumentItDoesNotKnow(t *testing.T) {
	if err := leaks(os.Stdout, []string{"everything"}); err == nil {
		t.Error("an unknown argument should not be silently ignored")
	}
}
