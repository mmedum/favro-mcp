package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// committedManifest is the manifest as the repository carries it,
// decoded. Every mutation test starts from it, so a test cannot pass by
// asserting against a shape the real manifest does not have.
func committedManifest(t *testing.T) bundleManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", mcpbManifestPath))
	if err != nil {
		t.Fatalf("read %s: %v", mcpbManifestPath, err)
	}
	var m bundleManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode %s: %v", mcpbManifestPath, err)
	}
	return m
}

// TestCommittedManifestPasses is the floor under every mutation test
// below: they prove a broken manifest is caught, and this proves the
// real one is not merely failing for some unrelated reason.
func TestCommittedManifestPasses(t *testing.T) {
	if problems := checkBundleManifest(committedManifest(t), mcpbStaged()); len(problems) > 0 {
		t.Errorf("the committed manifest does not pass its own check:\n%s", strings.Join(problems, "\n"))
	}
}

// TestDeletingTheWin32OverrideIsCaught is the check the shared standard
// names last, and the one a review found missing in a packer that had
// every other check in this file.
//
// Deleting the win32 override leaves a manifest where every command
// names a file the bundle really carries. Windows then runs the default
// command, which is the macOS universal binary. Nothing about the
// document is malformed and nothing about the staged tree is missing.
func TestDeletingTheWin32OverrideIsCaught(t *testing.T) {
	m := committedManifest(t)
	delete(m.Server.MCPConfig.PlatformOverrides, "win32")

	problems := checkBundleManifest(m, mcpbStaged())
	if !slices.ContainsFunc(problems, func(p string) bool {
		return strings.Contains(p, "win32") && strings.Contains(p, "favro-mcp.exe")
	}) {
		t.Errorf("a manifest that hands Windows the macOS binary was not caught; problems were:\n%s",
			strings.Join(problems, "\n"))
	}
}

// TestUnreachableOverrideIsCaught: an override for a platform the bundle
// does not claim is well formed, packs, and never runs.
func TestUnreachableOverrideIsCaught(t *testing.T) {
	m := committedManifest(t)
	m.Compatibility.Platforms = slices.DeleteFunc(m.Compatibility.Platforms,
		func(p string) bool { return p == "linux" })

	problems := checkBundleManifest(m, mcpbStaged())
	if !slices.ContainsFunc(problems, func(p string) bool {
		return strings.Contains(p, "linux") && strings.Contains(p, "can ever reach")
	}) {
		t.Errorf("an override no platform can reach was not caught; problems were:\n%s",
			strings.Join(problems, "\n"))
	}
}

// TestCommandOutsideTheBundleIsCaught covers both halves of the command
// rule: a path that is not under ${__dirname}, and one that is but names
// a file nobody stages.
func TestCommandOutsideTheBundleIsCaught(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{"absolute path", "/usr/local/bin/favro-mcp", "outside the bundle"},
		{"staged nothing", dirnameRef + "server/favro-mcp-typo", "does not carry"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := committedManifest(t)
			m.Server.MCPConfig.Command = tt.command
			problems := checkBundleManifest(m, mcpbStaged())
			if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, tt.want) }) {
				t.Errorf("command %q was not caught; problems were:\n%s", tt.command, strings.Join(problems, "\n"))
			}
		})
	}
}

// TestUndeclaredUserConfigReferenceIsCaught, including the composed form
// the standard singles out: checking only values that are nothing but
// one reference lets "${user_config.dir}/sub" through, and the server
// starts with the variable unsubstituted.
func TestUndeclaredUserConfigReferenceIsCaught(t *testing.T) {
	for _, value := range []string{"${user_config.nope}", "${user_config.nope}/sub"} {
		m := committedManifest(t)
		m.Server.MCPConfig.Env = map[string]string{"FAVRO_USER_EMAIL": value}
		problems := checkBundleManifest(m, mcpbStaged())
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, "user_config.nope") }) {
			t.Errorf("%q was not caught; problems were:\n%s", value, strings.Join(problems, "\n"))
		}
	}
}

// TestEntryPointMustBeStaged.
func TestEntryPointMustBeStaged(t *testing.T) {
	m := committedManifest(t)
	m.Server.EntryPoint = "server/not-packed"
	if problems := checkBundleManifest(m, mcpbStaged()); !slices.ContainsFunc(problems,
		func(p string) bool { return strings.Contains(p, "entry_point") }) {
		t.Errorf("an entry_point naming nothing staged was not caught: %v", problems)
	}

	m = committedManifest(t)
	m.Server.EntryPoint = ""
	if problems := checkBundleManifest(m, mcpbStaged()); len(problems) == 0 {
		t.Error("a manifest with no entry_point at all passed")
	}
}

