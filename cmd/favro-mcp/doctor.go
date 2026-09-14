package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/mmedum/favro-mcp/internal/auth"
	"github.com/mmedum/favro-mcp/internal/config"
	"github.com/mmedum/favro-mcp/internal/favroapi"
	"github.com/mmedum/favro-mcp/internal/redact"
	"github.com/mmedum/favro-mcp/internal/version"
)

// `favro-mcp doctor` answers the question every first-run bug report is
// really asking: which of the four things that can be wrong is wrong.
// The shared standard asks for it because most first reports are a
// missing credential or a binding that names an organization the token
// cannot see, and neither produces an error a user can act on — the
// server logs "credentials rejected" and exits.
//
// **Its output is written to be pasted into a public issue**, which is
// what the issue form asks for. That is why every line goes through
// internal/redact rather than being printed directly: hard rule 1 does
// not stop applying because the person pasting is the tenant. The
// redactor's placeholders are stable within a run, so a report still
// shows that the organization id the credentials carry is or is not one
// of the ids the token can see — which is the whole diagnostic value,
// and a blanket [REDACTED] would destroy it.
//
// It exits non-zero when any check fails, so it can gate a script.

// doctorTimeout caps the whole live half. A wedged DNS lookup should
// produce a report saying the API was unreachable, not a hang.
const doctorTimeout = 15 * time.Second

// status is one check's verdict. The zero value is deliberately not a
// pass: a check that returns without deciding reads as failed.
type status int

const (
	statusFail status = iota
	statusOK
	statusWarn
	// statusNote is a fact rather than a verdict — the build stamp, the
	// platform. It never affects the exit code.
	statusNote
)

func (s status) mark() string {
	switch s {
	case statusOK:
		return "ok  "
	case statusWarn:
		return "warn"
	case statusNote:
		return "    "
	default:
		return "FAIL"
	}
}

// check is one line of the report.
type check struct {
	status status
	label  string
	detail string
}

// runDoctor writes the report to w. stdout, like --version and
// --dump-schemas: a report exists to be redirected to a file or piped
// to a clipboard, and the auth subcommands' stderr discipline is about
// interactive prompts, which this has none of.
func runDoctor(ctx context.Context, cfg config.Config, w io.Writer, args []string) error {
	fs := flag.NewFlagSet("favro-mcp doctor", flag.ContinueOnError)
	fs.SetOutput(w)
	showIDs := fs.Bool("show-ids", false,
		"print organization ids and addresses in full; unsafe to paste into an issue")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// "" means production. A test points it at an httptest server.
	return doctorReport(ctx, cfg, w, *showIDs, "")
}

