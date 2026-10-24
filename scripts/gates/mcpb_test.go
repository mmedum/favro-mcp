package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/v4/internal/config"
)

// repoFile reads a path relative to the repository root.
func repoFile(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return data
}

// committed is the manifest as the repository carries it, decoded. Every
// mutation test starts from it, so a test cannot pass by asserting
// against a shape the real manifest does not have.
func committed(t *testing.T) bundleManifest {
	t.Helper()
	var m bundleManifest
	if err := json.Unmarshal(repoFile(t, mcpbManifestPath), &m); err != nil {
		t.Fatalf("decode %s: %v", mcpbManifestPath, err)
	}
	return m
}

// editedRaw is the committed manifest with one edit applied to it as a
// document, encoded again.
func editedRaw(t *testing.T, edit func(map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(repoFile(t, mcpbManifestPath), &doc); err != nil {
		t.Fatal(err)
	}
	edit(doc)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// edited is editedRaw read back the way the gate reads it.
func edited(t *testing.T, edit func(map[string]any)) bundleManifest {
	t.Helper()
	var m bundleManifest
	if err := json.Unmarshal(editedRaw(t, edit), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// dig is the object at a path in a decoded manifest.
func dig(doc map[string]any, path ...string) map[string]any {
	for _, key := range path {
		doc = doc[key].(map[string]any)
	}
	return doc
}

// copiedRoot is a temporary root holding every file committedManifest
// reads, copied from the repository, so a test can break one of them.
func copiedRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range []string{mcpbManifestPath, "go.mod", "LICENSE", ".goreleaser.yaml"} {
		put(t, root, rel, string(repoFile(t, rel)))
	}
	return root
}

// put writes body to rel under root.
func put(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// has reports whether one of problems contains want.
func has(problems []string, want string) bool {
	return slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, want) })
}

// TestCommittedManifestPasses is the floor under every mutation test
// below: they prove a broken manifest is caught, and this proves the
// real one passes every check the gate and the packer run, read from the
// repository root.
func TestCommittedManifestPasses(t *testing.T) {
	_, _, problems, err := committedManifest(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 {
		t.Errorf("the committed manifest does not pass its own check:\n%s", strings.Join(problems, "\n"))
	}
}

// committedManifest runs every check on the files under the root it is
// given: each break in a copy of the repository is refused, and refused
// alone, so no check is reached only by way of another.
func TestManifestProblemsRunsEveryCheck(t *testing.T) {
	for _, tc := range []struct {
		name string
		// file is the one broken; edit applies to the manifest, and old
		// and new to the file as text.
		file     string
		edit     func(map[string]any)
		old, new string
		want     string
	}{
		{
			name: "a version in the tree", file: mcpbManifestPath,
			edit: func(d map[string]any) { d["version"] = "1.2.3" }, want: `says version "1.2.3"`,
		},
		{name: "a schema at a branch", file: mcpbManifestPath, edit: func(d map[string]any) {
			d["$schema"] = "https://raw.githubusercontent.com/anthropics/mcpb/main/schemas/mcpb-manifest-v0.3.schema.json"
		}, want: "can be re-pointed"},
		{
			name: "a document the schema refuses", file: mcpbManifestPath,
			edit: func(d map[string]any) { d["keywords"] = "favro" },
			want: mcpbManifestPath + " does not satisfy mcpb-manifest-v0.3.schema.json",
		},
		{
			name: "another name", file: mcpbManifestPath, edit: func(d map[string]any) { d["name"] = "favro" },
			want: `the manifest's name is "favro" and the server's is "favro-mcp"`,
		},
		{
			name: "another server type", file: mcpbManifestPath,
			edit: func(d map[string]any) { dig(d, "server")["type"] = "node" },
			want: `server.type is "node"; the bundle carries a binary`,
		},
		// The decode into bundleManifest merges the two, so every check
		// sees the env and the overrides; the schema and the host keep
		// the second, which has neither.
		{
			name: "a second server object", file: mcpbManifestPath, old: `  "user_config": {`,
			new: `  "server": {"type": "binary", "entry_point": "server/favro-mcp-darwin", ` +
				`"mcp_config": {"command": "${__dirname}/server/favro-mcp-darwin", "args": []}},` +
				"\n" + `  "user_config": {`,
			want: "has server more than once",
		},
		{
			name: "a variable mapped twice", file: mcpbManifestPath,
			old:  `"FAVRO_UPLOAD_DIR": "${user_config.upload_dir}"`,
			new:  `"FAVRO_UPLOAD_DIR": "${user_config.upload_dir}", "FAVRO_UPLOAD_DIR": "${user_config.upload_dir}"`,
			want: "has server.mcp_config.env.FAVRO_UPLOAD_DIR more than once",
		},
		{
			name: "a go.mod at another major version", file: "go.mod", old: "/v4", new: "/v5",
			want: `the launcher says "go install github.com/mmedum/favro-mcp/v4/cmd/favro-mcp@latest", ` +
				`and go.mod's module gives "go install github.com/mmedum/favro-mcp/v5/cmd/favro-mcp@latest"`,
		},
		{
			name: "another license", file: "LICENSE", old: "Apache License", new: "MIT License",
			want: `the manifest says the license is "Apache-2.0" and LICENSE is MIT`,
		},
		// The archive a downstream user found missing.
		{
			name: "windows arm64 ignored", file: ".goreleaser.yaml", old: "      - arm64\n",
			new:  "      - arm64\n    ignore:\n      - goos: windows\n        goarch: arm64\n",
			want: ".goreleaser.yaml does not build windows-arm64, so the release carries no windows_arm64 archive",
		},
		{
			name: "no checksum glob", file: ".goreleaser.yaml", old: "    - glob: ./dist/*.mcpb\n\n# One SBOM",
			new: "\n# One SBOM", want: `.goreleaser.yaml's checksum block does not mention ".mcpb"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := copiedRoot(t)
			if tc.edit != nil {
				put(t, root, tc.file, string(editedRaw(t, tc.edit)))
			} else {
				// A Windows checkout carries CRLF, and the edits are
				// written with LF.
				body := strings.ReplaceAll(string(repoFile(t, tc.file)), "\r\n", "\n")
				if !strings.Contains(body, tc.old) {
					t.Fatalf("%s does not say %q", tc.file, tc.old)
				}
				put(t, root, tc.file, strings.ReplaceAll(body, tc.old, tc.new))
			}
			_, _, problems, err := committedManifest(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(problems) != 1 || !strings.Contains(problems[0], tc.want) {
				t.Errorf("problems:\n%s\nwant the one containing %q", strings.Join(problems, "\n"), tc.want)
			}
		})
	}
}

// A key is repeated only within one object, and each repeated key is
// named once, by its path, a list's elements and a repeated object's
// own keys included.
func TestRepeatedKeysNameEachPathOnce(t *testing.T) {
	for _, tc := range []struct {
		doc  string
		want []string
	}{
		{`{"a": [{"b": 1, "b": 2, "b": 3}], "c": {"d": 1}, "d": 1}`, []string{"a[0].b"}},
		{`{"x": {"y": 1}, "x": {"y": 1, "y": 2}}`, []string{"x", "x.y"}},
		{`{"server": {"type": "binary"}, "user_config": {"type": {"type": "boolean"}}}`, nil},
		{`{"a": 1,`, nil},
	} {
		if got := repeatedKeys([]byte(tc.doc)); !slices.Equal(got, tc.want) {
			t.Errorf("repeatedKeys(%s) = %q, want %q", tc.doc, got, tc.want)
		}
	}
}

// TestDeletingTheWin32OverrideIsCaught is the check the shared standard
// names last, and the one a review found missing in a packer that had
// every other check in this file.
//
// Deleting the win32 override leaves a manifest where every command
// names a program the bundle really carries. Windows then runs the
// default command, which is the macOS universal binary. Nothing about
// the document is malformed and nothing about the staged tree is
// missing.
func TestDeletingTheWin32OverrideIsCaught(t *testing.T) {
	m := committed(t)
	delete(m.Server.MCPConfig.PlatformOverrides, "win32")

	problems := checkBundleManifest(m, mcpbPrograms())
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
	m := committed(t)
	m.Compatibility.Platforms = slices.DeleteFunc(m.Compatibility.Platforms,
		func(p string) bool { return p == "linux" })

	problems := checkBundleManifest(m, mcpbPrograms())
	if !has(problems, "can ever reach") || !has(problems, "linux") {
		t.Errorf("an override no platform can reach was not caught; problems were:\n%s",
			strings.Join(problems, "\n"))
	}
}

// A command starts with ${__dirname}/ and names a program the packer
// stages: never a path outside the bundle, never a path the host would
// have to resolve against something, and never a file packed to be read.
func TestACommandNamesAStagedProgram(t *testing.T) {
	for _, tc := range []struct{ name, command, want string }{
		{"absolute path", "/usr/local/bin/favro-mcp", "a command starts with ${__dirname}/"},
		{"relative path", "server/favro-mcp-darwin", "a command starts with ${__dirname}/"},
		{
			"staged nothing", dirnameRef + "server/favro-mcp-typo",
			"the default command names server/favro-mcp-typo, which is not a program the packer stages",
		},
		{"the README", dirnameRef + "README.md", "names README.md, which is not a program the packer stages"},
		{"the manifest", dirnameRef + "manifest.json", "names manifest.json, which is not a program"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := committed(t)
			m.Server.MCPConfig.Command = tc.command
			if problems := checkBundleManifest(m, mcpbPrograms()); !has(problems, tc.want) {
				t.Errorf("command %q: want %q, problems were:\n%s", tc.command, tc.want, strings.Join(problems, "\n"))
			}
		})
	}
	// An override's command is held the same way.
	m := committed(t)
	o := m.Server.MCPConfig.PlatformOverrides["win32"]
	o.Command = dirnameRef + "NOTICE"
	m.Server.MCPConfig.PlatformOverrides["win32"] = o
	if problems := checkBundleManifest(m, mcpbPrograms()); !has(problems,
		"the win32 command names NOTICE, which is not a program the packer stages") {
		t.Errorf("an override naming NOTICE passed:\n%s", strings.Join(problems, "\n"))
	}
}

// entry_point names a program the packer stages, and the one the default
// command runs.
func TestEntryPointIsTheDefaultCommandsProgram(t *testing.T) {
	for _, tc := range []struct{ name, entry, want string }{
		{"staged nothing", "server/not-packed", "entry_point names server/not-packed, which is not a program"},
		{"the README", "README.md", "entry_point names README.md, which is not a program"},
		{"none", "", "the manifest declares no entry_point"},
		{
			"another program", mcpbLauncher,
			"entry_point is server/launch-linux.sh, and the default command runs server/favro-mcp-darwin",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := committed(t)
			m.Server.EntryPoint = tc.entry
			if problems := checkBundleManifest(m, mcpbPrograms()); !has(problems, tc.want) {
				t.Errorf("entry_point %q: want %q, problems were:\n%s", tc.entry, tc.want, strings.Join(problems, "\n"))
			}
		})
	}
}

// The programs are the binaries and the launcher, and nothing packed to
// be read.
func TestTheProgramsAreTheBinariesAndTheLauncher(t *testing.T) {
	want := []string{
		"server/favro-mcp-darwin", "server/favro-mcp-linux-amd64", "server/favro-mcp-linux-arm64",
		"server/favro-mcp.exe", "server/launch-linux.sh",
	}
	if got := mcpbPrograms(); !slices.Equal(got, want) {
		t.Errorf("mcpbPrograms = %q, want %q", got, want)
	}
}

// TestUndeclaredUserConfigReferenceIsCaught, including the composed form
// the standard singles out: checking only values that are nothing but
// one reference lets "${user_config.dir}/sub" through, and the server
// starts with the variable unsubstituted.
func TestUndeclaredUserConfigReferenceIsCaught(t *testing.T) {
	for _, value := range []string{"${user_config.nope}", "${user_config.nope}/sub"} {
		m := committed(t)
		m.Server.MCPConfig.Env = map[string]string{"FAVRO_USER_EMAIL": value}
		if problems := checkBundleManifest(m, mcpbPrograms()); !has(problems, "user_config.nope") {
			t.Errorf("%q was not caught; problems were:\n%s", value, strings.Join(problems, "\n"))
		}
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
	programs := mcpbPrograms()
	m := committed(t)
	if len(m.Compatibility.Platforms) == 0 {
		t.Fatal("the manifest claims no platforms; this test is reading nothing")
	}
	for _, platform := range m.Compatibility.Platforms {
		file, ok := want[platform]
		if !ok {
			t.Errorf("the manifest claims %s and mcpbPlatformFile names no file for it", platform)
			continue
		}
		if !slices.Contains(programs, file) {
			t.Errorf("%s is meant to run %s, which the packer does not stage as a program", platform, file)
		}
	}
}

// TestStampVersionRefusesAHandEditedVersion, and stamps a real one
// without disturbing anything else in the file.
func TestStampVersionRefusesAHandEditedVersion(t *testing.T) {
	raw := repoFile(t, mcpbManifestPath)

	out, err := stampVersion(raw, "1.2.3", mcpbManifestPath)
	if err != nil {
		t.Fatalf("stampVersion: %v", err)
	}
	var m bundleManifest
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("the stamped manifest is not JSON: %v", err)
	}
	if m.Version != "1.2.3" {
		t.Errorf("version = %q; want 1.2.3", m.Version)
	}
	// Re-encoding a map would sort the keys and escape < > &; the edit
	// is on the bytes so that neither happens.
	if len(out) != len(raw)+len("1.2.3")-len(placeholderVersion) {
		t.Errorf("the stamped manifest is %d bytes and the source is %d; the substitution changed "+
			"more than the version", len(out), len(raw))
	}

	// A version that is the placeholder, and a manifest already carrying
	// a real version, are both refused.
	if _, err := stampVersion(raw, placeholderVersion, mcpbManifestPath); err == nil {
		t.Error("stampVersion stamped the placeholder as a release version")
	}
	if _, err := stampVersion(out, "2.0.0", mcpbManifestPath); err == nil {
		t.Error("stampVersion packed a manifest that was edited by hand")
	}
	if _, err := stampVersion(raw, "", mcpbManifestPath); err == nil {
		t.Error("stampVersion accepted an empty version")
	}
}

// What ships is held to the vendored schema, not only what is committed.
func TestTheStampedManifestIsHeldToTheSchema(t *testing.T) {
	if _, err := stampedManifest(repoFile(t, mcpbManifestPath), "1.2.3"); err != nil {
		t.Fatalf("the committed manifest does not stamp: %v", err)
	}
	_, err := stampedManifest(editedRaw(t, func(d map[string]any) { d["keywords"] = "favro" }), "1.2.3")
	if want := "the stamped manifest does not satisfy mcpb-manifest-v0.3.schema.json"; err == nil ||
		!strings.Contains(err.Error(), want) {
		t.Errorf("stamping a document the schema refuses gave %v, want an error containing %q", err, want)
	}
}

// The vendored schema is the copy whose digest is recorded, and it reads
// as a real schema: it refuses a document missing what the format
// requires.
func TestTheVendoredSchemaIsTheRecordedOne(t *testing.T) {
	if err := validateDocument(mcpbSchemaFile, "an empty object", []byte(`{}`)); err == nil {
		t.Error("the vendored schema accepted an empty object, so it is not reading the format")
	}
	if err := validateDocument("mcpb-manifest-v9.9.schema.json", "x", []byte(`{}`)); err == nil ||
		!strings.Contains(err.Error(), "no vendored schema") {
		t.Errorf("an unknown schema name gave %v", err)
	}
	saved := vendoredSchemas[mcpbSchemaFile]
	t.Cleanup(func() { vendoredSchemas[mcpbSchemaFile] = saved })
	vendoredSchemas[mcpbSchemaFile] = vendoredSchema{source: saved.source, sha256: strings.Repeat("0", 64)}
	if _, err := loadSchema(mcpbSchemaFile); err == nil || !strings.Contains(err.Error(), "the recorded digest is") {
		t.Errorf("a schema whose bytes differ from the recorded digest loaded: %v", err)
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

// fakeHookDist is dist/ as it looks when the post hook runs: the
// directory per build that goreleaser writes, carrying the build id and,
// for amd64, a variant suffix — and no artifacts.json, because
// goreleaser writes that after the archives. That absence is why the
// packer globs where plugin.go reads a manifest, and it is why this
// helper is not plugin_test.go's fakeDist. The windows arm64 build is
// there too, as goreleaser writes it, and the bundle does not take it.
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
		"favro-mcp_windows_arm64_v8.0/favro-mcp.exe",
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

// TestMcpbPackRefusesAnAmbiguousGlob. The layout under dist/ carries a
// build id and an amd64 variant, and a bundle built from the wrong
// binary is not something a checksum catches.
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

// The packer runs the gate's checks, from the same root: a manifest the
// gate refuses is not packed.
func TestMcpbPackRunsEveryCheckTheGateRuns(t *testing.T) {
	root := copiedRoot(t)
	for _, rel := range []string{"NOTICE", "README.md"} {
		put(t, root, rel, string(repoFile(t, rel)))
	}
	t.Chdir(root)
	dist := fakeHookDist(t, "1.0.0")
	var out sink
	if err := mcpbPack(&out, []string{"v1.0.0", dist}); err != nil {
		t.Fatalf("the copied repository does not pack: %v", err)
	}
	put(t, root, mcpbManifestPath, strings.Replace(string(repoFile(t, mcpbManifestPath)),
		`"args": []`, `"args": ["--dry-run"]`, 1))
	err := mcpbPack(&out, []string{"v1.0.0", dist})
	if want := `mcp_config.args is ["--dry-run"]`; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("pack = %v, want it refused, naming %q", err, want)
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
	out.mustSay(t, "variables the server reads")
}

// TestDroppingAClaimedPlatformIsCaught is the mirror of the win32 case:
// the packer stages a binary for darwin, and a manifest that stops
// claiming darwin passes every other check in this file while shipping
// that binary as dead weight to a platform that cannot install it.
func TestDroppingAClaimedPlatformIsCaught(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "win32"} {
		t.Run(platform, func(t *testing.T) {
			m := committed(t)
			m.Compatibility.Platforms = slices.DeleteFunc(m.Compatibility.Platforms,
				func(p string) bool { return p == platform })
			delete(m.Server.MCPConfig.PlatformOverrides, platform)

			problems := checkBundleManifest(m, mcpbPrograms())
			if !slices.ContainsFunc(problems, func(p string) bool {
				return strings.Contains(p, platform) && strings.Contains(p, "dead weight")
			}) {
				t.Errorf("dropping %s was not caught; problems were:\n%s",
					platform, strings.Join(problems, "\n"))
			}
		})
	}
}

// TestAManifestThatConfiguresNothingIsCaught. Every referential loop in
// checkBundleManifest iterates a collection the manifest supplies — so
// an almost-empty manifest produced no problems at all.
func TestAManifestThatConfiguresNothingIsCaught(t *testing.T) {
	var m bundleManifest
	m.Server.EntryPoint = mcpbServerDir + "favro-mcp-darwin"
	m.Server.MCPConfig.Command = dirnameRef + mcpbServerDir + "favro-mcp-darwin"

	problems := checkBundleManifest(m, mcpbPrograms())
	for _, want := range []string{"compatibility.platforms", "user_config", "environment", "platform_overrides"} {
		if !has(problems, want) {
			t.Errorf("an empty manifest did not fail on %s; problems were:\n%s",
				want, strings.Join(problems, "\n"))
		}
	}
}

// What the manifest says it is: the server's own name, a binary, each
// platform once, and a privacy policy.
func TestTheIdentityRefusesEachBreak(t *testing.T) {
	if problems := identityProblems(committed(t)); len(problems) > 0 {
		t.Fatalf("the committed manifest: %q", problems)
	}
	for _, tc := range []struct {
		name   string
		breaks func(*bundleManifest)
		want   string
	}{
		{
			"another name", func(m *bundleManifest) { m.Name = "favro" },
			`the manifest's name is "favro" and the server's is "favro-mcp"`,
		},
		{
			"another type", func(m *bundleManifest) { m.Server.Type = "node" },
			`server.type is "node"; the bundle carries a binary`,
		},
		{"a platform twice", func(m *bundleManifest) {
			m.Compatibility.Platforms = append(m.Compatibility.Platforms, "linux")
		}, "compatibility.platforms is [darwin linux linux win32], which claims a platform twice"},
		{"no privacy policy", func(m *bundleManifest) { m.PrivacyPolicies = nil }, "privacy_policies is empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := committed(t)
			tc.breaks(&m)
			if got := identityProblems(m); len(got) != 1 || !strings.HasPrefix(got[0], tc.want) {
				t.Errorf("got %q, want the one problem %q", got, tc.want)
			}
		})
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

// The release carries the family's six archives: darwin, linux and
// windows, each on amd64 and arm64. A downstream user found the
// windows_arm64 one missing, because .goreleaser.yaml ignored the pair.
func TestReleaseArchivesAreTheFamilysSix(t *testing.T) {
	built, err := goreleaserMatrix(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if problems := releaseArchiveProblems(built); len(problems) > 0 {
		t.Errorf("the committed matrix: %q", problems)
	}
	short := slices.DeleteFunc(slices.Clone(built), func(p string) bool { return p == "windows-arm64" })
	want := []string{".goreleaser.yaml does not build windows-arm64, so the release carries no windows_arm64 " +
		"archive; the family's releases carry six, darwin, linux and windows on amd64 and arm64 each"}
	if got := releaseArchiveProblems(short); !slices.Equal(got, want) {
		t.Errorf("a matrix without windows-arm64 gives %q, want %q", got, want)
	}
	if got := releaseArchiveProblems(nil); len(got) != 6 {
		t.Errorf("an empty matrix gives %d problems, want 6: %q", len(got), got)
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

// What the manifest claims about ITSELF: which format version it is,
// which document defines that version, and where a failing install is
// reported.
//
// Every case below is well formed against the staged tree, and the last
// one passes every claim that holds the document against itself — a 0.2
// manifest citing a 0.2 schema is stale and entirely consistent, which
// is why the floor exists separately.
func TestTheManifestsOwnDeclarationIsHeld(t *testing.T) {
	const pinned = "https://raw.githubusercontent.com/anthropics/mcpb/v2.1.2/schemas/mcpb-manifest-v0.3.schema.json"

	// A commit SHA is the other ref that cannot move, and the stronger
	// of the two: refusing it would push somebody back to a branch.
	bySHA := committed(t)
	bySHA.Schema = "https://raw.githubusercontent.com/anthropics/mcpb/" +
		"0123456789abcdef0123456789abcdef01234567/schemas/mcpb-manifest-v0.3.schema.json"
	if problems := bySHA.declarationProblems(); len(problems) > 0 {
		t.Fatalf("a schema pinned to a commit was refused:\n%s", strings.Join(problems, "\n"))
	}

	cases := []struct {
		name   string
		breaks func(m *bundleManifest)
		want   string
	}{
		{"no $schema", func(m *bundleManifest) { m.Schema = "" }, "no $schema"},
		{
			"the unpinned dist path",
			func(m *bundleManifest) {
				m.Schema = "https://raw.githubusercontent.com/anthropics/mcpb/main/dist/mcpb-manifest.schema.json"
			},
			"not upstream's published",
		},
		{
			"served from a branch",
			func(m *bundleManifest) {
				m.Schema = "https://raw.githubusercontent.com/anthropics/mcpb/main/schemas/mcpb-manifest-v0.3.schema.json"
			},
			"can be re-pointed",
		},
		{
			// What refusing branch NAMES passes: a tag upstream moves
			// as it releases, which reads as pinned.
			"a partial tag",
			func(m *bundleManifest) {
				m.Schema = "https://raw.githubusercontent.com/anthropics/mcpb/v2.1/schemas/mcpb-manifest-v0.3.schema.json"
			},
			"can be re-pointed",
		},
		{
			"the right file from another host",
			func(m *bundleManifest) {
				m.Schema = "https://example.invalid/schemas/mcpb-manifest-v0.3.schema.json"
			},
			"not upstream's published",
		},
		{
			"a version that disagrees with the schema it cites",
			func(m *bundleManifest) { m.Schema = pinned; m.ManifestVersion = "0.4" },
			"cannot claim one version",
		},
		{
			"stale, and entirely self-consistent",
			func(m *bundleManifest) {
				m.Schema = "https://raw.githubusercontent.com/anthropics/mcpb/v2.1.2/schemas/mcpb-manifest-v0.2.schema.json"
				m.ManifestVersion = "0.2"
			},
			"still a manifest a version behind",
		},
		{"no support URL", func(m *bundleManifest) { m.Support = "" }, "no support URL"},
		{
			"no Claude Desktop version", func(m *bundleManifest) { m.Compatibility.ClaudeDesktop = "" },
			"compatibility.claude_desktop is empty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := committed(t)
			tc.breaks(&m)
			problems := m.declarationProblems()
			if len(problems) == 0 {
				t.Fatalf("%s was accepted", tc.name)
			}
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Fatalf("wanted %q, got:\n%s", tc.want, strings.Join(problems, "\n"))
			}
		})
	}
}

// The floor compares numbers, not text: "0.10" sorts before "0.3" as a
// string, a bug that waits for the tenth minor version and then reads
// like the manifest's fault.
func TestTheFloorComparesVersionsNumerically(t *testing.T) {
	for _, c := range []struct {
		version, floor string
		want           bool
	}{
		{"0.3", "0.3", false},
		{"0.2", "0.3", true},
		{"0.10", "0.3", false},
		{"0.9", "1.0", true},
		{"nonsense", "0.3", true},
	} {
		if got := olderThan(c.version, c.floor); got != c.want {
			t.Errorf("olderThan(%q, %q) = %v, want %v", c.version, c.floor, got, c.want)
		}
	}
}

// The committed manifest meets the floor, stated rather than read from
// the manifest: a test that takes its expected value from the thing
// under test passes on any value it finds there.
func TestTheCommittedManifestMeetsTheFloor(t *testing.T) {
	if got := committed(t).ManifestVersion; got != "0.3" {
		t.Fatalf("the committed manifest declares %q; this repository has checked 0.3", got)
	}
}

// What the bundle asks for, held to the server's own settings. Each case
// is one way a bundle install would differ from another client's
// configuration, starting from the committed manifest.
func TestTheBundleSettingsAreHeld(t *testing.T) {
	if problems := settingProblems(committed(t), bundleLeftOut); len(problems) > 0 {
		t.Fatalf("the committed manifest: %q", problems)
	}
	userConfig := func(d map[string]any, key string) map[string]any { return dig(d, "user_config", key) }
	env := func(d map[string]any) map[string]any { return dig(d, "server", "mcp_config", "env") }
	override := func(d map[string]any, platform string) map[string]any {
		return dig(d, "server", "mcp_config", "platform_overrides", platform)
	}
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{
			"a switch the bundle turns on", func(d map[string]any) { userConfig(d, "enable_destructive")["default"] = true },
			`the bundle's defaults configure the server otherwise than no environment does, in Destructive, ` +
				`from FAVRO_ENABLE_DESTRUCTIVE="true" FAVRO_UPLOAD_DIR=""`,
		},
		{"a switch asked as text", func(d map[string]any) {
			userConfig(d, "enable_destructive")["type"] = "string"
			userConfig(d, "enable_destructive")["default"] = "false"
		}, "user_config.enable_destructive is a string; FAVRO_ENABLE_DESTRUCTIVE is a switch, so ask for a boolean"},
		{
			"a switch with no default", func(d map[string]any) { delete(userConfig(d, "enable_destructive"), "default") },
			"mcp_config.env.FAVRO_ENABLE_DESTRUCTIVE spends ${user_config.enable_destructive}, which is optional with no default",
		},
		// The 4.0.0 bundle: a blank upload folder reached the server as
		// the text ${user_config.upload_dir}.
		{
			"an optional key with no default", func(d map[string]any) { delete(userConfig(d, "upload_dir"), "default") },
			"mcp_config.env.FAVRO_UPLOAD_DIR spends ${user_config.upload_dir}, which is optional with no default",
		},
		{
			"an optional key with a null default", func(d map[string]any) { userConfig(d, "upload_dir")["default"] = nil },
			"spends ${user_config.upload_dir}, which is optional with no default",
		},
		{
			"a key taking multiple values", func(d map[string]any) { userConfig(d, "upload_dir")["multiple"] = true },
			"spends ${user_config.upload_dir}, which takes multiple values",
		},
		{
			"defaults that configure the server", func(d map[string]any) { userConfig(d, "upload_dir")["default"] = "relative/dir" },
			`otherwise than no environment does, in Warnings, from FAVRO_ENABLE_DESTRUCTIVE="false" FAVRO_UPLOAD_DIR="relative/dir"`,
		},
		{"a setting the bundle no longer asks for", func(d map[string]any) {
			delete(dig(d, "user_config"), "enable_destructive")
			delete(env(d), "FAVRO_ENABLE_DESTRUCTIVE")
		}, "user_config does not ask for enable_destructive, which sets FAVRO_ENABLE_DESTRUCTIVE"},
		{
			"a key reaching another variable", func(d map[string]any) { env(d)["FAVRO_UPLOAD_DIR"] = "${user_config.user_email}" },
			`mcp_config.env.FAVRO_UPLOAD_DIR is "${user_config.user_email}", want "${user_config.upload_dir}"`,
		},
		{"a key no setting has", func(d map[string]any) {
			dig(d, "user_config")["log_level"] = map[string]any{"type": "string", "default": ""}
		}, "user_config asks for log_level, which is no setting's key"},
		{
			"a left-out variable set", func(d map[string]any) { env(d)["FAVRO_LOG_LEVEL"] = "debug" },
			"mcp_config.env sets FAVRO_LOG_LEVEL, which the bundle leaves out: " + diagnostics,
		},
		{
			"a removed variable set", func(d map[string]any) { env(d)["FAVRO_REQUIRE_PROMPT"] = "false" },
			"mcp_config.env sets FAVRO_REQUIRE_PROMPT, which a release removed",
		},
		{
			"a variable the server does not read", func(d map[string]any) { env(d)["FAVRO_NOT_A_SETTING"] = "x" },
			"mcp_config.env sets FAVRO_NOT_A_SETTING, which the server does not read",
		},
		{
			"no args", func(d map[string]any) { delete(dig(d, "server", "mcp_config"), "args") },
			"mcp_config.args is missing; keep it as []",
		},
		{
			"null args", func(d map[string]any) { dig(d, "server", "mcp_config")["args"] = nil },
			"mcp_config.args is null; keep it as []",
		},
		{"a flag in args", func(d map[string]any) {
			dig(d, "server", "mcp_config")["args"] = []any{"--dry-run"}
		}, `mcp_config.args is ["--dry-run"]; the bundle passes every setting through mcp_config.env`},
		// The host takes an override's env in place of the base's, so an
		// empty one unsets every variable on that platform.
		{
			"an empty override env", func(d map[string]any) { override(d, "win32")["env"] = map[string]any{} },
			"platform_overrides.win32.env is set; a host replaces mcp_config.env with it, an empty one too",
		},
		{
			"an empty override args", func(d map[string]any) { override(d, "linux")["args"] = []any{} },
			"platform_overrides.linux.args is set; a host replaces mcp_config.args with it, an empty one too",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := settingProblems(edited(t, tc.edit), bundleLeftOut); !has(got, tc.want) {
				t.Errorf("wanted %q, got:\n%s", tc.want, strings.Join(got, "\n"))
			}
		})
	}
}

// The settings the bundle asks for are every variable the server reads,
// less the removed ones and the left-out ones, and an exclusion the
// server has no use for is refused.
func TestBundleVarsAreDerivedFromTheSettings(t *testing.T) {
	asked, problems := bundleVarsFrom(config.Vars(), bundleLeftOut)
	if len(problems) > 0 {
		t.Fatalf("bundleLeftOut: %q", problems)
	}
	var names []string
	for _, v := range asked {
		names = append(names, v.Name)
	}
	want := []string{"FAVRO_API_TOKEN", "FAVRO_ENABLE_DESTRUCTIVE", "FAVRO_ORGANIZATION_ID", "FAVRO_UPLOAD_DIR", "FAVRO_USER_EMAIL"}
	if !slices.Equal(names, want) {
		t.Errorf("asked %q, want %q", names, want)
	}

	// A setting the server starts reading is asked for until the bundle
	// asks or a reason is written.
	added := append(config.Vars(), config.Var{Name: "FAVRO_NEW_SETTING"})
	if asked, _ := bundleVarsFrom(added, bundleLeftOut); !slices.ContainsFunc(asked,
		func(v config.Var) bool { return v.Name == "FAVRO_NEW_SETTING" }) {
		t.Error("a new setting is not asked for")
	}
	for _, tc := range []struct{ name, want string }{
		{"FAVRO_GONE", "bundleLeftOut names FAVRO_GONE, which the server does not read"},
		{config.EnvRequirePrompt, "bundleLeftOut names FAVRO_REQUIRE_PROMPT, which a release removed, and so left out already"},
	} {
		leftOut := map[string]string{tc.name: "a reason"}
		if _, got := bundleVarsFrom(config.Vars(), leftOut); !slices.Equal(got, []string{tc.want}) {
			t.Errorf("leaving out %s gives %q, want %q", tc.name, got, tc.want)
		}
	}
	// And settingProblems reports it, so the gate does.
	if got := settingProblems(committed(t), map[string]string{"FAVRO_GONE": "a reason"}); !has(got,
		"bundleLeftOut names FAVRO_GONE") {
		t.Errorf("settingProblems does not report a stale exclusion: %q", got)
	}
}

// fieldDiff names the fields two values disagree on, an unexported one
// included, and reports an unexported one it cannot compare unnamed
// rather than panicking.
func TestTheConfigDiffNamesTheFieldsThatDiffer(t *testing.T) {
	type cfg struct {
		Shown  int
		hidden int
		rest   []string
		held   any
	}
	for _, tc := range []struct {
		b    cfg
		want []string
	}{
		{cfg{2, 1, nil, nil}, []string{"Shown"}},
		{cfg{1, 2, nil, nil}, []string{"hidden"}},
		{cfg{1, 1, []string{"x"}, nil}, []string{"an unexported field"}},
		{cfg{1, 1, nil, nil}, nil},
	} {
		if got := fieldDiff(cfg{1, 1, nil, nil}, tc.b); !slices.Equal(got, tc.want) {
			t.Errorf("fieldDiff against %+v = %q, want %q", tc.b, got, tc.want)
		}
	}
	// An interface holding a slice on both sides compares as a type and
	// panics in Value.Equal; it is reported unnamed instead.
	if got := fieldDiff(cfg{held: []string{"y"}}, cfg{held: []string{"x"}}); !slices.Equal(got,
		[]string{"an unexported field"}) {
		t.Errorf("an interface field differing gives %q, want [an unexported field]", got)
	}
	if got := fieldDiff(config.Config{Destructive: true, Warnings: []string{"w"}}, config.Config{}); !slices.Equal(got,
		[]string{"Destructive", "Warnings"}) {
		t.Errorf("fieldDiff over config.Config = %q, want [Destructive Warnings]", got)
	}
}

// bundleSwitches is the switches the committed bundle asks for.
var bundleSwitches = []string{"enable_destructive"}

// refusingDestructive is the committed manifest with FAVRO_REQUIRE_PROMPT
// spending enable_destructive, so the server refuses enable_destructive
// on, and nothing else. The server refuses no set of its switches, so
// this is how the refusal check is made to fire.
func refusingDestructive(t *testing.T) bundleManifest {
	t.Helper()
	return edited(t, func(d map[string]any) {
		dig(d, "server", "mcp_config", "env")["FAVRO_REQUIRE_PROMPT"] = "${user_config.enable_destructive}"
	})
}

// refusals is refusalProblems over the bundle's switches, the defaults
// checked first as settingProblems checks them.
func refusals(t *testing.T, m bundleManifest, declared [][]string) []string {
	t.Helper()
	_, defaultsErr := defaultsProblems(m)
	return refusalProblems(m, bundleSwitches, declared, defaultsErr)
}

const undeclaredDestructive = "the server refuses [enable_destructive on], and bundleRefused does not declare it"

// Every set of the switches is started, and the refused ones are held to
// the declared list both ways, each defect reported once.
func TestTheBundleRefusalsAreDeclared(t *testing.T) {
	if got := refusals(t, committed(t), bundleRefused); len(got) != 0 {
		t.Errorf("the committed bundle and bundleRefused: %q", got)
	}
	if got := refusals(t, refusingDestructive(t), nil); len(got) != 1 || !strings.HasPrefix(got[0], undeclaredDestructive) {
		t.Errorf("an undeclared refusal gives %q, want the one problem %q", got, undeclaredDestructive)
	}
	declared := [][]string{{"enable_destructive"}}
	if got := refusals(t, refusingDestructive(t), declared); len(got) != 0 {
		t.Errorf("a declared refusal gives %q, want nothing", got)
	}
	const starts = "bundleRefused declares that the server refuses [enable_destructive on], and it starts with them"
	if got := refusals(t, committed(t), declared); !slices.Equal(got, []string{starts}) {
		t.Errorf("a declared refusal the server starts with gives %q, want %q once", got, starts)
	}
	// When the defaults do not start, no set is started: the defaults
	// check reports it.
	if got := refusalProblems(committed(t), bundleSwitches, declared,
		errors.New("the defaults do not start")); len(got) != 0 {
		t.Errorf("a set was started beside defaults that do not start: %q", got)
	}
	if got := refusals(t, committed(t), [][]string{{"enable_destructive", "upload_dir"}}); !slices.Contains(got,
		"bundleRefused names upload_dir, which is not a switch the bundle asks for") {
		t.Errorf("a declared key that is not a switch gives %q", got)
	}
}

// Each field in a declared set names the others' titles, trimmed, and a
// key with no title is reported once, however many sets name it. The
// bundle has one switch, so a second is added for the pair.
func TestTheDeclaredSetsNameEachOther(t *testing.T) {
	switches := []string{"enable_destructive", "other"}
	pair := [][]string{{"enable_destructive", "other"}}
	withOther := func(otherDescription, destructiveDescription, destructiveTitle string) bundleManifest {
		return edited(t, func(d map[string]any) {
			dig(d, "user_config")["other"] = map[string]any{
				"type": "boolean", "title": "Other", "description": otherDescription, "default": false,
			}
			dig(d, "user_config", "enable_destructive")["description"] = destructiveDescription
			dig(d, "user_config", "enable_destructive")["title"] = destructiveTitle
		})
	}
	if got := declaredProblems(withOther("Refused with Deleting on.", "Refused with Other on.", " Deleting "),
		switches, pair); len(got) != 0 {
		t.Errorf("fields that name each other give %q, want nothing", got)
	}
	want := []string{
		`user_config.enable_destructive does not name "Other", which the server refuses it with`,
		`user_config.other does not name "Deleting", which the server refuses it with`,
	}
	if got := declaredProblems(withOther("Register more.", "Register the deletes.", "Deleting"),
		switches, pair); !slices.Equal(got, want) {
		t.Errorf("fields that do not name each other give %q, want %q", got, want)
	}
	untitled := withOther("x", "x", " ")
	twice := [][]string{{"enable_destructive", "other"}, {"enable_destructive"}}
	for _, defaultsErr := range []error{nil, errors.New("the defaults do not start")} {
		got := refusalProblems(untitled, switches, twice, defaultsErr)
		if n := len(slices.DeleteFunc(got, func(p string) bool {
			return !strings.HasPrefix(p, "user_config.enable_destructive has no title")
		})); n != 1 {
			t.Errorf("defaults error %v: an untitled enable_destructive is reported %d times, want 1", defaultsErr, n)
		}
	}
}

// A switch in a set is turned to the value other than its default: off
// for one that defaults to true, and on for any other, a required one
// with no default included, which is turned on rather than left out.
func TestTheRefusalsTurnEachSwitchFromItsDefault(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{"defaults off", func(map[string]any) {}, "true"},
		{"defaults on", func(d map[string]any) { dig(d, "user_config", "enable_destructive")["default"] = true }, "false"},
		{"required with no default", func(d map[string]any) {
			dig(d, "user_config", "enable_destructive")["required"] = true
			delete(dig(d, "user_config", "enable_destructive"), "default")
		}, "true"},
	} {
		m := edited(t, tc.edit)
		env, _ := hostEnv(m, "enable_destructive")
		if got, ok := env["FAVRO_ENABLE_DESTRUCTIVE"]; !ok || got != tc.want {
			t.Errorf("%s: turned, FAVRO_ENABLE_DESTRUCTIVE is %q (set %t), want %q", tc.name, got, ok, tc.want)
		}
	}
	if got := turnedNames(edited(t, func(d map[string]any) {
		dig(d, "user_config", "enable_destructive")["default"] = true
	}), []string{"enable_destructive"}); got != "[enable_destructive off]" {
		t.Errorf("turnedNames of a default-on switch = %q", got)
	}
}

// settingProblems runs the refusal check over the switches the bundle
// asks for, so a refusal the server adds fails the gate itself, and
// defaults the server refuses start no set.
func TestSettingProblemsHoldTheRefusals(t *testing.T) {
	if got := settingProblems(refusingDestructive(t), bundleLeftOut); !has(got, undeclaredDestructive) {
		t.Errorf("settingProblems = %q, want one containing %q", got, undeclaredDestructive)
	}
	got := settingProblems(edited(t, func(d map[string]any) {
		dig(d, "server", "mcp_config", "env")["FAVRO_REQUIRE_PROMPT"] = "true"
	}), bundleLeftOut)
	if !has(got, "the environment the bundle's defaults give the server is refused: FAVRO_REQUIRE_PROMPT was removed") ||
		has(got, "the server refuses [") {
		t.Errorf("defaults the server refuses give %q, want the defaults problem and no set started", got)
	}
}

// The launcher's install hint, for a machine the bundle has no binary
// for, names the module go.mod declares, major version included, and
// goModule reads go.mod under Windows line endings too.
func TestTheLauncherHintNamesTheModule(t *testing.T) {
	if got := goModule("module example.test/m/v9\r\n\r\ngo 1.27\r\n"); got != "example.test/m/v9" {
		t.Fatalf("a CRLF go.mod reads as module %q", got)
	}
	module := goModule(string(repoFile(t, "go.mod")))
	if problems := installHintProblems(mcpbLauncherScript(), module); len(problems) > 0 {
		t.Errorf("the committed launcher: %q", problems)
	}
	if got := installHintProblems("echo no hint", module); !slices.Equal(got,
		[]string{"the launcher gives no install hint for an architecture the bundle does not carry"}) {
		t.Errorf("a launcher with no hint gives %q", got)
	}
	if got := installHintProblems(mcpbLauncherScript(), ""); len(got) != 1 {
		t.Errorf("a go.mod naming no module gives %q", got)
	}
}

// The license the manifest names is the one LICENSE carries.
func TestLicenseOfReadsTheLicense(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"                                 Apache License\n", "Apache-2.0"},
		{"MIT License\r\n", "MIT"},
		{"Some other terms\n", ""},
	} {
		root := t.TempDir()
		put(t, root, "LICENSE", tc.text)
		if got, err := licenseOf(root); err != nil || got != tc.want {
			t.Errorf("licenseOf(%q) = %q, %v; want %q", tc.text, got, err, tc.want)
		}
	}
	if _, err := licenseOf(t.TempDir()); err == nil {
		t.Error("a root with no LICENSE gave no error")
	}
}
