// Command favro-mcp is a Model Context Protocol server for Favro.
//
// Default invocation runs as an MCP server over stdio. Auth-management
// subcommands are exposed for storing credentials in the OS keyring:
//
//	favro-mcp                run as MCP server over stdio
//	favro-mcp --version      print version + commit
//	favro-mcp --dry-run      run server with all writes forced into dry-run
//	favro-mcp --dump-schemas print the tool schemas as JSON and exit
//	favro-mcp auth login     interactive credential capture
//	favro-mcp auth status    show user/org (token never printed)
//	favro-mcp auth logout    delete keyring entries
//	favro-mcp auth which     print where active credentials came from
//
// All logs go to stderr; stdout is reserved for the MCP protocol stream.
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/auth"
	"github.com/mmedum/favro-mcp/internal/favro"
	"github.com/mmedum/favro-mcp/internal/server"
	"github.com/mmedum/favro-mcp/internal/version"
)

// envSkipValidate, when set to a non-empty value, suppresses the
// startup live-validation HTTP call. Lets protocol-only integration
// tests exercise the MCP surface without contacting Favro.
const envSkipValidate = "FAVRO_MCP_SKIP_VALIDATE"

// envLogLevel selects the slog level. Values are case-insensitive:
// debug, info (default), warn, error.
const envLogLevel = "FAVRO_LOG_LEVEL"

// envEnableDestructive registers the delete-style tools. Unset — the
// default — and they are not in tools/list at all.
//
// Off by default because a client-side prompt is not a safety layer:
// a host in an auto-approve permission mode runs a tool annotated
// destructive without asking anyone, and the MCP spec says clients
// treat tool annotations as untrusted. The tool that cannot run
// unattended is the one that was never registered.
const envEnableDestructive = "FAVRO_ENABLE_DESTRUCTIVE"

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		// Errors are already logged via slog; avoid double-printing.
		os.Exit(1)
	}
}

// run is main() lifted into a function for testability. stderr is
// where every diagnostic goes; stdout is reserved for the MCP
// protocol stream when the server is running.
func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	configureLogging(stderr)

	if len(args) > 0 {
		switch args[0] {
		case "auth":
			return runAuth(args[1:], stdin, stderr)
		case "version", "--version", "-V":
			printVersion(stdout)
			return nil
		case "help", "--help", "-h":
			printUsage(stdout)
			return nil
		}
	}

	return runServer(args, stdout, stderr)
}

// configureLogging installs a stderr-bound slog default handler. Level
// comes from FAVRO_LOG_LEVEL; unrecognized values fall back to info
// and emit a warning once the handler is installed.
func configureLogging(stderr io.Writer) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(envLogLevel)))
	level, recognized := parseLogLevel(raw)
	h := slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
	if !recognized {
		// gosec G706 flags os.Getenv values flowing into log output, but
		// env vars are trusted local config in our threat model.
		slog.Warn("unrecognized log level — falling back to info", //nolint:gosec
			"var", envLogLevel,
			"value", raw,
		)
	}
}

