// Package redact is the one thing the live driver prints through.
//
// The driver talks to a real Favro organization, so everything it sees
// is exactly what hard rule 1 says may never enter this repository:
// card names, ids, addresses, an organization id in every header. A
// transcript is useful anyway — it is how a person checks what the
// wire contract turned out to be — so the answer is not to print less
// but to print through here.
//
// **Placeholders are stable within a run.** The same card id becomes
// `{card 1}` everywhere it appears, so a transcript still shows that
// the id a list returned is the id a get was called with. That is the
// property a blanket `[REDACTED]` destroys, and following an id across
// three calls is most of what reading a transcript is for.
//
// The `transcript` gate parses the driver and fails if anything reaches
// the terminal except through a Redactor. Without it, redaction is a
// list of call sites somebody remembered to route, and the next print
// added while debugging looks exactly like the safe ones beside it.
//
// **What this cannot redact, and the boundary it draws.** Every shape
// below is anchored on something a tenant's data is and this server's
// values are not — a 24-hex run, an address, a link. A card's *name* is
// none of those: it is ordinary words, and a pattern that caught it
// would catch the rest of the sentence too. That is the same reasoning
// §9 gives for the leak gate, and it has the same consequence here.
//
// So a verbose transcript carries real names. Measured on a run against
// a real organization: 56 KB of output, zero ids, zero addresses, zero
// links — and 54 names. That is acceptable for what this is, a
// maintainer's terminal showing them an organization they already have
// a token for. It is not acceptable in a file, and the leak gate cannot
// catch it, because names are words. **Do not commit a transcript.**
package redact

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// The shapes a Favro tenant's data takes. Each is anchored on something
// this server's own values cannot look like, for the reason §9 gives:
// an organization name is ordinary words, so patterns alone would
// either miss it or redact the whole transcript.
var (
	// favroID is the 24-character hex id Favro mints for most
	// resources. The length is the discriminator — a 24-hex run is not
	// a word anybody writes by accident.
	favroID = regexp.MustCompile(`\b[0-9a-f]{24}\b`)

	// shortID is Favro's other id shape, seen on webhooks and comments:
	// 17 characters of mixed-case base62. Bounded on both sides so it
	// cannot swallow a sentence.
	shortID = regexp.MustCompile(`\b[A-Za-z0-9]{17}\b`)

	// email is deliberately loose: a false positive costs a redacted
	// word in a transcript, and a false negative costs an address.
	email = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

	// appLink is a URL into the Favro app, which carries the
	// organization and often a card in its path.
	appLink = regexp.MustCompile(`https?://(?:www\.)?favro\.com/organization/[^\s"'<>]*`)

	// presigned is an attachment URL: the host is Favro's bucket and
	// the query is a signature that grants access to the file.
	presigned = regexp.MustCompile(`https?://[^\s"'<>]*\.amazonaws\.com/[^\s"'<>]*`)

	// cardRef is the human reference people paste: a board prefix, a
	// hyphen, then a number.
	cardRef = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,9}-\d+\b`)
)

// kind names a family of redacted value, and is what appears in the
// placeholder.
type kind string

const (
	kindID        kind = "id"
	kindShortID   kind = "id"
	kindEmail     kind = "user"
	kindLink      kind = "link"
	kindAttachURL kind = "attachment"
	kindCardRef   kind = "card-ref"
)

// Redactor replaces tenant data with stable placeholders.
//
// Safe for concurrent use: the live driver reads a server's stderr on
// one goroutine while printing results on another, and a transcript
// that interleaved a half-written map would be worse than useless.
type Redactor struct {
	mu     sync.Mutex
	seen   map[string]string // original -> placeholder
	counts map[kind]int

	// extra are values the caller knows are secret and that no pattern
	// would catch — the API token, the organization id, the account's
	// own address.
	extra []string
}

// New returns a Redactor. The values passed are redacted literally
// wherever they appear, whatever they look like: the credentials this
// process holds are the one thing no pattern should be trusted to find.
func New(literal ...string) *Redactor {
	r := &Redactor{seen: map[string]string{}, counts: map[kind]int{}}
	for _, v := range literal {
		if len(strings.TrimSpace(v)) >= 3 {
			r.extra = append(r.extra, v)
		}
	}
	// Longest first, so a token that contains a shorter secret does not
	// leave the remainder of it behind.
	sort.Slice(r.extra, func(i, j int) bool { return len(r.extra[i]) > len(r.extra[j]) })
	return r
}

// String returns s with every tenant-identifying value replaced.
func (r *Redactor) String(s string) string {
	if s == "" {
		return s
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// The caller's own secrets first and literally. A token can contain
	// anything, including a run that looks like an id, so pattern
	// matching afterwards must not get the chance to split it.
	for _, v := range r.extra {
		s = strings.ReplaceAll(s, v, "{credential}")
	}

	// Longest shapes first: an app link contains ids, and an address
	// contains a domain. Replacing the id inside a link first would
	// leave the link half-redacted and still identifying.
	for _, step := range []struct {
		re *regexp.Regexp
		k  kind
	}{
		{appLink, kindLink},
		{presigned, kindAttachURL},
		{email, kindEmail},
		{favroID, kindID},
		{shortID, kindShortID},
		{cardRef, kindCardRef},
	} {
		s = step.re.ReplaceAllStringFunc(s, func(match string) string {
			return r.placeholder(match, step.k)
		})
	}
	return s
}

// Stringf redacts a formatted string. The format is trusted — it is a
// literal in the driver — and the arguments are not.
func (r *Redactor) Stringf(format string, args ...any) string {
	return r.String(fmt.Sprintf(format, args...))
}

// placeholder returns the stable stand-in for one value. Callers hold
// the lock.
func (r *Redactor) placeholder(value string, k kind) string {
	if p, ok := r.seen[value]; ok {
		return p
	}
	r.counts[k]++
	p := fmt.Sprintf("{%s %d}", k, r.counts[k])
	r.seen[value] = p
	return p
}

// Count returns how many distinct values have been redacted. The live
// driver prints it at the end: a run that redacted nothing either
// touched nothing or stopped redacting, and those look identical
// otherwise.
func (r *Redactor) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.seen)
}
