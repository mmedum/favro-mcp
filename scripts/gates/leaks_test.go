package main

import (
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
		{"a reserved documentation domain", "user@example.test and user@example.com"},             // leakcheck:allow
		{"a Go identifier used as a value", `{"organization_id": smokeOrgID}`},                    // leakcheck:allow
		{"a hyphenated fixture id", `OrganizationID: "org-stored"`},                               // leakcheck:allow
		{"a synthetic id that says so", `"cardId": "synthetic0000000000000001"`},                  // leakcheck:allow
		{"a standards citation", "ISO-8601 and GO-2026-4971 and BSD-3-Clause"},                    // leakcheck:allow
		{"a git SHA", "pinned at 3d3c42e5aac5ba805825da76410c181273ba90b1"},                       // leakcheck:allow
		{"the bare API host", "https://favro.com/api/v1/cards"},                                   // leakcheck:allow
		{"a marked line", "FAVRO_API_TOKEN=b2c3d4e5f6a7b8c9d0e1f2a3b4c5 // " + "leakcheck:allow"}, // gitleaks:allow
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
	out, err := exec.Command("git", "-C", root, "grep", "-l", "-E",
		`\b(`+strings.Join(prefixes(), "|")+`)-[0-9]`).Output()
	if err != nil {
		t.Fatalf("git grep: %v", err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		t.Fatal("git grep found no citations at all; this check is not reading the repository")
	}
	for prefix := range allowedRefPrefixes {
		if err := exec.Command("git", "-C", root, "grep", "-q", "-E",
			`\b`+prefix+`-[0-9]`).Run(); err != nil {
			t.Errorf("allowedRefPrefixes has %q (%s) and nothing in the repository uses it; "+
				"delete the entry rather than leaving a hole open",
				prefix, allowedRefPrefixes[prefix])
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
	if !strings.Contains(out.String(), "files scanned") {
		t.Errorf("the gate must say how much it read, got %q", out.String())
	}
}

func TestLeaksRejectsAnArgumentItDoesNotKnow(t *testing.T) {
	if err := leaks(os.Stdout, []string{"everything"}); err == nil {
		t.Error("an unknown argument should not be silently ignored")
	}
}