// parseLogLevel maps a case-folded env value to a slog.Level. The
// second return is false for unknown values (callers may want to
// surface a warning).
func parseLogLevel(s string) (slog.Level, bool) {
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

// runServer is the default path: resolve credentials, optionally
// validate them live, build the MCP server, and run it over stdio.
// stderr carries usage and flag-parse diagnostics; everything else
// goes through the slog default handler run() already bound to it.
func runServer(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("favro-mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we print our own usage to stderr.
	dryRun := fs.Bool("dry-run", false, "force every mutating tool into dry-run mode regardless of input")
	dumpSchemas := fs.Bool("dump-schemas", false, "print the tool schemas as JSON and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stderr)
			return nil
		}
		errf(stderr, "favro-mcp: %v\n\n", err)
		printUsage(stderr)
		return err
	}

	// Before credentials: the schema dump describes the surface, and the
	// surface does not depend on who is calling. The schema-diff gate
	// builds this binary at an old tag and asks it for its schemas, and
	// that build has no keyring and no environment to read.
	if *dumpSchemas {
		return dumpSchemaSurface(context.Background(), stdout)
	}

	if *dryRun {
		// Wired through Client.ForceDryRun below — every POST/PUT/
		// DELETE/PATCH issued via the Favro client short-circuits and
		// returns a *favro.DryRunRecord. Phase 5 adds the high-level
		// mutating tools that exercise this gate.
		slog.Info("--dry-run set; all mutating Favro requests will short-circuit and return a DryRunRecord")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rt, err := auth.ResolveDefault(ctx)
	if err != nil {
		slog.Error("could not resolve Favro credentials",
			"hint", missingCredsHint(),
			"error", err)
		return err
	}
	// The organization id is deliberately absent. It used to be here,
	// which named the tenant in the first line of every session at the
	// default log level — the kind of thing only a live run shows, and
	// it showed it during A1's live check. favro_ping still returns it,
	// because a tool result goes to the caller who asked; a log goes to
	// whoever ends up holding the file.
	slog.Info("favro-mcp starting",
		"version", version.String(),
		"credential_source", rt.Source,
	)

	if os.Getenv(envSkipValidate) == "" {
		v := auth.DefaultValidator()
		if err := v.Validate(ctx, rt.Token); err != nil {
			slog.Error("Favro credentials rejected at startup", "error", err)
			return err
		}
		slog.Debug("startup credentials validated against Favro")
	} else {
		slog.Warn("FAVRO_MCP_SKIP_VALIDATE is set — startup live validation disabled")
	}

	client := favro.NewClient(rt.Token)
	client.ForceDryRun = *dryRun

	opts := server.Options{Destructive: destructiveEnabled()}
	if opts.Destructive {
		slog.Warn("FAVRO_ENABLE_DESTRUCTIVE is set — delete-style tools are registered and can run unattended")
	}

	srv := server.New(client, rt.Source, version.String(), opts)
	if err := srv.Run(ctx, &mcp.StdioTransport{}); !cleanDisconnect(err) {
		slog.Error("MCP server exited with error", "error", err)
		return err
	}
	slog.Info("favro-mcp shut down cleanly")
	return nil
}

// dumpSchemaSurface writes the whole tool surface to w. The client it
// builds is never used to reach Favro — listing tools touches no
// handler — so an empty token is the right one to pass: asking for
// credentials here would make the gate that reads this need them too.
// Destructive is on here regardless of the environment: this file is
// the record of every schema this binary can serve, and whether a tool
// is registered is a deployment decision rather than a wire one. A dump
// that followed the flag would drop thirteen tools out of the committed
// snapshot, and the schema-diff gate would then stop watching them for
// the breaking changes it exists to catch.
func dumpSchemaSurface(ctx context.Context, stdout io.Writer) error {
	srv := server.New(favro.NewClient(auth.Token{}), "none", version.String(), server.Options{Destructive: true})
	return server.DumpSchemas(ctx, srv, stdout, version.String())
}

// destructiveEnabled reads envEnableDestructive. Anything Go reads as
// false, and anything it cannot read at all, means off: a typo in the
// variable that enables deletes must not enable deletes.
func destructiveEnabled() bool {
	raw := os.Getenv(envEnableDestructive)
	if raw == "" {
		return false
	}
	on, err := strconv.ParseBool(raw)
	if err != nil {
		slog.Warn("ignoring unparseable "+envEnableDestructive+"; delete-style tools stay unregistered",
			"hint", "set it to true or false")
		return false
	}
	return on
}

// cleanDisconnect reports whether the server stopped for an ordinary
// reason: the context was cancelled, or the client went away.
//
// errors.Is(err, io.EOF) does not catch a client going away. The SDK
// reports a closed connection as JSON-RPC -32004 with the EOF only as
// message text, so an unmatched error made this process exit 1 every
// time a host closed the pipe — which every host logs as a crash. The
// code is matched, not the text: `jsonrpc.Error` is a public alias of
// the SDK's wire type, so errors.As reaches it without sniffing a
// string that is free to change.
func cleanDisconnect(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
		return true
	}
	var je *jsonrpc.Error
	if errors.As(err, &je) {
		return je.Code == -32004 || je.Code == -32003
	}
	return false
}

func printVersion(w io.Writer) {
	errf(w, "favro-mcp %s\n", version.String())
}

func printUsage(w io.Writer) {
	errf(w, `favro-mcp — Model Context Protocol server for Favro

Usage:
  favro-mcp                Run as MCP server over stdio.
  favro-mcp --dry-run      Run server; force every mutating tool into dry-run.
  favro-mcp --version      Print version and exit.
  favro-mcp --dump-schemas Print the tool schemas as JSON and exit.
  favro-mcp auth login     Interactively store credentials in the OS keyring.
  favro-mcp auth status    Show the active user / organization (token never printed).
  favro-mcp auth logout    Delete keyring entries.
  favro-mcp auth which     Show whether the active credentials come from env or keyring.

Environment:
  %s          Favro user email (Basic Auth username).
  %s           Favro API token (Basic Auth password).
  %s     Favro organization id; the server is single-org.
  %s           debug | info | warn | error  (default: info)
  %s   When set, skip the startup /organizations ping.
  %s  Set to true to register the delete-style tools (default: off).
`, auth.EnvUserEmail, auth.EnvAPIToken, auth.EnvOrganizationID, envLogLevel, envSkipValidate,
		envEnableDestructive)
}

// missingCredsHint is the canonical "tell the user what to do next"
// string when no Favro credentials resolve. Centralized so cmd/auth.go
// and the runServer error path don't drift apart.
func missingCredsHint() string {
	return "set " + auth.EnvUserEmail + " / " + auth.EnvAPIToken + " / " + auth.EnvOrganizationID + ", or run `favro-mcp auth login`"
}
