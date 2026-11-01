package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v4/internal/server"
)

// A fixed resource, a template, a prompt and the instructions are in the
// dump, so no schema gate misses a change to them. The server has none
// of them today, so this test builds one that does.
func TestDumpSchemasListsEverythingAClientIsTold(t *testing.T) {
	s := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, &mcp.ServerOptions{Instructions: "Start with favro_ping."})
	mcp.AddTool(s, &mcp.Tool{Name: "favro_ping", Description: "d"},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
			return nil, struct{}{}, nil
		})
	read := func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) { return nil, nil }
	s.AddResource(&mcp.Resource{URI: "favro://example", Name: "example"}, read)
	s.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "favro://cards/{id}", Name: "card"}, read)
	s.AddPrompt(&mcp.Prompt{Name: "summarize"},
		func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) { return nil, nil })
	var buf bytes.Buffer
	if err := server.DumpSchemas(context.Background(), s, &buf, "test"); err != nil {
		t.Fatal(err)
	}
	var dump struct {
		Instructions string `json:"instructions"`
		Resources    []struct {
			URI string `json:"uri"`
		} `json:"resources"`
		ResourceTemplates []struct {
			URITemplate string `json:"uriTemplate"`
		} `json:"resourceTemplates"`
		Prompts []struct {
			Name string `json:"name"`
		} `json:"prompts"`
	}
	if err := json.Unmarshal(buf.Bytes(), &dump); err != nil {
		t.Fatal(err)
	}
	if dump.Instructions != "Start with favro_ping." {
		t.Errorf("instructions = %q, want the server's", dump.Instructions)
	}
	if len(dump.Resources) != 1 || dump.Resources[0].URI != "favro://example" {
		t.Errorf("resources = %+v, want favro://example", dump.Resources)
	}
	if len(dump.ResourceTemplates) != 1 || dump.ResourceTemplates[0].URITemplate != "favro://cards/{id}" {
		t.Errorf("resource templates = %+v, want favro://cards/{id}", dump.ResourceTemplates)
	}
	if len(dump.Prompts) != 1 || dump.Prompts[0].Name != "summarize" {
		t.Errorf("prompts = %+v, want summarize", dump.Prompts)
	}
}

// A listing that fails fails the dump rather than writing a partial
// surface, which the schema gates would read as items removed; so does a
// server with no tools, which describes nothing.
func TestDumpSchemasFailsOnAListingItCannotRead(t *testing.T) {
	server1 := func(failing string) *mcp.Server {
		s := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
		mcp.AddTool(s, &mcp.Tool{Name: "favro_ping", Description: "d"},
			func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
				return nil, struct{}{}, nil
			})
		s.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
			return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
				if method == failing {
					return nil, errors.New("refused")
				}
				return next(ctx, method, req)
			}
		})
		return s
	}
	for method, want := range map[string]string{
		"tools/list":               "list tools: ",
		"resources/list":           "list resources: ",
		"resources/templates/list": "list resource templates: ",
		"prompts/list":             "list prompts: ",
	} {
		t.Run(method, func(t *testing.T) {
			var buf bytes.Buffer
			err := server.DumpSchemas(context.Background(), server1(method), &buf, "test")
			if err == nil || !strings.HasPrefix(err.Error(), want) {
				t.Errorf("DumpSchemas = %v, want an error starting %q", err, want)
			}
			if buf.Len() != 0 {
				t.Errorf("a failed dump wrote %q", buf.String())
			}
		})
	}
	var buf bytes.Buffer
	empty := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	if err := server.DumpSchemas(context.Background(), empty, &buf, "test"); err == nil ||
		!strings.Contains(err.Error(), "registered no tools") {
		t.Errorf("DumpSchemas of a server with no tools = %v, want it refused", err)
	}
}
