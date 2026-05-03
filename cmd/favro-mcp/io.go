package main

import (
	"fmt"
	"io"
)

// errf and errln are convenience wrappers around fmt.Fprintf / fmt.Fprintln
// that drop the (n int, err error) return. Diagnostic and CLI output is
// best-effort: if writing to stderr or stdout fails (closed pipe, full
// disk), there's no useful recovery — abandoning the message is the
// least-bad outcome. Centralizing the discards in one place also lets the
// linter see a single suppression-by-design rather than a sprinkle of
// `_, _ =` across every print site.
func errf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func errln(w io.Writer, args ...any) {
	_, _ = fmt.Fprintln(w, args...)
}

func errprint(w io.Writer, s string) {
	_, _ = fmt.Fprint(w, s)
}
