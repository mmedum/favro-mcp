package redact

import (
	"strings"
	"sync"
	"testing"
)

// Values shaped like a tenant's and belonging to nobody.
//
// These have to be genuinely 24-hex, because that shape is the thing
// under test: an id that said "NoSuch" in it would not match the
// pattern and every assertion below would pass against a redactor that
// does nothing. The leak gate exempts them by name for the same reason
// its own tests are exempt — a scanner has to contain the shapes it
// catches.
const (
	cardID   = "0f1e2d3c4b5a69788796a5b4" // leakcheck:allow
	otherID  = "1a2b3c4d5e6f708192a3b4c5" // leakcheck:allow
	shortHex = "LpeXFaAwnPw7ynmdr"
	addr     = "someone@example.test"
	token    = "synthetic-api-token-for-the-redaction-test"
	orgID    = "9f8e7d6c5b4a39281726354d" // leakcheck:allow
)

func TestRedactsEveryShape(t *testing.T) {
	t.Parallel()

	r := New()
	cases := map[string]string{
		"a card id":     cardID,
		"a short id":    shortHex,
		"an address":    addr,
		"an app link":   "https://favro.com/organization/" + orgID + "/board",
		"an attachment": "https://favro.s3.eu-central-1.amazonaws.com/" + cardID + ".png?X-Amz-Signature=abc",
		"a card ref":    "BSC-4242",
	}
	for name, value := range cases {
		got := r.String("before " + value + " after")
		if strings.Contains(got, value) {
			t.Errorf("%s survived redaction: %q", name, got)
		}
		if !strings.HasPrefix(got, "before ") || !strings.HasSuffix(got, " after") {
			t.Errorf("%s: the surrounding text must survive: %q", name, got)
		}
	}
}

// TestPlaceholdersAreStable is the property that makes a redacted
// transcript worth reading: following one id across three calls is most
// of what a transcript is for, and a blanket [REDACTED] destroys it.
func TestPlaceholdersAreStable(t *testing.T) {
	t.Parallel()

	r := New()
	first := r.String("listed " + cardID)
	second := r.String("fetched " + cardID)
	third := r.String("fetched " + otherID)

	a := strings.TrimPrefix(first, "listed ")
	b := strings.TrimPrefix(second, "fetched ")
	c := strings.TrimPrefix(third, "fetched ")

	if a != b {
		t.Errorf("the same id must redact to the same placeholder: %q then %q", a, b)
	}
	if a == c {
		t.Errorf("different ids must redact to different placeholders: both %q", a)
	}
}

// TestLiteralsBeatPatterns covers the values no pattern should be
// trusted to find: the credentials this process holds.
func TestLiteralsBeatPatterns(t *testing.T) {
	t.Parallel()

	r := New(token, addr, orgID)
	got := r.String("auth=" + token + " user=" + addr + " org=" + orgID)

	for _, secret := range []string{token, addr, orgID} {
		if strings.Contains(got, secret) {
			t.Errorf("a declared credential survived: %q", got)
		}
	}
	if !strings.Contains(got, "{credential}") {
		t.Errorf("declared credentials should say so: %q", got)
	}
}

// A token that contains a shorter declared secret must not be left
// half-redacted, which is why the literals are sorted longest-first.
func TestLongerLiteralsWin(t *testing.T) {
	t.Parallel()

	r := New("abc", "abcdef123456")
	got := r.String("value=abcdef123456")
	if strings.Contains(got, "def123456") {
		t.Errorf("the longer secret should be replaced whole: %q", got)
	}
}

// An app link contains an id, and redacting the id first would leave a
// half-redacted link that still names the organization.
func TestLongestShapeWins(t *testing.T) {
	t.Parallel()

	r := New()
	got := r.String("open https://favro.com/organization/" + orgID + "/board/" + cardID)
	if strings.Contains(got, "favro.com/organization") {
		t.Errorf("the whole link should go, not the ids inside it: %q", got)
	}
	if strings.Contains(got, orgID) || strings.Contains(got, cardID) {
		t.Errorf("no id may survive inside a link: %q", got)
	}
}

func TestCountReportsWhatItRedacted(t *testing.T) {
	t.Parallel()

	r := New()
	if r.Count() != 0 {
		t.Errorf("a fresh redactor has redacted nothing, got %d", r.Count())
	}
	r.String(cardID + " " + cardID + " " + otherID)
	if got := r.Count(); got != 2 {
		t.Errorf("Count() = %d, want 2 distinct values", got)
	}
}

func TestStringfRedactsTheArguments(t *testing.T) {
	t.Parallel()

	r := New()
	got := r.Stringf("card %s is on widget %s", cardID, otherID)
	if strings.Contains(got, cardID) || strings.Contains(got, otherID) {
		t.Errorf("arguments must be redacted: %q", got)
	}
	if !strings.HasPrefix(got, "card ") {
		t.Errorf("the format is trusted and should survive: %q", got)
	}
}

func TestEmptyAndPlainTextAreUntouched(t *testing.T) {
	t.Parallel()

	r := New()
	if got := r.String(""); got != "" {
		t.Errorf("String(\"\") = %q", got)
	}
	plain := "listed 12 collections on page 1 of 3"
	if got := r.String(plain); got != plain {
		t.Errorf("ordinary prose must survive: %q", got)
	}
}

