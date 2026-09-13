package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The launcher is generated from the same table the packer stages from,
// which is what closes the hole the shared standard names last: a
// manifest can pass every referential check while the launcher, which no
// manifest mentions, dispatches to names nothing stages.
func TestLauncherDispatchesToEveryStagedBinary(t *testing.T) {
	script := launcherScript()
	files := staged()
	for _, p := range platforms {
		if len(p.uname) == 0 {
			continue
		}
		target := "bin/" + p.dir() + "/" + p.binary()
		if !slices.Contains(files, target) {
			t.Errorf("the launcher's table names %s and the packer does not stage it", target)
		}
		if !strings.Contains(script, p.dir()+"/"+p.binary()) {
			t.Errorf("the launcher does not dispatch to %s", p.dir())
		}
	}
	if !strings.Contains(script, "exec ") {
		t.Error("the launcher must exec: the server talks MCP over that process's stdio, and a " +
			"shell left in the middle owns the pipes")
	}
	if !strings.Contains(script, ">&2") {
		t.Error("the unsupported-platform message must go to stderr; a line of English on stdout " +
			"corrupts the JSON-RPC session")
	}
}

func TestGoreleaserMatrixIsTheOneTheBundleClaims(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	built, err := goreleaserMatrix(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(built) < 4 {
		t.Fatalf("the matrix parsed as %v; that is not this repository's build list", built)
	}
	if slices.Contains(built, "windows-arm64") {
		t.Error("windows-arm64 is `ignore`d in .goreleaser.yaml and should not be in the matrix")
	}
	for _, p := range platforms {
		if !slices.Contains(built, p.dir()) {
			t.Errorf("the bundle stages %s and goreleaser does not build it", p.dir())
		}
	}
}

// A parse that silently finds nothing would make every platform read as
// unbuilt, which is a gate that fails for the wrong reason — or, with
// the comparison the other way round, one that passes for it.
func TestGoreleaserMatrixRefusesAFileItCannotRead(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".goreleaser.yaml"), []byte("version: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := goreleaserMatrix(dir); err == nil {
		t.Error("a config with no build matrix should fail loudly rather than report no platforms")
	}
}

