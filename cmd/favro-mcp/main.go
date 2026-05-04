// Command favro-mcp is a Model Context Protocol server for Favro.
//
// Default invocation runs as an MCP server over stdio. Auth-management
// subcommands are exposed for storing credentials in the OS keyring:
//
//	favro-mcp                run as MCP server over stdio
//	favro-mcp --version      print version + commit
//	favro-mcp --dry-run      run server with all writes forced into dry-run
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
	"strings"
	"syscall"

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

	return runServer(args)
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
func runServer(args []string) error {
	fs := flag.NewFlagSet("favro-mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we print our own usage to stderr.
	dryRun := fs.Bool("dry-run", false, "force every mutating tool into dry-run mode regardless of input")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(os.Stderr)
			return nil
		}
		errf(os.Stderr, "favro-mcp: %v\n\n", err)
		printUsage(os.Stderr)
		return err
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
	slog.Info("favro-mcp starting",
		"version", version.String(),
		"credential_source", rt.Source,
		"organization_id", rt.Token.OrganizationID,
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

	srv := server.New(client, rt.Source, version.String())
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("MCP server exited with error", "error", err)
		return err
	}
	slog.Info("favro-mcp shut down cleanly")
	return nil
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
`, auth.EnvUserEmail, auth.EnvAPIToken, auth.EnvOrganizationID, envLogLevel, envSkipValidate)
}

// missingCredsHint is the canonical "tell the user what to do next"
// string when no Favro credentials resolve. Centralized so cmd/auth.go
// and the runServer error path don't drift apart.
func missingCredsHint() string {
	return "set " + auth.EnvUserEmail + " / " + auth.EnvAPIToken + " / " + auth.EnvOrganizationID + ", or run `favro-mcp auth login`"
}
