package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// The published schema the bundle manifest cites, kept here so it can be
// checked rather than merely named.
//
// `declarationProblems` holds the manifest's $schema to being present,
// pinned and agreeing with manifest_version. That is a claim ABOUT the
// reference; none of it opens the schema. A document can cite the right
// file and not satisfy it.
//
// Vendored rather than fetched, because a gate that needs the network
// fails on a train. Embedded rather than read from disk, so the gate does
// not depend on the directory it was started in. A frozen copy cannot
// notice upstream publishing a newer schema: the digest proves these
// bytes are the ones somebody reviewed, not that upstream still serves
// them.
//
//go:embed schemas/*.json
var schemaFS embed.FS

// mcpbSchemaFile is the vendored copy of the schema a 0.3 manifest
// cites. It moves with minManifestVersion: the floor says which format
// this repository has checked, and this is the document that says what
// that format is.
const mcpbSchemaFile = "mcpb-manifest-v0.3.schema.json"

type vendoredSchema struct {
	// source is where the bytes came from.
	source string
	// sha256 is the digest of what was reviewed, over the file with LF
	// line endings. Without it, "make the document pass" and "edit the
	// schema" are the same amount of work.
	sha256 string
}

var vendoredSchemas = map[string]vendoredSchema{
	mcpbSchemaFile: {
		source: "https://raw.githubusercontent.com/anthropics/mcpb/v2.1.2/schemas/mcpb-manifest-v0.3.schema.json",
		sha256: "3a0ac9d845711a1b9b17dfa5a52f8b60628239d6a86a9db417206a9efc78592d",
	},
}

// loadSchema returns the vendored schema, having first checked that the
// bytes are the ones recorded. A Windows checkout may carry the file with
// CRLF line endings, so the digest is taken over LF.
func loadSchema(name string) (*jsonschema.Resolved, error) {
	want, known := vendoredSchemas[name]
	if !known {
		return nil, fmt.Errorf("no vendored schema named %q; the names are the files under scripts/gates/schemas", name)
	}
	raw, err := schemaFS.ReadFile("schemas/" + name)
	if err != nil {
		return nil, fmt.Errorf("read vendored %s: %w", name, err)
	}
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if got := hex.EncodeToString(sha256Of(raw)); got != want.sha256 {
		return nil, fmt.Errorf("vendored %s hashes to %s and the recorded digest is %s; if it was refreshed on "+
			"purpose, update the digest in the same commit, and re-read what changed rather than only the hash",
			name, got, want.sha256)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("vendored %s is not a valid schema document: %w", name, err)
	}
	resolved, err := s.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolve vendored %s: %w", name, err)
	}
	return resolved, nil
}

func sha256Of(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

// validateDocument holds one of this repository's documents to the
// vendored schema it cites. what names the document in the failure,
// because "does not validate" without it sends the reader to the wrong
// file.
func validateDocument(schemaName, what string, raw []byte) error {
	resolved, err := loadSchema(schemaName)
	if err != nil {
		return err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("%s is not valid JSON: %w", what, err)
	}
	if err := resolved.Validate(doc); err != nil {
		return fmt.Errorf("%s does not satisfy %s, which it cites: %w", what, schemaName, err)
	}
	return nil
}
