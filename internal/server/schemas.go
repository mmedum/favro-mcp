package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// sdkModule is the import path whose version the dump reports. Read from
// the build info rather than written down: a constant here would be a
// copy of go.mod, and a copy goes stale because it is a copy.
const sdkModule = "github.com/modelcontextprotocol/go-sdk"

// DumpSchemas writes the tool surface as the wire carries it.
//
// The schemas come from a real ListTools over an in-memory transport
// rather than from the registration structs, because what a caller sees
// is what the SDK derived from the Go input types — the jsonschema tags,
// the required set, the defaults — and reading the structs would compare
// this server against itself instead of against the wire.
//
// It must show the WHOLE surface, including any tool that registers only
// behind an environment flag. A dump that showed a default build would
// make the schema diff blind to exactly the tools whose disappearance
// matters most. That obligation lands here rather than in the gate: a
// gate setting an environment variable fixes only the gate, while every
// other reader of --dump-schemas keeps the partial answer.
func DumpSchemas(ctx context.Context, srv *mcp.Server, w io.Writer, version string) error {
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		return fmt.Errorf("connect server: %w", err)
	}
	defer func() { _ = ss.Close() }()

	client := mcp.NewClient(&mcp.Implementation{Name: "schema-dump", Version: version}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		return fmt.Errorf("connect client: %w", err)
	}
	defer func() { _ = cs.Close() }()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	if len(res.Tools) == 0 {
		return fmt.Errorf("the server registered no tools; this dump would describe nothing")
	}
	slices.SortFunc(res.Tools, func(a, b *mcp.Tool) int { return strings.Compare(a.Name, b.Name) })

	out := struct {
		Server  string      `json:"server"`
		Version string      `json:"version"`
		SDK     string      `json:"sdk"`
		Tools   []*mcp.Tool `json:"tools"`
	}{serverName, version, sdkVersion(), res.Tools}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// sdkVersion is the MCP SDK version this binary was linked against, or
// "" when the build carries no module information — which is the case
// for a test binary, and is why nothing here fails on an empty answer.
func sdkVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, dep := range info.Deps {
		if dep.Path == sdkModule {
			return dep.Version
		}
	}
	return ""
}
