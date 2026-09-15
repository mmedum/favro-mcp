//go:build evals

package main

import (
	"bufio"
	"os"

	"github.com/mmedum/favro-mcp/internal/redact"
)

// out is the only thing in this program that writes to the terminal.
//
// The live driver has had this since A6 and this harness shipped
// without it, which the `transcript` gate did not catch because it read
// one hard-coded directory. Both drivers talk to a real organization,
// so both need it: a failing task prints the model's whole trace, and
// every argument in it is an id the model resolved from live data. A
// trace pasted into an issue on a public repository is the disclosure
// path this exists to close.
//
// It does not close all of it. The redactor removes ids, addresses and
// the credentials this process holds; a card NAME is ordinary words and
// survives. Do not commit a transcript.
var out = newPrinter(redact.New(
	os.Getenv("FAVRO_USER_EMAIL"),
	os.Getenv("FAVRO_API_TOKEN"),
	os.Getenv("FAVRO_ORGANIZATION_ID"),
))

type printer struct {
	red *redact.Redactor
	w   *bufio.Writer
	err *bufio.Writer
}

func newPrinter(red *redact.Redactor) *printer {
	return &printer{
		red: red,
		w:   bufio.NewWriter(os.Stdout),
		err: bufio.NewWriter(os.Stderr),
	}
}

// line prints one redacted line to stdout.
func (p *printer) line(format string, args ...any) {
	_, _ = p.w.WriteString(p.red.Stringf(format, args...))
	_ = p.w.WriteByte('\n')
	_ = p.w.Flush()
}

// fail prints one redacted line to stderr.
func (p *printer) fail(format string, args ...any) {
	_, _ = p.err.WriteString(p.red.Stringf(format, args...))
	_ = p.err.WriteByte('\n')
	_ = p.err.Flush()
}

// redacted renders one value, for a caller that builds its own string.
func (p *printer) redacted(s string) string { return p.red.String(s) }
