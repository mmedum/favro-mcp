package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/mmedum/favro-mcp/internal/auth"
)

// runAuth dispatches the `auth <subcommand>` subtree. stderr carries
// every prompt and diagnostic so stdout stays reserved for the MCP
// protocol (these subcommands don't run the server, but the discipline
// is consistent across the binary).
func runAuth(args []string, stdin io.Reader, stderr io.Writer) error {
	if len(args) == 0 {
		return authUsage(stderr)
	}
	ctx := context.Background()

	switch args[0] {
	case "login":
		return authLogin(ctx, stdin, stderr)
	case "status":
		return authStatus(ctx, stderr)
	case "logout":
		return authLogout(ctx, stderr)
	case "which":
		return authWhich(ctx, stderr)
	case "help", "--help", "-h":
		return authUsage(stderr)
	default:
		errf(stderr, "favro-mcp auth: unknown subcommand %q\n\n", args[0])
		_ = authUsage(stderr)
		return fmt.Errorf("unknown auth subcommand: %s", args[0])
	}
}

func authUsage(w io.Writer) error {
	errprint(w, `favro-mcp auth — manage Favro credentials in the OS keyring

Usage:
  favro-mcp auth login     Capture email / API token / organization id (token input is masked).
  favro-mcp auth status    Show the active user / organization (token never printed).
  favro-mcp auth logout    Delete keyring entries.
  favro-mcp auth which     Print where the active credentials come from (env or keyring).
`)
	return nil
}

// authLogin prompts for email, API token (masked), and organization id,
// then writes them to the OS keyring. Token.Validate inside KeyringSource.Save
// is the single canonical validation point — we don't pre-validate here.
func authLogin(ctx context.Context, stdin io.Reader, stderr io.Writer) error {
	r := bufio.NewReader(stdin)

	email, err := promptLine(r, stderr, "Favro user email: ")
	if err != nil {
		return err
	}
	apiToken, err := promptSecret(r, stderr, "Favro API token (input hidden): ")
	if err != nil {
		return err
	}
	orgID, err := promptLine(r, stderr, "Favro organization id: ")
	if err != nil {
		return err
	}

	tok := auth.Token{Email: email, APIToken: apiToken, OrganizationID: orgID}
	if err := (auth.KeyringSource{}).Save(ctx, tok); err != nil {
		return err
	}
	errf(stderr, "Stored credentials for %s in OS keyring.\n", tok.Email)
	return nil
}

// authStatus loads the active token via the same precedence as the
// server and prints email / org id / source. The API token is never
// printed, not even partially.
func authStatus(ctx context.Context, stderr io.Writer) error {
	rt, err := auth.ResolveDefault(ctx)
	if err != nil {
		errf(stderr, "No Favro credentials configured.\n  hint: %s.\n  error: %v\n", missingCredsHint(), err)
		return err
	}
	errf(stderr,
		"Source:       %s\nUser:         %s\nOrganization: %s\nToken:        ***** (not displayed)\n",
		rt.Source, rt.Token.Email, rt.Token.OrganizationID,
	)
	return nil
}

// authLogout deletes the keyring entries unconditionally. Env-var
// credentials are unaffected — the server's resolution will continue
// to find them on next start.
func authLogout(ctx context.Context, stderr io.Writer) error {
	if err := (auth.KeyringSource{}).Delete(ctx); err != nil {
		errf(stderr, "Failed to delete keyring entries: %v\n", err)
		return err
	}
	errln(stderr, "Removed keyring entries (env-var credentials, if any, are unaffected).")
	return nil
}

// authWhich prints which Source produced the active token, or reports
// that no credentials are configured. Exit code is non-zero when no
// credentials exist, so shells can branch on it.
func authWhich(ctx context.Context, stderr io.Writer) error {
	rt, err := auth.ResolveDefault(ctx)
	if err != nil {
		errln(stderr, "(no credentials configured)")
		return err
	}
	errln(stderr, rt.Source)
	return nil
}

// promptLine writes prompt to stderr and reads a single line from r.
// Surrounding whitespace is trimmed: the trailing newline always, and
// leading/trailing spaces because credential fields with stray
// whitespace are virtually always mistakes (and Token.Validate would
// reject them anyway).
func promptLine(r *bufio.Reader, stderr io.Writer, prompt string) (string, error) {
	errprint(stderr, prompt)
	line, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// promptSecret reads a line from the controlling terminal with echo
// disabled. When stdin is not a TTY (e.g. piped from a script or test
// harness) it falls back to reading from the same buffered reader
// promptLine uses, so any bytes already buffered ahead aren't lost.
//
// The TTY path bypasses r and reads directly from os.Stdin's fd
// because term.ReadPassword requires a real fd, not an io.Reader.
func promptSecret(r *bufio.Reader, stderr io.Writer, prompt string) (string, error) {
	errprint(stderr, prompt)

	// File descriptors are small ints on every platform Go targets;
	// the uintptr→int conversion is safe by construction.
	fd := int(os.Stdin.Fd()) //nolint:gosec
	if !term.IsTerminal(fd) {
		errln(stderr, "(stdin is not a terminal — input will be visible)")
		return promptLine(r, stderr, "")
	}

	pw, err := term.ReadPassword(fd)
	errln(stderr) // newline after the masked input
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(pw)), nil
}