func doctorReport(ctx context.Context, cfg config.Config, w io.Writer, showIDs bool, baseURL string) error {
	ctx, cancel := context.WithTimeout(ctx, doctorTimeout)
	defer cancel()

	// Resolve first, so the redactor holds the token before anything is
	// printed. Only the token goes in as a literal: it can look like
	// anything, so no pattern should be trusted to find it. The address
	// and the organization id have shapes the redactor already knows,
	// and passing them here would flatten all three to {credential} —
	// which destroys the property this report is built on, that the
	// same value reads as the same placeholder throughout. A binding
	// failure has to be able to say {id 1} is not {id 2}.
	rt, resolveErr := auth.ResolveDefault(ctx)
	tok := rt.Token

	// The token goes in as a secret and the identifiers are registered,
	// which is the split internal/redact draws: secrets collapse to
	// {credential}, identifiers keep a stable {kind n}. A binding
	// failure has to be able to say {id 1} is not {id 2}, so the
	// address and the organization id must not go in as secrets.
	r := redact.New(tok.APIToken)
	r.Literal(redact.KindUser, tok.Email)
	r.Literal(redact.KindID, tok.OrganizationID)

	// The mode is the choice of tier, and nothing else. Both scrub the
	// token, because it is the one value neither mode ever prints.
	show := r.String
	if showIDs {
		show = r.Secrets
	}

	// Each section is produced as it is printed, not before. The API
	// section can take doctorTimeout to answer, and on the machine this
	// subcommand exists for — no credentials, wedged DNS, unreachable
	// Favro — building the whole report first means a silent terminal
	// for fifteen seconds and then everything at once. The two sections
	// that need nothing appear immediately.
	sections := []struct {
		name   string
		checks func() []check
	}{
		{"build", buildChecks},
		{"credentials", func() []check { return credentialChecks(rt, resolveErr) }},
		{"favro api", func() []check { return apiChecks(ctx, rt, resolveErr, baseURL, r) }},
		{"settings", func() []check { return settingChecks(cfg) }},
	}

	if showIDs {
		errf(w, "favro-mcp doctor --show-ids\n\n"+
			"Ids and addresses are shown in full so you can act on them. The token is\n"+
			"still never printed. DO NOT paste this into an issue — run without\n"+
			"--show-ids for a report that is safe to share.\n")
	} else {
		errf(w, "favro-mcp doctor\n\n"+
			"Paste this whole report into an issue. Ids, addresses and the token are\n"+
			"replaced with stable placeholders — the same value reads as the same\n"+
			"{placeholder} throughout, so the report still shows what matched what.\n"+
			"Run with --show-ids to see the real values on your own screen.\n")
	}

	var failed, warned int
	for _, s := range sections {
		errf(w, "\n%s\n", s.name)
		for _, c := range s.checks() {
			switch c.status {
			case statusFail:
				failed++
			case statusWarn:
				warned++
			}
			// The label is this program's own text and the detail is
			// not; redacting the whole line costs nothing and means a
			// detail that grows a new field is covered by default.
			errf(w, "  %s %-26s %s\n", c.status.mark(), c.label, show(c.detail))
		}
	}

	if showIDs {
		errf(w, "\n%d failed, %d warnings. NOT REDACTED (--show-ids): do not paste this into an issue.\n",
			failed, warned)
	} else {
		// The count is the check on the checker. A report that redacted
		// nothing either had nothing to redact or stopped redacting,
		// and those print the same page otherwise.
		errf(w, "\n%d failed, %d warnings, %d values redacted.\n", failed, warned, r.Count())
	}
	if failed > 0 {
		return fmt.Errorf("%d check(s) failed", failed)
	}
	return nil
}

func buildChecks() []check {
	return []check{
		{statusNote, "version", version.String()},
		// Which of the three ways this binary learned its version. A
		// `go install` build reports "buildinfo", and that is the
		// difference between a report naming a release and one naming
		// nothing.
		{statusNote, "version source", version.Source()},
		{statusNote, "module", version.Module()},
		{statusNote, "platform", runtime.GOOS + "/" + runtime.GOARCH + ", " + runtime.Version()},
	}
}

// credentialChecks reports what resolved and from where, without
// contacting Favro.
func credentialChecks(rt auth.ResolvedToken, resolveErr error) []check {
	if resolveErr != nil {
		return []check{{
			statusFail, "resolution",
			fmt.Sprintf("%v — %s", resolveErr, missingCredsHint()),
		}}
	}
	out := []check{{statusOK, "source", rt.Source}}

	// Each field separately: "missing credential field(s): email" is
	// the message, and a user reading a wall of it cannot tell which of
	// the three they forgot.
	for _, f := range []struct {
		label string
		value string
	}{
		{"email", rt.Token.Email},
		{"organization id", rt.Token.OrganizationID},
	} {
		if strings.TrimSpace(f.value) == "" {
			out = append(out, check{statusFail, f.label, "not set"})
			continue
		}
		out = append(out, check{statusOK, f.label, f.value})
	}
	if n := len(rt.Token.APIToken); n == 0 {
		out = append(out, check{statusFail, "api token", "not set"})
	} else {
		// The length, never the value, and never a prefix: a prefix of
		// a secret is still part of a secret.
		out = append(out, check{statusOK, "api token", fmt.Sprintf("set (%d characters)", n)})
	}

	if err := rt.Token.Validate(); err != nil {
		out = append(out, check{statusFail, "well-formed", err.Error()})
	}
	return out
}

