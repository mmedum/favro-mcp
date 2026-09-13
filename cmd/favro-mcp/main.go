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
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/auth"
	"github.com/mmedum/favro-mcp/internal/config"
	"github.com/mmedum/favro-mcp/internal/favroapi"
	"github.com/mmedum/favro-mcp/internal/server"
	"github.com/mmedum/favro-mcp/internal/version"
)

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
	cfg := configureLogging(stderr)

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

	return runServer(args, cfg, stdout, stderr)
}

// configureLogging installs a stderr-bound slog default handler and
// returns the settings it read on the way.
//
// The order is the whole reason this returns a Config rather than
// logging what it found: the log level is one of the settings, so
// anything config.Load could not read has to be warned about after the
// handler exists, not while it is being chosen.
func configureLogging(stderr io.Writer) config.Config {
	cfg := config.Load()
	slog.SetDefault(slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: cfg.LogLevel})))
	for _, w := range cfg.Warnings {
		// gosec G706 flags env values flowing into log output, but env
		// vars are trusted local config in our threat model.
		slog.Warn(w) //nolint:gosec
	}
	return cfg
}

// runServer is the default path: resolve credentials, optionally
// validate them live, build the MCP server, and run it over stdio.
// stderr carries usage and flag-parse diagnostics; everything else
// goes through the slog default handler run() already bound to it.
func runServer(args []string, cfg config.Config, stdout io.Writer, stderr io.Writer) error {
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
		// returns a *favroapi.DryRunRecord. Phase 5 adds the high-level
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

	if !cfg.SkipValidate {
		v := auth.DefaultValidator()
		if err := v.Validate(ctx, rt.Token); err != nil {
			slog.Error("Favro credentials rejected at startup", "error", err)
			return err
		}
		slog.Debug("startup credentials validated against Favro")
	} else {
		slog.Warn(config.EnvSkipValidate + " is set — startup live validation disabled")
	}

	client := favroapi.NewClient(rt.Token)
	client.ForceDryRun = *dryRun

	opts := server.Options{
		Destructive:      cfg.Destructive,
		CredentialSource: rt.Source,
		Version:          version.String(),
	}
	if opts.Destructive {
		slog.Warn(config.EnvEnableDestructive + " is set — delete-style tools are registered and can run unattended")
	}

	srv := server.New(client, opts)
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
	srv := server.New(favroapi.NewClient(auth.Token{}), server.Options{
		Destructive:      true,
		CredentialSource: "none",
		Version:          version.String(),
	})
	return server.DumpSchemas(ctx, srv, stdout, version.String())
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
`, auth.EnvUserEmail, auth.EnvAPIToken, auth.EnvOrganizationID,
		config.EnvLogLevel, config.EnvSkipValidate, config.EnvEnableDestructive)
}

// missingCredsHint is the canonical "tell the user what to do next"
// string when no Favro credentials resolve. Centralized so cmd/auth.go
// and the runServer error path don't drift apart.
func missingCredsHint() string {
	return "set " + auth.EnvUserEmail + " / " + auth.EnvAPIToken + " / " + auth.EnvOrganizationID + ", or run `favro-mcp auth login`"
}