// The driver reads a server's stderr on one goroutine and prints
// results on another.
func TestConcurrentUse(t *testing.T) {
	t.Parallel()

	r := New(token)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if got := r.String(cardID + " " + token); strings.Contains(got, token) {
					t.Errorf("a credential survived under concurrency: %q", got)
				}
			}
		}()
	}
	wg.Wait()

	if got := r.Count(); got != 1 {
		t.Errorf("Count() = %d, want 1 — the one id, with the credential replaced literally", got)
	}
}

// TestNamesAreNotRedacted pins the boundary the package doc draws, so
// that a reader finds out here rather than by discovering names in a
// transcript.
//
// A card's name is ordinary words. A pattern that caught it would catch
// the sentence around it, which is §9's reasoning for the leak gate and
// has the same consequence for this one.
func TestNamesAreNotRedacted(t *testing.T) {
	t.Parallel()

	r := New()
	const line = `{"name":"Print visitor passes","cardId":"` + cardID + `"}`

	got := r.String(line)
	if strings.Contains(got, cardID) {
		t.Errorf("the id must go: %q", got)
	}
	if !strings.Contains(got, "Print visitor passes") {
		t.Errorf("the name is expected to survive, and the doc says so: %q", got)
	}
}

// Literal exists because every pattern in this package is anchored on a
// shape a tenant's data takes, so a value that IS tenant data and takes
// some other shape passes straight through. These fixtures are
// deliberately not 24 hex characters, for that reason.
func TestLiteralRedactsWhatNoPatternMatches(t *testing.T) {
	r := New()
	r.Literal(KindID, "org-not-hex-shaped")

	got := r.String("bound to org-not-hex-shaped today")
	if strings.Contains(got, "org-not-hex-shaped") {
		t.Errorf("String() = %q; the registered value survived", got)
	}
	if !strings.Contains(got, "{id 1}") {
		t.Errorf("String() = %q; want a kinded placeholder, not a blanket one", got)
	}
}

// The placeholder has to be the same one every time, or a reader cannot
// tell that the id a list returned is the id a get was called with.
func TestLiteralPlaceholderIsStableAndKinded(t *testing.T) {
	r := New()
	r.Literal(KindID, "org-alpha-not-real")
	r.Literal(KindUser, "someone@example.test")

	first := r.String("org-alpha-not-real and someone@example.test")
	second := r.String("org-alpha-not-real again")

	if !strings.Contains(first, "{id 1}") || !strings.Contains(first, "{user 1}") {
		t.Errorf("String() = %q; want both kinds to appear", first)
	}
	if !strings.Contains(second, "{id 1}") {
		t.Errorf("the second call gave %q; the same value must read as the same placeholder", second)
	}
}

// Longest first, or a value that contains a shorter registered one
// leaves the remainder behind.
func TestLiteralReplacesLongestFirst(t *testing.T) {
	r := New()
	r.Literal(KindID, "org-abc")
	r.Literal(KindID, "org-abc-extended")

	got := r.String("org-abc-extended")
	if strings.Contains(got, "org-abc") {
		t.Errorf("String() = %q; a fragment of a registered value survived", got)
	}
}

// Count answers "did redaction happen". A value registered and never
// printed must not inflate it, or the number stops being able to tell
// "redacted nothing" from "stopped redacting" — which is the failure
// this package's own summary line exists to catch.
func TestCountCountsWhatWasReplacedNotWhatWasRegistered(t *testing.T) {
	r := New()
	r.Literal(KindID, "org-appears-here")
	r.Literal(KindID, "org-never-appears")

	r.String("only org-appears-here is in this line")
	if got := r.Count(); got != 1 {
		t.Errorf("Count() = %d after replacing one value; want 1", got)
	}
}

// Registering the same value twice must not mint a second placeholder.
func TestLiteralIgnoresADuplicateRegistration(t *testing.T) {
	r := New()
	r.Literal(KindID, "org-registered-twice")
	r.Literal(KindID, "org-registered-twice")

	got := r.String("org-registered-twice")
	if strings.Count(got, "{id") != 1 {
		t.Errorf("String() = %q; want exactly one placeholder", got)
	}
	if n := r.Count(); n != 1 {
		t.Errorf("Count() = %d; a duplicate registration counted twice", n)
	}
}

// Too short to be meaningful, and short values are where a literal
// replacement does the most damage to the rest of the line.
func TestLiteralIgnoresAShortValue(t *testing.T) {
	r := New()
	r.Literal(KindID, "ab")
	if got := r.String("ab is a word"); got != "ab is a word" {
		t.Errorf("String() = %q; a two-character registration should be ignored", got)
	}
}

// Secrets is the tier for output that carries real ids on purpose: the
// token still goes, and nothing else is touched.
func TestSecretsScrubsTheTokenAndLeavesIdentifiers(t *testing.T) {
	r := New("tok-not-a-real-secret")
	r.Literal(KindID, "org-not-hex-shaped")

	got := r.Secrets("tok-not-a-real-secret reaching org-not-hex-shaped for someone@example.test")
	if strings.Contains(got, "tok-not-a-real-secret") {
		t.Errorf("Secrets() = %q; the token survived", got)
	}
	if !strings.Contains(got, "org-not-hex-shaped") {
		t.Errorf("Secrets() = %q; it must leave identifiers alone — that is what it is for", got)
	}
	if !strings.Contains(got, "someone@example.test") {
		t.Errorf("Secrets() = %q; it applied a pattern, which is the other tier's job", got)
	}
}