// TestLauncherNamesWhatThePackerStages. The launcher is a shell script
// no manifest mentions, so every other check here passes while it
// dispatches to a name nothing stages.
func TestLauncherNamesWhatThePackerStages(t *testing.T) {
	if problems := mcpbLauncherAgrees(); len(problems) > 0 {
		t.Errorf("the generated launcher disagrees with the packer:\n%s", strings.Join(problems, "\n"))
	}

	script := mcpbLauncherScript()
	staged := mcpbStaged()
	for _, b := range mcpbBinaries {
		if len(b.uname) == 0 {
			continue
		}
		if !slices.Contains(staged, b.name) {
			t.Errorf("the launcher's table names %s and the packer does not stage it", b.name)
		}
	}
	if !strings.Contains(script, "exec ") {
		t.Error("the launcher must exec: the server talks MCP over that process's stdio, and a " +
			"shell left in the middle owns the pipes")
	}
	if !strings.Contains(script, ">&2") {
		t.Error("the unsupported-machine message must go to stderr; a line of English on stdout " +
			"corrupts the JSON-RPC session before the client's first request completes")
	}
}

// TestEveryClaimedPlatformIsStagedFor — the table behind the check, held
// to the manifest from the other side.
func TestEveryClaimedPlatformIsStagedFor(t *testing.T) {
	want := mcpbPlatformFile()
	staged := mcpbStaged()
	m := committedManifest(t)
	if len(m.Compatibility.Platforms) == 0 {
		t.Fatal("the manifest claims no platforms; this test is reading nothing")
	}
	for _, platform := range m.Compatibility.Platforms {
		file, ok := want[platform]
		if !ok {
			t.Errorf("the manifest claims %s and mcpbPlatformFile names no file for it", platform)
			continue
		}
		if !slices.Contains(staged, file) {
			t.Errorf("%s is meant to run %s, which the packer does not stage", platform, file)
		}
	}
}

// TestRenderBundleManifestRefusesAHandEditedVersion, and stamps a real
// one without disturbing anything else in the file.
func TestRenderBundleManifestRefusesAHandEditedVersion(t *testing.T) {
	source := filepath.Join("..", "..", mcpbManifestPath)

	out, err := renderBundleManifest("1.2.3", source)
	if err != nil {
		t.Fatalf("renderBundleManifest: %v", err)
	}
	var m bundleManifest
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("the rendered manifest is not JSON: %v", err)
	}
	if m.Version != "1.2.3" {
		t.Errorf("version = %q; want 1.2.3", m.Version)
	}
	// Re-encoding a map would sort the keys and escape < > &; the edit
	// is on the bytes so that neither happens.
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(raw) && len(out) != len(raw)+len("1.2.3")-len(placeholderVersion) {
		t.Errorf("the rendered manifest is %d bytes and the source is %d; the substitution changed "+
			"more than the version", len(out), len(raw))
	}

	// A version that is the placeholder, and a manifest already carrying
	// a real version, are both refused.
	if _, err := renderBundleManifest(placeholderVersion, source); err == nil {
		t.Error("renderBundleManifest stamped the placeholder as a release version")
	}
	edited := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(edited, out, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := renderBundleManifest("2.0.0", edited); err == nil {
		t.Error("renderBundleManifest packed a manifest that was edited by hand")
	}
	if _, err := renderBundleManifest("", source); err == nil {
		t.Error("renderBundleManifest accepted an empty version")
	}
}

// TestMcpbPackWritesAnInstallableBundle drives the packer against a
// synthetic dist, then reads the zip back: the manifest at the root, the
// mode on every staged binary, and one entry per file.
func TestMcpbPackWritesAnInstallableBundle(t *testing.T) {
	dist := fakeHookDist(t, "9.9.9")

	var out sink
	if err := mcpbPack(&out, []string{"v9.9.9", dist}); err != nil {
		t.Fatalf("mcpbPack: %v", err)
	}
	out.mustSay(t, "9.9.9")

	zr, err := zip.OpenReader(filepath.Join(dist, "favro-mcp_9.9.9.mcpb"))
	if err != nil {
		t.Fatalf("the packer wrote no bundle: %v", err)
	}
	defer func() { _ = zr.Close() }()

	modes := map[string]os.FileMode{}
	for _, f := range zr.File {
		modes[f.Name] = f.Mode().Perm()
	}
	if _, ok := modes[mcpbManifestName]; !ok {
		t.Error("the bundle has no manifest at its root; an installer looks there and nowhere else")
	}
	for _, name := range mcpbStaged() {
		mode, ok := modes[name]
		if !ok {
			t.Errorf("the bundle does not carry %s", name)
			continue
		}
		// The mode has to be set on every staged binary, not only the
		// entry point: a zip carries the mode, and the Windows binary
		// arrives unrunnable without it.
		if strings.HasPrefix(name, mcpbServerDir) && mode&0o111 == 0 {
			t.Errorf("%s is staged %v; everything under %s is something the bundle runs", name, mode, mcpbServerDir)
		}
	}

	// The version in the packed manifest is the one asked for, which is
	// one of the five places a release has to agree about.
	f, err := zr.Open(mcpbManifestName)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var m bundleManifest
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m.Version != "9.9.9" {
		t.Errorf("the packed manifest says version %q; want 9.9.9", m.Version)
	}
}