func TestServerCommandReadsTheOneEntry(t *testing.T) {
	got, err := serverCommand(map[string]any{"mcpServers": map[string]any{
		"favro": map[string]any{"command": "${CLAUDE_PLUGIN_ROOT}/bin/favro-mcp"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "${CLAUDE_PLUGIN_ROOT}/bin/favro-mcp" {
		t.Errorf("command = %q", got)
	}
	for _, bad := range []map[string]any{
		{},
		{"mcpServers": map[string]any{}},
		{"mcpServers": map[string]any{"a": map[string]any{}, "b": map[string]any{}}},
		{"mcpServers": map[string]any{"favro": map[string]any{}}},
	} {
		if _, err := serverCommand(bad); err == nil {
			t.Errorf("%v should not read as a valid bundle config", bad)
		}
	}
}

// The same inputs should give the same archive, byte for byte, and an
// unset timestamp writes zeroes that display as the impossible
// 1980-00-00.
func TestWriteZipIsReproducibleAndKeepsModes(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{
		"bin/favro-mcp":              []byte("#!/usr/bin/env bash\n"),
		"bin/linux-amd64/favro-mcp":  []byte("ELF"),
		".claude-plugin/plugin.json": []byte("{}"),
	}
	executable := map[string]bool{"bin/favro-mcp": true, "bin/linux-amd64/favro-mcp": true}

	first := filepath.Join(dir, "one.plugin")
	second := filepath.Join(dir, "two.plugin")
	for _, out := range []string{first, second} {
		if err := writeZip(out, files, executable); err != nil {
			t.Fatal(err)
		}
	}
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("two packs of the same input differ; the archive is not reproducible")
	}

	zr, err := zip.OpenReader(first)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	seen := map[string]os.FileMode{}
	for _, f := range zr.File {
		seen[f.Name] = f.Mode().Perm()
		if f.Modified.Year() < 1981 {
			t.Errorf("%s has no modification time; an unset one displays as 1980-00-00", f.Name)
		}
	}
	// Every staged binary, not only the entry point: a zip carries the
	// mode, and the Windows binary arrives unrunnable without it.
	for name := range executable {
		if seen[name]&0o111 == 0 {
			t.Errorf("%s is not executable in the archive (%v)", name, seen[name])
		}
	}
	if seen[".claude-plugin/plugin.json"]&0o111 != 0 {
		t.Error("a manifest does not need the execute bit")
	}
}

// The gate against the repository.
func TestPluginGateAgainstThisRepository(t *testing.T) {
	var out sink
	if err := pluginGate(&out, nil); err != nil {
		t.Fatalf("the committed manifest should match what the packer stages: %v", err)
	}
	out.mustSay(t, "files staged")
}

// The version the manifest will claim, against the version the binary
// reports. A sibling shipped a bundle that agreed in four of the five
// places it states its version; this is the one of the five that a user
// meets at runtime.
func TestVersionsAgree(t *testing.T) {
	host := runtime.GOOS + "-" + runtime.GOARCH

	// A snapshot legitimately disagrees: goreleaser stamps the binary
	// from the last tag and names the archive after the next patch.
	if err := versionsAgree(map[string]string{host: "/nonexistent"}, "1.1.3-snapshot-abc1234"); err != nil {
		t.Errorf("a snapshot should be exempt, got %v", err)
	}

	// Nothing staged for this machine: a foreign architecture cannot be
	// executed, and refusing to pack for that reason would be a gate
	// failing on where it ran.
	if err := versionsAgree(map[string]string{"plan9-mips": "/nonexistent"}, "1.2.0"); err != nil {
		t.Errorf("no host binary means nothing to ask, got %v", err)
	}

	// A staged binary that does not report the version being stamped.
	if err := versionsAgree(map[string]string{host: os.Args[0]}, "9.9.9"); err == nil {
		t.Error("a binary that does not report the packed version should stop the pack")
	}
}

// stageBundle against a synthetic dist: the version comes from
// goreleaser's own metadata, the manifest is stamped through a JSON
// decode and encode, and every binary is staged executable.
func TestStageBundleStampsTheVersionAndStagesEveryPlatform(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	// A snapshot version, because the staged binaries here are synthetic
	// and the last staging step asks the host's one for its version. The
	// real-version path is TestVersionsAgree's subject; this test's is
	// what gets staged and how the manifest is stamped.
	const version = "1.2.0-snapshot-abc1234"
	dist := fakeDist(t, version)

	files, executable, packed, err := stageBundle(root, dist)
	if err != nil {
		t.Fatal(err)
	}
	if packed != version {
		t.Errorf("version = %q, want the one goreleaser recorded", packed)
	}
	var manifest map[string]any
	if err := json.Unmarshal(files[".claude-plugin/plugin.json"], &manifest); err != nil {
		t.Fatalf("the staged manifest is not JSON: %v", err)
	}
	if manifest["version"] != version {
		t.Errorf("the staged manifest says version %v; the bundle would claim a version its "+
			"binaries do not", manifest["version"])
	}
	if manifest["name"] != "favro-mcp" {
		t.Errorf("the stamp rewrote more than the version: %v", manifest)
	}
	for _, want := range staged() {
		if _, ok := files[want]; !ok {
			t.Errorf("the packer does not stage %s, which the gate says it will", want)
		}
	}
	for _, p := range platforms {
		name := "bin/" + p.dir() + "/" + p.binary()
		if !executable[name] {
			t.Errorf("%s is staged without the execute bit; on Windows it arrives unrunnable", name)
		}
	}
}

// The committed manifest carries a placeholder, and the packer refuses
// any other value: a manifest edited by hand would otherwise ship a
// version nothing else in the release agrees with.
func TestStageBundleRefusesWhatItCannotTrust(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := stageBundle(root, t.TempDir()); err == nil {
		t.Error("no dist at all should be an error naming what to run first")
	}
	if _, _, _, err := stageBundle(root, fakeDist(t, placeholderVersion)); err == nil {
		t.Error("the packer must not stamp the placeholder as a real version")
	}
	if _, _, _, err := stageBundle(root, fakeDist(t, "")); err == nil {
		t.Error("an empty version should stop the pack")
	}

	// A dist missing a platform the launcher dispatches to: the bundle
	// would install and then fail for every user of that platform, with
	// the launcher's own "missing from the bundle" message.
	dist := fakeDist(t, "1.2.0-snapshot-abc1234")
	trimmed := filepath.Join(dist, "artifacts.json")
	data, err := os.ReadFile(trimmed)
	if err != nil {
		t.Fatal(err)
	}
	var artifacts []map[string]any
	if err := json.Unmarshal(data, &artifacts); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(trimmed, artifacts[:1]); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := stageBundle(root, dist); err == nil {
		t.Error("a dist missing a platform should stop the pack")
	}
}

// fakeDist writes what goreleaser leaves behind: a metadata document, an
// artifact list, and a binary per platform.
func fakeDist(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	if err := writeJSON(filepath.Join(dir, "metadata.json"), map[string]any{"version": version}); err != nil {
		t.Fatal(err)
	}
	var artifacts []map[string]any
	for _, p := range platforms {
		bin := filepath.Join(dir, p.dir(), p.binary())
		if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bin, []byte("synthetic binary for "+p.dir()), 0o755); err != nil {
			t.Fatal(err)
		}
		artifacts = append(artifacts, map[string]any{
			"path": bin, "goos": p.goos, "goarch": p.goarch, "type": "Binary",
		})
	}
	if err := writeJSON(filepath.Join(dir, "artifacts.json"), artifacts); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Two binaries for one platform means packing whichever sorted first,
// and a bundle built from the wrong binary is not something a checksum
// catches: the checksum is of whatever was built.
func TestBuiltBinariesRefusesAnAmbiguousDist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "artifacts.json")
	if err := writeJSON(path, []map[string]any{
		{"path": "a", "goos": "linux", "goarch": "amd64", "type": "Binary"},
		{"path": "b", "goos": "linux", "goarch": "amd64", "type": "Binary"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := builtBinaries(path); err == nil {
		t.Error("two binaries for one platform should stop the pack")
	}
	if err := writeJSON(path, []map[string]any{{"path": "a", "type": "Archive"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := builtBinaries(path); err == nil {
		t.Error("a dist with no binaries should fail rather than stage nothing")
	}
}

// The premise the lint exclusions and the whole scripts/ directory rest
// on: the release builds the server and nothing else.
func TestOnlyTheServerShips(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	if problems := onlyTheServerShips(root); len(problems) > 0 {
		t.Errorf("the release should build only ./cmd/favro-mcp: %v", problems)
	}
	// A config with no build at all must not read as "nothing extra
	// ships": it reads as a check that has lost its subject.
	if problems := onlyTheServerShips(t.TempDir()); len(problems) == 0 {
		t.Error("an unreadable build config should be reported, not passed")
	}
}
