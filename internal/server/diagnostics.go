package server

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// rateLimitToolName is the canonical name for the diagnostics tool.
const rateLimitToolName = "favro_rate_limit_status"

// rateLimitInput is empty — the tool reads observed state, no input.
type rateLimitInput struct{}

// RateLimitOutput is the structured response from
// favro_rate_limit_status. The fields mirror Favro's X-RateLimit-*
// headers plus a couple of derived convenience fields. All zero/empty
// when no Favro request has been made yet (HaveSeen == false).
type RateLimitOutput struct {
	HaveSeen          bool   `json:"have_seen" jsonschema:"true once at least one Favro response has been observed"`
	Limit             int    `json:"limit" jsonschema:"per-organization quota Favro reported (X-RateLimit-Limit)"`
	Remaining         int    `json:"remaining" jsonschema:"requests remaining in the current window (X-RateLimit-Remaining); -1 means the header was absent"`
	ResetUnix         int64  `json:"reset_unix" jsonschema:"epoch seconds when the current window resets (X-RateLimit-Reset); 0 if absent"`
	ResetIn           string `json:"reset_in" jsonschema:"human-readable time until reset"`
	RetryAfterSeconds int    `json:"retry_after_seconds" jsonschema:"seconds to wait before retrying (only set on the most recent 429); 0 otherwise"`
	LastObservedUnix  int64  `json:"last_observed_unix" jsonschema:"epoch seconds when the snapshot was recorded"`
	LastObservedAgo   string `json:"last_observed_ago" jsonschema:"human-readable age of the snapshot"`
	LastPath          string `json:"last_path" jsonschema:"URL path of the request that produced the snapshot"`
	LastStatus        int    `json:"last_status" jsonschema:"HTTP status code of that response"`
}

// registerRateLimitStatus wires favro_rate_limit_status into srv. The
// tool reads from client.LatestRateLimit; it does not contact Favro.
func registerRateLimitStatus(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: rateLimitToolName,
		Description: "Reports the most recently observed Favro rate-limit headers " +
			"(X-RateLimit-Limit / Remaining / Reset, plus Retry-After on 429). " +
			"Read-only and does NOT contact Favro — it surfaces what the client " +
			"already saw on prior requests so the caller can decide whether to slow down.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
			Title:        "Favro rate-limit status",
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ rateLimitInput) (*mcp.CallToolResult, RateLimitOutput, error) {
		snap, ok := client.LatestRateLimit()
		if !ok {
			return nil, RateLimitOutput{HaveSeen: false, Remaining: -1}, nil
		}
		out := RateLimitOutput{
			HaveSeen:          true,
			Limit:             snap.Limit,
			Remaining:         snap.Remaining,
			RetryAfterSeconds: int(snap.RetryAfter / time.Second),
			LastPath:          snap.Path,
			LastStatus:        snap.Status,
		}
		if !snap.Reset.IsZero() {
			out.ResetUnix = snap.Reset.Unix()
			out.ResetIn = formatShortDuration(time.Until(snap.Reset))
		}
		if !snap.ObservedAt.IsZero() {
			out.LastObservedUnix = snap.ObservedAt.Unix()
			out.LastObservedAgo = formatShortDuration(time.Since(snap.ObservedAt))
		}
		return nil, out, nil
	})
}

// formatShortDuration returns a second-resolution rendering like "5s",
// "2m0s", "1h0m0s". Negative durations clamp to "0s" so the surface
// never has confusing past-as-negative values.
func formatShortDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return d.Truncate(time.Second).String()
}
