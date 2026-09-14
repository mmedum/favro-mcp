// Package version exposes build-time metadata for the favro-mcp binary.
//
// Values are populated at link time via -ldflags "-X" and consumed by the
// auth and server packages so tool responses never need to depend on the
// build system directly.
package version

import (
	"runtime/debug"
	"strings"
	"sync"
)

// These are overridden at build time via -ldflags. See Makefile.
var (
	// Tag is the most recent git tag describing the build, e.g. "v0.1.0".
	Tag = "dev"
	// Commit is the abbreviated git SHA the binary was built from.
	Commit = "unknown"
)

// unstamped is what Tag says when no -ldflags reached the build, which
// is every `go install` — the documented way to install this server.
const unstamped = "dev"

// String returns a "<tag> (<commit>)" representation suitable for
// --version output and slog fields.
func String() string {
	s := stamp()
	return s.tag + " (" + s.commit + ")"
}

// Source names where String's answer came from: the linker, the module
// Go recorded at install time, or neither. `doctor` prints it, because
// "dev (unknown)" and a real version are the same sentence to a user
// trying to say which build they are running.
func Source() string { return stamp().source }

// Module is the import path this binary was built from, when Go
// recorded one. The issue form asks for it: a `go install` of a fork
// produces a binary indistinguishable from this one otherwise.
func Module() string { return stamp().module }

type buildStamp struct{ tag, commit, source, module string }

// stamp is computed once. Not because this is a hot path — it is read a
// handful of times at startup and once per `doctor` run — but because
// debug.ReadBuildInfo walks a table embedded in the binary on every
// call, and there is no reason for three exported readers to walk it
// three times when sync.OnceValue costs nothing.
var stamp = sync.OnceValue(func() buildStamp {
	// The second return is whether build info is available at all; nil
	// carries that into stampFrom, which has to handle it anyway.
	info, _ := debug.ReadBuildInfo()
	return stampFrom(Tag, Commit, info)
})

// stampFrom implements the fallback the shared standard's §10 names
// first: `go install` applies no ldflags, so a binary installed the
// documented way reports "dev (unknown)" and every bug report from one
// is unattributable to a build. Go records the module version and the
// VCS revision itself, and both are available without a build system.
//
// The linker wins when it spoke. A release binary carries the tag
// goreleaser stamped, and that string is the one five other places in a
// release agree about.
//
// Its two inputs are passed in rather than read from the package and the
// runtime, so every branch below can be tested against a build this test
// binary is not.
func stampFrom(tag, commit string, info *debug.BuildInfo) buildStamp {
	if tag != unstamped {
		return buildStamp{tag, commit, "ldflags", moduleFrom(info)}
	}
	if info == nil {
		return buildStamp{tag, commit, "unstamped", moduleFrom(info)}
	}

	// "(devel)" is what a build from a local tree reports, which is no
	// more informative than "dev" — leave the placeholder rather than
	// trade it for a different one.
	if v := info.Main.Version; v != "" && v != "(devel)" {
		tag = v
	}
	if revision, modified := vcsStamp(info); revision != "" {
		commit = revision
		if modified {
			commit += "-dirty"
		}
	}
	source := "buildinfo"
	if tag == unstamped && commit == "unknown" {
		source = "unstamped"
	}
	return buildStamp{tag, commit, source, moduleFrom(info)}
}

// vcsStamp is the revision Go recorded and whether the tree was dirty,
// or "" when it recorded neither. Abbreviated to match what the linker
// stamps, so the two paths print the same shape.
func vcsStamp(info *debug.BuildInfo) (revision string, modified bool) {
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
			if len(revision) > 7 {
				revision = revision[:7]
			}
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	return revision, modified
}

func moduleFrom(info *debug.BuildInfo) string {
	if info == nil || info.Main.Path == "" {
		return "unknown"
	}
	return strings.TrimSuffix(info.Main.Path, "/")
}
