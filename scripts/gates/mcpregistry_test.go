package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestMCPRegistryAgainstThisRepository is the gate against the entry it
// guards, which is the run that matters.
func TestMCPRegistryAgainstThisRepository(t *testing.T) {
	var out sink
	if err := registry(&out, nil); err != nil {
		t.Fatalf("the committed registry entry would be refused: %v", err)
	}
	out.mustSay(t, registryServerName)
	out.mustSay(t, registryPlaceholder)
}

// TestMCPRegistryEntryCarriesAPlaceholder is the rule that keeps the
// committed entry from going stale. A real version in the tree is a
// stale version the moment the next release ships, and the hash beside
// it would be a lie about a file nobody can fetch.
func TestMCPRegistryEntryCarriesAPlaceholder(t *testing.T) {
	t.Parallel()

	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(root + "/" + registryFile)
	if err != nil {
		t.Fatal(err)
	}
	var entry registryEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Version != registryPlaceholder {
		t.Errorf("version is %q, want the placeholder %q — registry-publish fills the real one in at tag time",
			entry.Version, registryPlaceholder)
	}
	if got := entry.Packages[0].FileSha256; got != strings.Repeat("0", 64) {
		t.Errorf("fileSha256 is %q, want 64 zeros; a real hash in the tree names a file that may not exist", got)
	}
}

// TestMCPRegistryCatchesWhatTheRegistryRefuses pins each rule the
// registry enforces in code and its schema does not. Every one of these
// is refused at publish time, after the login has succeeded, which is
// the worst moment to find out — so each needs to fail here instead.
func TestMCPRegistryCatchesWhatTheRegistryRefuses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{
			name: "http identifier",
			edit: func(m map[string]any) { pkgOf(m)["identifier"] = "http://github.com/x/y/releases/download/v1/mcp.mcpb" },
			want: "must use https",
		},
		{
			name: "not a release asset",
			edit: func(m map[string]any) { pkgOf(m)["identifier"] = "https://github.com/x/mcp/archive/v1.mcpb" },
			want: "/releases/download/",
		},
		{
			// Note this can only fire alongside the .mcpb rule: any
			// identifier ending in .mcpb contains "mcp" by definition.
			// Both are kept because both are what the registry checks,
			// and a caller reading one rule should not have to know it
			// is subsumed by another.
			name: "no mcp in the identifier",
			edit: func(m map[string]any) {
				pkgOf(m)["identifier"] = "https://github.com/x/y/releases/download/v1/bundle.zip"
			},
			want: `must contain "mcp"`,
		},
		{
			name: "registryBaseUrl set",
			edit: func(m map[string]any) { pkgOf(m)["registryBaseUrl"] = "https://example.invalid" },
			want: "registryBaseUrl is set",
		},
		{
			name: "short hash",
			edit: func(m map[string]any) { pkgOf(m)["fileSha256"] = "abc123" },
			want: "64 hex characters",
		},
		{
			name: "wrong namespace",
			edit: func(m map[string]any) { m["name"] = "favro-mcp" },
			want: "io.github. namespace",
		},
		{
			name: "description too long",
			edit: func(m map[string]any) { m["description"] = strings.Repeat("x", 101) },
			want: "at most 100",
		},
		{
			name: "two packages",
			edit: func(m map[string]any) {
				pkgs, _ := m["packages"].([]any)
				m["packages"] = append(pkgs, pkgs[0])
			},
			want: "publishes exactly one",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := committedEntry(t)
			tc.edit(m)
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			_, problems := checkRegistryEntry(raw, registryPlaceholder)
			if len(problems) == 0 {
				t.Fatalf("no problem reported; the registry would refuse this at publish time")
			}
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Errorf("problems = %v, want one naming %q", problems, tc.want)
			}
		})
	}
}

// TestSumFromChecksums reads a hash the way the publish path does.
func TestSumFromChecksums(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/checksums.txt"
	want := strings.Repeat("a", 64)
	body := want + "  favro-mcp_1.2.3.mcpb\n" + strings.Repeat("b", 64) + "  other.tar.gz\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := sumFromChecksums(path, "favro-mcp_1.2.3.mcpb")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != want {
		t.Errorf("got = %v, want %v", got, want)
	}
	// A bundle the release did not build must not silently publish with
	// somebody else's hash.
	if _, err := sumFromChecksums(path, "favro-mcp_9.9.9.mcpb"); err == nil {
		t.Error("a missing bundle must be an error")
	}
}

func committedEntry(t *testing.T) map[string]any {
	t.Helper()

	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(root + "/" + registryFile)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func pkgOf(m map[string]any) map[string]any {
	pkgs, _ := m["packages"].([]any)
	p, _ := pkgs[0].(map[string]any)
	return p
}
