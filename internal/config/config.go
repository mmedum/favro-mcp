// Package config is every FAVRO_* setting the process reads, resolved
// once at startup and validated before anything uses it.
//
// Credentials are not here: those resolve through internal/auth, which
// checks the environment and then the OS keyring and reports which one
// won. What is here is the settings that change how the server behaves
// rather than who it runs as — and every one of them is read in exactly
// one place, so "what does this variable do" has a single answer.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Environment variable names. Exported so that --help, the README
// staleness gate and the tests all name the same strings rather than
// three copies of them.
const (
	// EnvLogLevel selects the slog level: debug, info (default), warn,
	// error. Case-insensitive.
	EnvLogLevel = "FAVRO_LOG_LEVEL"

	// EnvSkipValidate, set to anything non-empty, suppresses the
	// startup live-validation call. Lets protocol-only integration
	// tests exercise the MCP surface without contacting Favro.
	EnvSkipValidate = "FAVRO_MCP_SKIP_VALIDATE"

	// EnvEnableDestructive registers the delete-style tools. Unset —
	// the default — and they are not in tools/list at all.
	//
	// Off by default because a client-side prompt is not a safety
	// layer: a host in an auto-approve permission mode runs a tool
	// annotated destructive without asking anyone, and the MCP spec
	// says clients treat tool annotations as untrusted. The tool that
	// cannot run unattended is the one that was never registered.
	EnvEnableDestructive = "FAVRO_ENABLE_DESTRUCTIVE"

	// EnvUploadDir is the one directory the upload tools read files
	// from. Unset — the default — and they are not registered, because
	// an upload is a file leaving this computer and a path is something
	// a card's text can suggest to a model.
	EnvUploadDir = "FAVRO_UPLOAD_DIR"

	// EnvRequirePrompt was removed with the question it governed: the
	// server no longer asks the person, and the client's own approval
	// decides each call. It is read only to refuse a configuration that
	// still turns it on (readRemoved).
	EnvRequirePrompt = "FAVRO_REQUIRE_PROMPT"

	// EnvInteractionHint, set to false, took Claude Code's
	// requiresUserInteraction mark off the writes the server asked
	// about. It was removed in the same release, and is read only to
	// warn a configuration that still turns the mark off.
	EnvInteractionHint = "FAVRO_INTERACTION_HINT"
)

// Config is the resolved settings.
type Config struct {
	// LogLevel is the slog level to install. Info when the variable
	// held something this package could not read.
	LogLevel slog.Level

	// SkipValidate suppresses the startup credential check.
	SkipValidate bool

	// Destructive registers the delete-style tools.
	Destructive bool

	// UploadDir is the absolute, existing directory uploads are read
	// from, or empty when uploads are off.
	UploadDir string

	// Refusals are the removed settings this environment still turns on,
	// as sentences ready to log. The server does not start while there
	// is one: ignored, a setting would quietly allow what its owner set
	// it to stop.
	Refusals []string

	// Warnings is what Load could not read, as sentences ready to log.
	//
	// Carried rather than logged because Load runs before the logger it
	// would log through exists — the log level is one of the things it
	// resolves. Empty when the environment was well-formed.
	Warnings []string
}

