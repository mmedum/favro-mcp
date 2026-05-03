// Package version exposes build-time metadata for the favro-mcp binary.
//
// Values are populated at link time via -ldflags "-X" and consumed by the
// auth and server packages so tool responses never need to depend on the
// build system directly.
package version

// These are overridden at build time via -ldflags. See Makefile.
var (
	// Tag is the most recent git tag describing the build, e.g. "v0.1.0".
	Tag = "dev"
	// Commit is the abbreviated git SHA the binary was built from.
	Commit = "unknown"
)

// String returns a "<tag> (<commit>)" representation suitable for
// --version output and slog fields.
func String() string {
	return Tag + " (" + Commit + ")"
}
