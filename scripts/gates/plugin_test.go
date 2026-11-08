package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The launcher is generated from the same table the packer stages from,
// which closes the quietest hole a bundle has: a manifest can pass every
// referential check while the launcher, which no manifest mentions,
// dispatches to names nothing stages.
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

// releaseMatrix is six archives, windows-arm64 among them, and the one
// pair a bundle runs another's binary on: Windows on arm64 runs
// windows-amd64 under emulation. Every other test reads the table.
func TestTheReleaseMatrixIsTheFamilysSix(t *testing.T) {
	want := []string{"darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64"}
	if got := releasePairs(); !slices.Equal(got, want) {
		t.Errorf("releasePairs = %q, want %q", got, want)
	}
	if got := emulated(); len(got) != 1 || got["windows-arm64"] != "windows-amd64" {
		t.Errorf("emulated = %v, want windows-arm64 running windows-amd64", got)
	}
}

func TestGoreleaserMatrixIsTheOneTheBundleClaims(t *testing.T) {
	release, err := readGoreleaser(parityRepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	built, err := goreleaserMatrix(release)
	if err != nil {
		t.Fatal(err)
	}
	if want := releasePairs(); !slices.Equal(built, want) {
		t.Fatalf("the matrix parsed as %v, want %v", built, want)
	}
	for _, p := range platforms {
		if !slices.Contains(built, p.dir()) {
			t.Errorf("the bundle stages %s and goreleaser does not build it", p.dir())
		}
	}
}

// Every platform the release builds is one the plugin stages, or one
// that runs a staged binary under emulation; and the exemption holds only
// while its reason does.
func TestPluginMatrixProblems(t *testing.T) {
	six := releasePairs()
	if got := pluginMatrixProblems(six, emulated()); len(got) != 0 {
		t.Errorf("the six release pairs with windows-arm64 emulated: %q", got)
	}
	for _, tc := range []struct {
		name      string
		built     []string
		emulation map[string]string
		want      []string
	}{
		{
			"a built platform nothing reaches", six, nil,
			[]string{"GoReleaser builds windows-arm64 and no launcher arm reaches it; that binary ships unusable"},
		},
		{
			"emulated by a platform not staged", six,
			map[string]string{"windows-arm64": "windows-386"},
			[]string{"GoReleaser builds windows-arm64 and no launcher arm reaches it; that binary ships unusable"},
		},
		{
			"a staged platform not built", six[1:], emulated(),
			[]string{"the bundle stages bin/darwin-amd64, which .goreleaser.yaml does not build"},
		},
		{
			"emulation for a platform not built", six[:5], emulated(),
			[]string{"emulated names windows-arm64, which .goreleaser.yaml does not build"},
		},
		{
			"emulation for a staged platform", six,
			map[string]string{"windows-arm64": "windows-amd64", "linux-arm64": "linux-amd64"},
			[]string{"emulated names linux-arm64, which the bundle stages"},
		},
	} {
		if got := pluginMatrixProblems(tc.built, tc.emulation); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A parse that silently finds nothing would make every platform read as
// unbuilt, which is a gate that fails for the wrong reason — or, with
// the comparison the other way round, one that passes for it.
func TestGoreleaserMatrixRefusesAFileItCannotRead(t *testing.T) {
	if _, err := goreleaserMatrix(parseGoreleaser("version: 2\n")); err == nil {
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
	entries := []bundleFile{
		{name: ".claude-plugin/plugin.json", body: []byte("{}")},
		{name: "bin/favro-mcp", body: []byte("#!/usr/bin/env bash\n")},
		{name: "bin/linux-amd64/favro-mcp", body: []byte("ELF")},
	}
	executable := map[string]bool{"bin/favro-mcp": true, "bin/linux-amd64/favro-mcp": true}
	mode := func(name string) fs.FileMode {
		if executable[name] {
			return 0o755
		}
		return 0o644
	}

	first := filepath.Join(dir, "one.plugin")
	second := filepath.Join(dir, "two.plugin")
	for _, out := range []string{first, second} {
		if err := writeZipAt(out, entries, mode); err != nil {
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
	t.Chdir(parityRepoRoot)
	var out sink
	if err := pluginGate(&out, nil); err != nil {
		t.Fatalf("the committed manifest should match what the packer stages: %v", err)
	}
	out.mustSay(t, "files staged")
}

// The gate reads every build the release config holds, in goreleaser's
// usual shape, a build's first key on its dash: a second build of the
// gates ships maintainer tooling, and one for a platform no launcher arm
// reaches ships a binary nobody can run.
func TestThePluginGateReadsEveryBuild(t *testing.T) {
	const lastBuild = "      - arm64\n\nuniversal_binaries:"
	for _, tc := range []struct{ name, build, want string }{
		{"the committed builds", "", ""},
		{
			"a second build of the gates", "  - main: ./scripts/gates\n    goos:\n      - linux\n    goarch:\n      - amd64\n",
			".goreleaser.yaml:36: builds.main builds ./scripts/gates as well as the server; everything outside ./cmd is " +
				"maintainer tooling and must not ship",
		},
		{
			"a second build for a platform no launcher reaches", "  - main: ./cmd/favro-mcp\n    goos:\n      - freebsd\n    goarch:\n      - amd64\n",
			"GoReleaser builds freebsd-amd64 and no launcher arm reaches it; that binary ships unusable",
		},
		{
			"a build the reader cannot read whole", "  - main: ./cmd/favro-mcp\n    binary: &bin favro-mcp\n    goos:\n      - linux\n    goarch:\n      - amd64\n",
			".goreleaser.yaml:37: binary: an anchor, an alias or a tag; write the value out",
		},
		{
			"a build on goreleaser's default architectures", "  - main: ./cmd/favro-mcp\n    goos:\n      - linux\n",
			".goreleaser.yaml:36: a build that leaves goos or goarch to goreleaser's default, which this gate does not read",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, rel := range []string{"go.mod", manifestPath, mcpPath, parityGoreleaser} {
				put(t, root, rel, strings.ReplaceAll(string(repoFile(t, rel)), "\r\n", "\n"))
			}
			cfg := strings.ReplaceAll(string(repoFile(t, parityGoreleaser)), "\r\n", "\n")
			if !strings.Contains(cfg, lastBuild) {
				t.Fatalf("%s does not say %q", parityGoreleaser, lastBuild)
			}
			put(t, root, parityGoreleaser, strings.Replace(cfg, lastBuild, "      - arm64\n"+tc.build+"\nuniversal_binaries:", 1))
			t.Chdir(root)
			err := pluginGate(io.Discard, nil)
			if got := fmt.Sprint(err); tc.want == "" && err != nil || tc.want != "" && got != tc.want {
				t.Errorf("pluginGate = %v, want %q", err, tc.want)
			}
		})
	}
}

// The version the manifest will claim, against the version the binary
// reports. A bundle states its version in five places, and this is the
// one a user meets at runtime.
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

// stageBundle against dist as the post hook sees it: the version comes
// from goreleaser's own metadata, the manifest is stamped through a JSON
// decode and encode, and every binary is staged executable.
func TestStageBundleStampsTheVersionAndStagesEveryPlatform(t *testing.T) {
	// A snapshot version, which versionsAgree does not execute, so the
	// placeholders stand. The real-version path is TestVersionsAgree's
	// subject; this test's is what gets staged and how the manifest is
	// stamped.
	const version = "1.2.0-snapshot-abc1234"
	dist := inertHookDist(t, version)

	files, executable, packed, err := stageBundle(parityRepoRoot, dist)
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
	if _, _, _, err := stageBundle(parityRepoRoot, t.TempDir()); err == nil {
		t.Error("no dist at all should be an error naming what to run first")
	}
	if _, _, _, err := stageBundle(parityRepoRoot, inertHookDist(t, placeholderVersion)); err == nil {
		t.Error("the packer must not stamp the placeholder as a real version")
	}
	if _, _, _, err := stageBundle(parityRepoRoot, inertHookDist(t, "")); err == nil {
		t.Error("an empty version should stop the pack")
	}

	// A dist missing a platform the launcher dispatches to: the bundle
	// would install and then fail for every user of that platform, with
	// the launcher's own "missing from the bundle" message.
	dist := inertHookDist(t, "1.2.0-snapshot-abc1234")
	if err := os.RemoveAll(filepath.Join(dist, "favro-mcp_linux_arm64_v8.0")); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := stageBundle(parityRepoRoot, dist)
	if want := "dist carries no linux-arm64 binary matching *linux_arm64*/favro-mcp; the bundle would ship " +
		"a launcher arm pointing at nothing"; err == nil || err.Error() != want {
		t.Errorf("a dist missing linux-arm64 gave %v, want an error naming %q", err, want)
	}
}

// The packer writes into dist, where `./dist/*.plugin` in
// .goreleaser.yaml's checksum and release extra_files finds it. A bundle
// written anywhere else matches nothing, and GoReleaser skips an empty
// glob without a word, so the release ships no .plugin.
func TestPluginPackWritesTheBundleIntoDist(t *testing.T) {
	t.Chdir(parityRepoRoot)
	dist := inertHookDist(t, "1.2.0-snapshot-abc1234")
	var out sink
	if err := pluginPack(&out, []string{dist}); err != nil {
		t.Fatalf("pluginPack: %v", err)
	}
	out.mustSay(t, "packed favro-mcp.plugin: version 1.2.0-snapshot-abc1234")

	zr, err := zip.OpenReader(filepath.Join(dist, "favro-mcp.plugin"))
	if err != nil {
		t.Fatalf("the packer wrote no bundle into dist: %v", err)
	}
	defer func() { _ = zr.Close() }()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	want := []string{
		".claude-plugin/plugin.json", ".mcp.json", "LICENSE", "NOTICE", "README.md",
		"bin/darwin-amd64/favro-mcp", "bin/darwin-arm64/favro-mcp", "bin/favro-mcp", "bin/favro-mcp.cmd",
		"bin/linux-amd64/favro-mcp", "bin/linux-arm64/favro-mcp", "bin/windows-amd64/favro-mcp.exe",
	}
	if !slices.Equal(names, want) {
		t.Errorf("the bundle carries %q, want %q", names, want)
	}
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
// catches: the checksum is of whatever was built. A second amd64 variant
// in the build config is how dist comes to hold two.
func TestBuiltBinariesRefusesAnAmbiguousDist(t *testing.T) {
	dist := inertHookDist(t, "1.2.0-snapshot-abc1234")
	second := filepath.Join(dist, "favro-mcp_linux_amd64_v3", "favro-mcp")
	if err := os.MkdirAll(filepath.Dir(second), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("not a real binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := builtBinaries(dist)
	// The gate prints slash paths on every OS.
	slash := filepath.ToSlash(dist)
	if want := "dist carries 2 linux-amd64 binaries (" + slash + "/favro-mcp_linux_amd64_v1/favro-mcp " +
		slash + "/favro-mcp_linux_amd64_v3/favro-mcp); packing one of them is how a bundle ships the wrong build"; err == nil || err.Error() != want {
		t.Errorf("two linux-amd64 builds gave %v, want an error naming %q", err, want)
	}
}

// The premise the lint exclusions and the whole scripts/ directory rest
// on: the release builds the server and nothing else.
func TestOnlyTheServerShips(t *testing.T) {
	release, err := readGoreleaser(parityRepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if problems := onlyTheServerShips(release); len(problems) > 0 {
		t.Errorf("the release should build only ./cmd/favro-mcp: %v", problems)
	}
	for _, tc := range []struct{ name, cfg, want string }{
		// A config with no build at all must not read as "nothing extra
		// ships": it reads as a check that has lost its subject.
		{"no build", "version: 2\n", ".goreleaser.yaml names no build; this check is reading the wrong shape"},
		{
			"a build that names no main", "builds:\n  - id: x\n    binary: x\n",
			".goreleaser.yaml:2: a build that names no main, so goreleaser builds the module root; name ./cmd/favro-mcp",
		},
		// A ko image, or any other section with a main, builds that package
		// and ships it as the release's.
		{
			"another section's main",
			"builds:\n  - main: ./cmd/favro-mcp\nkos:\n  - id: tooling\n    main: ./scripts/gates\n    repositories:\n      - ghcr.io/example/x\n",
			".goreleaser.yaml:5: kos.main builds ./scripts/gates as well as the server; everything outside ./cmd is " +
				"maintainer tooling and must not ship",
		},
	} {
		if got := onlyTheServerShips(parseGoreleaser(tc.cfg)); !slices.Equal(got, []string{tc.want}) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