// TestMcpbPackRefusesAnAmbiguousGlob. The layout under dist/ carries a
// build id and an amd64 variant, and a bundle built from the wrong
// binary is not something a checksum catches.
// fakeHookDist is dist/ as it looks when the post hook runs: the
// directory per build that goreleaser writes, carrying the build id and,
// for amd64, a variant suffix — and no artifacts.json, because
// goreleaser writes that after the archives. That absence is why the
// packer globs where plugin.go reads a manifest, and it is why this
// helper is not plugin_test.go's fakeDist.
//
// Every staged binary is an executable script reporting `version` rather
// than an inert file: the packer asks the binary for the host platform
// what version it thinks it is, which is the one of a bundle's five
// version claims checkable at pack time.
func fakeHookDist(t *testing.T, version string) string {
	t.Helper()
	dist := t.TempDir()
	for _, p := range []string{
		"favro-mcp_darwin_all/favro-mcp",
		"favro-mcp_windows_amd64_v1/favro-mcp.exe",
		"favro-mcp_linux_amd64_v1/favro-mcp",
		"favro-mcp_linux_arm64/favro-mcp",
	} {
		full := filepath.Join(dist, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("not a real binary"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The host's own staged binary has to be a REAL executable, because
	// the packer asks it for its version. A shell script named
	// favro-mcp.exe is not something Windows will run, which is how this
	// fixture passed on Linux and macOS and failed on the third runner.
	buildHostStub(t, filepath.Join(dist, hostStagedPath()), version)
	return dist
}

// hostStagedPath is where fakeHookDist puts the binary versionsAgree
// will execute: the one whose goarch matches this machine, with the
// darwin universal binary standing in for both macOS architectures.
func hostStagedPath() string {
	host := runtime.GOOS + "-" + runtime.GOARCH
	switch {
	case runtime.GOOS == "darwin":
		return filepath.Join("favro-mcp_darwin_all", "favro-mcp")
	case host == "windows-amd64":
		return filepath.Join("favro-mcp_windows_amd64_v1", "favro-mcp.exe")
	case host == "linux-amd64":
		return filepath.Join("favro-mcp_linux_amd64_v1", "favro-mcp")
	default:
		return filepath.Join("favro-mcp_linux_arm64", "favro-mcp")
	}
}

// buildHostStub compiles a program that answers --version the way the
// real server does. Compiled rather than scripted so it runs on every
// platform the test matrix covers.
func buildHostStub(t *testing.T, out, version string) {
	t.Helper()
	// fakeHookDist staged a placeholder here first, and `go build -o`
	// refuses to overwrite a file it did not produce.
	if err := os.Remove(out); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "main.go")
	program := "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"favro-mcp " + version + " (test)\") }\n"
	if err := os.WriteFile(src, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", out, src)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the stub server: %v\n%s", err, combined)
	}
}

// TestMcpbPackRefusesABinaryThatDisagreesAboutItsVersion. A bundle whose
// binary disagrees with its manifest is indistinguishable from a correct
// one until somebody runs it.
func TestMcpbPackRefusesABinaryThatDisagreesAboutItsVersion(t *testing.T) {
	dist := fakeHookDist(t, "1.0.0")
	var out sink
	err := mcpbPack(&out, []string{"v2.0.0", dist})
	if err == nil {
		t.Fatal("the packer stamped 2.0.0 into a manifest beside a binary reporting 1.0.0")
	}
	if !strings.Contains(err.Error(), "2.0.0") {
		t.Errorf("the error does not name the version being stamped: %v", err)
	}
}

func TestMcpbPackRefusesAnAmbiguousGlob(t *testing.T) {
	dist := t.TempDir()
	for _, p := range []string{
		"favro-mcp_darwin_all/favro-mcp",
		"other_darwin_all/favro-mcp",
	} {
		full := filepath.Join(dist, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out sink
	err := mcpbPack(&out, []string{"v1.0.0", dist})
	if err == nil {
		t.Fatal("the packer accepted a glob matching two darwin binaries")
	}
	if !strings.Contains(err.Error(), "exactly one") {
		t.Errorf("the error does not say the glob was ambiguous: %v", err)
	}
}

// TestMcpbGateReportsHowMuchItRead.
func TestMcpbGateReportsHowMuchItRead(t *testing.T) {
	var out sink
	if err := mcpbGate(&out, nil); err != nil {
		t.Fatalf("mcpbGate: %v", err)
	}
	out.mustSay(t, "files staged")
	out.mustSay(t, "platforms")
}

// TestManifestEnvNamesAreNamesTheServerReads. The bundle is how most
// people will configure this server, and an env name it stopped reading
// produces an install that collects a value and starts a server that
// never sees it.
func TestManifestEnvNamesAreNamesTheServerReads(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	m := committedManifest(t)
	if len(m.env()) == 0 {
		t.Fatal("the manifest sets no environment variables; this test is reading nothing")
	}
	if problems := mcpbEnvIsReal(root, m); len(problems) > 0 {
		t.Errorf("%s", strings.Join(problems, "\n"))
	}

	m.Server.MCPConfig.Env["FAVRO_NOT_A_REAL_SETTING"] = "x"
	if problems := mcpbEnvIsReal(root, m); len(problems) == 0 {
		t.Error("an environment variable no source file reads was not caught")
	}
}

// TestDroppingAClaimedPlatformIsCaught is the mirror of the win32 case:
// the packer stages a binary for darwin, and a manifest that stops
// claiming darwin passes every other check in this file while shipping
// that binary as dead weight to a platform that cannot install it.
func TestDroppingAClaimedPlatformIsCaught(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "win32"} {
		t.Run(platform, func(t *testing.T) {
			m := committedManifest(t)
			m.Compatibility.Platforms = slices.DeleteFunc(m.Compatibility.Platforms,
				func(p string) bool { return p == platform })
			delete(m.Server.MCPConfig.PlatformOverrides, platform)

			problems := checkBundleManifest(m, mcpbStaged())
			if !slices.ContainsFunc(problems, func(p string) bool {
				return strings.Contains(p, platform) && strings.Contains(p, "dead weight")
			}) {
				t.Errorf("dropping %s was not caught; problems were:\n%s",
					platform, strings.Join(problems, "\n"))
			}
		})
	}
}

// TestAManifestThatConfiguresNothingIsCaught. checkBundleManifest is
// what the packer runs at release time, where no test is present, and
// every referential loop in it iterates a collection the manifest
// supplies — so an almost-empty manifest produced no problems at all.
func TestAManifestThatConfiguresNothingIsCaught(t *testing.T) {
	var m bundleManifest
	m.Server.EntryPoint = mcpbServerDir + "favro-mcp"
	m.Server.MCPConfig.Command = dirnameRef + mcpbServerDir + "favro-mcp"

	problems := checkBundleManifest(m, mcpbStaged())
	for _, want := range []string{"compatibility.platforms", "user_config", "environment", "platform_overrides"} {
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, want) }) {
			t.Errorf("an empty manifest did not fail on %s; problems were:\n%s",
				want, strings.Join(problems, "\n"))
		}
	}
}