// apiChecks is the live half: can these credentials reach Favro, and is
// the organization they are bound to one this token can actually see.
//
// The second question is the one worth the request. A token that works
// and an organization id that does not belong to it is the failure this
// server cannot report usefully at runtime — every tool returns a
// not_found, and the tool that would explain why is this one.
func apiChecks(ctx context.Context, rt auth.ResolvedToken, resolveErr error, baseURL string, r *redact.Redactor) []check {
	if resolveErr != nil {
		return []check{{statusWarn, "reachable", "skipped — no credentials resolved"}}
	}
	if err := rt.Token.Validate(); err != nil {
		return []check{{statusWarn, "reachable", "skipped — credentials are not well-formed"}}
	}

	client := favroapi.NewClient(rt.Token)
	if baseURL != "" {
		client.BaseURL = baseURL
	}
	start := time.Now()
	page, err := client.ListOrganizations(ctx, 1, "")
	elapsed := time.Since(start).Round(time.Millisecond)

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return []check{{statusFail, "reachable", fmt.Sprintf("no response in %s", doctorTimeout)}}
		}
		return []check{{statusFail, "reachable", err.Error()}}
	}

	out := []check{{statusOK, "reachable", fmt.Sprintf("GET /organizations in %s", elapsed)}}

	var ids []string
	for _, o := range page.Entities {
		// Registered rather than left to the id pattern. These arrive
		// from the API rather than from configuration, and the pattern
		// is anchored on a 24-hex run — an id Favro mints in some other
		// shape would print in full, in a report written to be pasted
		// into a public issue.
		r.Literal(redact.KindID, o.OrganizationID)
		ids = append(ids, o.OrganizationID)
	}
	switch {
	case len(ids) == 0:
		out = append(out, check{
			statusWarn, "organizations visible",
			"none — the token authenticates but belongs to no organization",
		})
	case slices.Contains(ids, rt.Token.OrganizationID):
		out = append(out, check{
			statusOK, "organization binding",
			fmt.Sprintf("%s, which is %s", rt.Token.OrganizationID, oneOf(len(ids))),
		})
	default:
		// Naming the visible ids is what makes this actionable. Under
		// the default redaction a maintainer reading the issue sees
		// "{id 1} is not among … ({id 2})" — enough to tell the two
		// apart, which is the diagnosis. It is not enough to FIX it,
		// and that is what --show-ids is for.
		out = append(out, check{
			statusFail, "organization binding",
			fmt.Sprintf("%s is not among the %d this token can see (%s) — %s names the wrong organization; "+
				"re-run with --show-ids to see which id to use",
				rt.Token.OrganizationID, len(ids), strings.Join(ids, ", "), auth.EnvOrganizationID),
		})
	}
	if page.HasNextPage() {
		out = append(out, check{statusNote, "organizations", "more than one page; only the first was read"})
	}
	return out
}

// settingChecks reports the environment this server would start in.
// Every one of these changes what the server does, and none of them is
// visible in a bug report otherwise.
func settingChecks(cfg config.Config) []check {
	out := []check{{statusOK, config.EnvLogLevel, describeEnv(config.EnvLogLevel, cfg.LogLevel.String())}}

	destructive := check{
		statusOK, config.EnvEnableDestructive,
		"unset — the delete-style tools are not registered",
	}
	if cfg.Destructive {
		// A warning rather than a fact: a host in an auto-approve mode
		// runs an annotated tool without prompting, which is the whole
		// reason the flag exists.
		destructive = check{
			statusWarn, config.EnvEnableDestructive,
			"true — delete-style tools ARE registered and can run unattended",
		}
	}
	out = append(out, destructive)

	skip := check{statusOK, config.EnvSkipValidate, "unset — credentials are checked at startup"}
	if cfg.SkipValidate {
		skip = check{statusWarn, config.EnvSkipValidate, "set — startup credential validation is disabled"}
	}
	out = append(out, skip)

	// Anything config.Load could not read. These are already warned at
	// startup, where nobody filing an issue was looking.
	for _, warning := range cfg.Warnings {
		out = append(out, check{statusWarn, "configuration", warning})
	}
	return out
}

// oneOf phrases the count without the "1 organizations" that a bare
// %d produces on the overwhelmingly common single-organization account.
func oneOf(n int) string {
	if n == 1 {
		return "the only organization this token can see"
	}
	return fmt.Sprintf("one of the %d organizations this token can see", n)
}

// describeEnv says whether a value came from the environment or is the
// default, which is the distinction a report needs: "info" alone does
// not say whether the user set it.
func describeEnv(name, value string) string {
	if raw := strings.TrimSpace(os.Getenv(name)); raw != "" {
		return value + " (from " + name + ")"
	}
	return value + " (default)"
}