// Load reads the environment. It never fails: a value this package
// cannot read falls back to the safe default and is described in
// Warnings, so the caller can say so once a logger exists. A removed
// setting still turned on is described in Refusals, and the server
// refuses to start on it; the other commands still run.
//
// Nothing here reads a flag, because the flags that exist (--dry-run,
// --version, --dump-schemas) are a different kind of thing: they select
// what the process does rather than configure how it does it, and
// cmd/favro-mcp parses them where it dispatches on them.
func Load() Config {
	cfg := Config{}

	raw := strings.ToLower(strings.TrimSpace(os.Getenv(EnvLogLevel)))
	level, recognized := ParseLogLevel(raw)
	cfg.LogLevel = level
	if !recognized {
		cfg.Warnings = append(cfg.Warnings,
			fmt.Sprintf("unrecognized %s %q — falling back to info", EnvLogLevel, raw))
	}

	cfg.SkipValidate = os.Getenv(EnvSkipValidate) != ""

	// Anything Go cannot read as a bool, and anything absent, means
	// off: a typo in the variable that enables deletes must not enable
	// deletes.
	if raw := os.Getenv(EnvEnableDestructive); raw != "" {
		on, err := strconv.ParseBool(raw)
		switch {
		case err != nil:
			cfg.Warnings = append(cfg.Warnings,
				fmt.Sprintf("ignoring unreadable %s %q — delete-style tools stay unregistered; set it to true or false",
					EnvEnableDestructive, raw))
		default:
			cfg.Destructive = on
		}
	}

	cfg.readRemoved()

	if raw := strings.TrimSpace(os.Getenv(EnvUploadDir)); raw != "" {
		dir, err := UploadDir(raw)
		switch {
		case err != nil:
			cfg.Warnings = append(cfg.Warnings,
				fmt.Sprintf("ignoring %s: %v — the upload tools stay unregistered", EnvUploadDir, err))
		default:
			cfg.UploadDir = dir
		}
	}

	return cfg
}

// readRemoved describes the settings a release took out that the
// environment still sets: in Refusals one that turned a guard on, so the
// server does not start, and in Warnings one whose loss only makes the
// server stricter.
func (cfg *Config) readRemoved() {
	// A removed setting set to anything but false is refused: a value
	// that is not a boolean may mean on.
	if raw := strings.TrimSpace(os.Getenv(EnvRequirePrompt)); raw != "" {
		if on, err := strconv.ParseBool(raw); err != nil || on {
			cfg.Refusals = append(cfg.Refusals, EnvRequirePrompt+" was removed: the server no longer asks the "+
				"person, and the client's own approval decides each call. Unset it to start the server. Leave "+
				EnvEnableDestructive+" off for a client that runs with nobody watching, and list the writes you "+
				"want to approve under your client's ask rules, as the README shows")
		}
	}

	// FAVRO_INTERACTION_HINT=false took the mark off. Ignoring it only
	// makes the server stricter, so the server starts, but a deployment
	// that relied on it is told why Claude Code now refuses those calls
	// where it cannot prompt. true was the default, and changes nothing.
	if raw := strings.TrimSpace(os.Getenv(EnvInteractionHint)); raw != "" {
		if on, err := strconv.ParseBool(raw); err != nil || !on {
			cfg.Warnings = append(cfg.Warnings, EnvInteractionHint+" was removed and is ignored: the widest "+
				"destructive tools always carry Claude Code's requiresUserInteraction mark, so Claude Code prompts "+
				"on every call of them and refuses them where it cannot prompt, as in `claude -p`. Unset it")
		}
	}
}

// UploadDir checks a FAVRO_UPLOAD_DIR value: an absolute path to a
// directory that exists. A relative one would mean whatever directory
// the host happened to start the server in.
func UploadDir(raw string) (string, error) {
	if !filepath.IsAbs(raw) {
		return "", fmt.Errorf("%q is not an absolute path", raw)
	}
	dir := filepath.Clean(raw)
	info, err := os.Stat(dir) //nolint:gosec // G703: the person names this directory; it is checked absolute and cleaned above
	switch {
	case err != nil:
		return "", fmt.Errorf("%q cannot be read: %w", dir, err)
	case !info.IsDir():
		return "", fmt.Errorf("%q is not a directory", dir)
	}
	return dir, nil
}

// ParseLogLevel maps a case-folded value to a slog.Level. The second
// return is false for values it does not recognize; the level is info
// in that case.
func ParseLogLevel(s string) (slog.Level, bool) {
	switch s {
	case "", "info":
		return slog.LevelInfo, true
	case "debug":
		return slog.LevelDebug, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}