// TestReleaseWiringIsHeld drives each requirement by removing it from a
// synthetic block set. The four lines it covers are invisible to every
// other check here: delete checksum.extra_files and the release
// succeeds, make check passes, and the bundle ships outside the
// signature while README says it is covered.
func TestReleaseWiringIsHeld(t *testing.T) {
	whole, err := goreleaserBlocks(repoRoot(t))
	if err != nil {
		t.Fatalf("goreleaserBlocks: %v", err)
	}
	problems, checked := mcpbReleaseWiring(whole)
	if len(problems) > 0 {
		t.Fatalf("the committed .goreleaser.yaml does not satisfy its own wiring check:\n%s",
			strings.Join(problems, "\n"))
	}
	if checked < 8 {
		t.Errorf("the wiring check made %d assertions; it is not reading the file", checked)
	}

	// Every block it depends on, removed one at a time.
	for _, block := range []string{"universal_binaries", "checksum", "release", "signs", "sboms", "builds", "archives"} {
		t.Run("without "+block, func(t *testing.T) {
			partial := map[string]string{}
			for k, v := range whole {
				if k != block {
					partial[k] = v
				}
			}
			if got, _ := mcpbReleaseWiring(partial); len(got) == 0 {
				t.Errorf("removing the %s block was not caught", block)
			}
		})
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}
