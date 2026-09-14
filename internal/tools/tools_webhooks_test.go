package tools

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// webhookBody is what Favro returns for a webhook, secret included.
//
// A bare array, because that is what the live endpoint sends — every
// other Favro collection uses the paginated envelope and this one does
// not. The first version of this fixture was an envelope, matching the
// assumption rather than the API, and it passed while the client could
// not decode a real response.
const webhookBody = `[{
	"webhookId":"wh-1",
	"widgetCommonId":"w-1",
	"name":"Nightly export",
	"postToUrl":"https://receiver.invalid/hook",
	"secret":"synthetic-webhook-signing-secret",
	"options":{"columnId":"col-1","notifications":["Card created","Card moved"]}
}]`

// TestWebhookSecretIsNeverDecoded is the assertion behind the comment
// on favro.Webhook.
//
// Favro returns the signing secret on every read, and it authenticates
// deliveries to whoever consumes that webhook: anyone holding it can
// forge an event. The wire type has no field for it, so it cannot reach
// structuredContent, the readable half, or a log — and the check is
// made over the whole serialized result rather than over the fields
// somebody remembered to look at.
func TestWebhookSecretIsNeverDecoded(t *testing.T) {
	t.Parallel()

	const secret = "synthetic-webhook-signing-secret"

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(webhookBody))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listWebhooksToolName,
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %v", res.Content)
	}

	whole, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(whole), secret) {
		t.Error("the webhook signing secret reached the tool result; favro.Webhook must not model it")
	}

	// And the result still has to be worth returning.
	out := decodeStructured[listWebhooksOutput](t, res)
	if len(out.Webhooks) != 1 {
		t.Fatalf("len(out.Webhooks) = %d, want 1", len(out.Webhooks))
	}
	if out.Count != 1 {
		t.Errorf("out.Count = %d, want 1", out.Count)
	}
	if got := out.Webhooks[0].PostToURL; got != "https://receiver.invalid/hook" {
		t.Errorf("PostToURL = %q, want the address that identifies the webhook", got)
	}
	if got := out.Webhooks[0].WebhookID; got != "wh-1" {
		t.Errorf("WebhookID = %q, want wh-1", got)
	}
}

// TestWebhookColumnIDsAcceptsBothSpellings covers the reference
// disagreeing with the API: the field table documents `columnIds` as an
// array and every example shows a singular `columnId` string. A read
// that handled only the documented shape would drop the scope Favro
// actually sends.
func TestWebhookColumnIDsAcceptsBothSpellings(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		`{"columnIds":["a","b"]}`: {"a", "b"},
		`{"columnId":"a"}`:        {"a"},
		`{"columnIds":"a"}`:       {"a"}, // the plural key with a single value
		`{}`:                      nil,
	}
	for body, want := range cases {
		var opts favro.WebhookOptions
		if err := json.Unmarshal([]byte(body), &opts); err != nil {
			t.Fatalf("unmarshal %s: %v", body, err)
		}
		got := opts.ColumnIDs()
		if len(got) != len(want) {
			t.Errorf("ColumnIDs() for %s = %v, want %v", body, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("ColumnIDs() for %s = %v, want %v", body, got, want)
				break
			}
		}
	}
}

// TestDeleteWebhookIsGatedAndDryRunnable covers the two rules a
// destructive tool has to satisfy at once.
func TestDeleteWebhookIsGatedAndDryRunnable(t *testing.T) {
	t.Parallel()

	if _, present := listToolsWith(t, Options{})[deleteWebhookToolName]; present {
		t.Error("favro_delete_webhook is registered without FAVRO_ENABLE_DESTRUCTIVE")
	}

	calls := 0
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	cs := connectInMemoryWith(t, c)

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      deleteWebhookToolName,
		Arguments: map[string]any{"webhook_id": "wh-1", "dry_run": true},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %v", res.Content)
	}
	if calls != 0 {
		t.Errorf("dry run reached Favro %d times, want 0", calls)
	}

	out := decodeStructured[writeOutput[struct{}]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if !strings.Contains(out.PredictedStateDiff, "wh-1") {
		t.Errorf("the predicted change must name the webhook: %q", out.PredictedStateDiff)
	}
}

// TestListWebhooksDecodesABareArray is the wire contract itself, and it
// is here because a live call is what found it: Favro answers
// GET /webhooks with a JSON array, while every other collection answers
// with {entities, page, pages, requestId}. An empty organization
// returns `[]`, which is the case that fails loudest against a client
// expecting the envelope.
func TestListWebhooksDecodesABareArray(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"none":     `[]`,
		"one":      webhookBody,
		"no scope": `[{"webhookId":"wh-2","name":"Unscoped","postToUrl":"https://receiver.invalid/2"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			cs := connectInMemoryWith(t, c)

			res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
				Name:      listWebhooksToolName,
				Arguments: map[string]any{},
			})
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if res.IsError {
				t.Fatalf("a bare array must decode: %v", res.Content)
			}
			out := decodeStructured[listWebhooksOutput](t, res)
			if out.Count != len(out.Webhooks) {
				t.Errorf("Count = %d, len(Webhooks) = %d", out.Count, len(out.Webhooks))
			}
		})
	}
}
