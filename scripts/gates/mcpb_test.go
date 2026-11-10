package main

import (
	"archive/zip"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
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
	data, err := os.ReadFile(filepath.Join(parityRepoRoot, filepath.FromSlash(rel)))
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
// holds the bundle to, copied from the repository, so a test can break
// one of them. release.yml is the one workflow among them.
func copiedRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range []string{mcpbManifestPath, "go.mod", "LICENSE", parityGoreleaser, releaseWorkflow} {
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
	_, problems, err := committedManifest(parityRepoRoot)
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
		// Go's decode matches a struct field's key in any case, so the
		// gate reads the second command and the host the first.
		{
			name: "a key spelled in another case", file: mcpbManifestPath,
			old:  `"command": "${__dirname}/server/favro-mcp-darwin",`,
			new:  `"command": "${__dirname}/server/favro-mcp-darwin", "Command": "${__dirname}/server/launch-linux.sh",`,
			want: "has server.mcp_config.Command more than once, counting keys that differ only in case",
		},
		{
			name: "a go.mod naming no module", file: "go.mod", old: "module github.com/mmedum/favro-mcp/v4",
			new: "// no module", want: "go.mod names no module, so the launcher can say nothing of how to install",
		},
		{
			name: "another license", file: "LICENSE", old: "Apache License", new: "MIT License",
			want: `the manifest says the license is "Apache-2.0" and LICENSE is MIT`,
		},
		{
			name: "a license it cannot name", file: "LICENSE", old: "Apache License", new: "Some License",
			want: "LICENSE is neither the Apache License nor the MIT License",
		},
		// The archive a downstream user found missing.
		{
			name: "windows arm64 ignored", file: parityGoreleaser, old: "      - arm64\n",
			new:  "      - arm64\n    ignore:\n      - goos: windows\n        goarch: arm64\n",
			want: ".goreleaser.yaml does not build windows-arm64, so the release carries no windows_arm64 archive",
		},
		// Every build is read, in goreleaser's usual shape, its first key on
		// the dash.
		{
			name: "a second build for a platform the matrix does not name", file: parityGoreleaser,
			old:  "      - arm64\n\nuniversal_binaries:",
			new:  "      - arm64\n  - main: ./cmd/favro-mcp\n    goos:\n      - freebsd\n    goarch:\n      - amd64\n\nuniversal_binaries:",
			want: ".goreleaser.yaml builds freebsd-amd64, which is not in the release matrix",
		},
		{
			name: "no checksum glob", file: parityGoreleaser,
			old: "    - glob: ./dist/*.mcpb\n    - glob: ./dist/*.plugin\n\n# One SBOM",
			new: "    - glob: ./dist/*.plugin\n\n# One SBOM", want: ".goreleaser.yaml has no checksum.extra_files.glob ending in .mcpb",
		},
		{
			name: "a shape the config reader does not read", file: parityGoreleaser,
			old: "    binary: favro-mcp\n", new: "    binary: &bin favro-mcp\n",
			want: ".goreleaser.yaml:13: binary: an anchor, an alias or a tag; write the value out",
		},
		{
			name: "a checksum glob named only in a comment", file: parityGoreleaser,
			old: "    - glob: ./dist/*.mcpb\n    - glob: ./dist/*.plugin\n\n# One SBOM",
			new: "    - glob: ./dist/*.plugin # not ./dist/*.mcpb\n\n# One SBOM", want: ".goreleaser.yaml has no checksum.extra_files.glob ending in .mcpb",
		},
		{
			name: "no attested plugin", file: releaseWorkflow, old: "            dist/*.plugin\n", new: "",
			want: "does not name dist/*.plugin, so it ships with no provenance",
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
			_, problems, err := committedManifest(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(problems) != 1 || !strings.Contains(problems[0], tc.want) {
				t.Errorf("problems:\n%s\nwant the one containing %q", strings.Join(problems, "\n"), tc.want)
			}
		})
	}
}

// A key is repeated only within one object, two that differ only in
// case are one, and each repeated key is named once, by its path, a
// list's elements and a repeated object's own keys included. A key a
// path would misread is quoted, and a document the walk cannot read is
// refused rather than passed.
func TestRepeatedKeysNameEachPathOnce(t *testing.T) {
	for _, tc := range []struct {
		doc  string
		want []string
	}{
		{`{"a": [{"b": 1, "b": 2, "b": 3}], "c": {"d": 1}, "d": 1}`, []string{"a[0].b"}},
		{`{"x": {"y": 1}, "x": {"y": 1, "y": 2}}`, []string{"x", "x.y"}},
		{`{"server": {"type": "binary"}, "user_config": {"type": {"type": "boolean"}}}`, nil},
		{`{"server": {"command": "a", "Command": "b", "COMMAND": "c"}}`, []string{"server.Command"}},
		// The Kelvin sign folds to k, as Go's decode folds it.
		{`{"kind": 1, "\u212aind": 2}`, []string{"\u212aind"}},
		{`{"env": {"a.b": 1, "a.b": 2}, "c[0]": 1, "c[0]": 2}`, []string{`env["a.b"]`, `["c[0]"]`}},
		{`{"env": {"a": {"b": 1}, "a.b": 1}}`, nil},
	} {
		got, err := repeatedKeys([]byte(tc.doc))
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("repeatedKeys(%s) = %q, %v; want %q", tc.doc, got, err, tc.want)
		}
	}
	if got, err := repeatedKeys([]byte(`{"a": 1,`)); err == nil {
		t.Errorf("a document cut short gives %q and no error", got)
	}
}

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

// TestUndeclaredUserConfigReferenceIsCaught, including the composed form:
// checking only values that are nothing but
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
	script := mcpbLauncherScript("example.test/m")
	if problems := mcpbLauncherAgrees(script); len(problems) > 0 {
		t.Errorf("the generated launcher disagrees with the packer:\n%s", strings.Join(problems, "\n"))
	}

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
	t.Chdir(parityRepoRoot)
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

// inertHookDist is dist/ as it looks when the post hook runs, for both
// packers, with goreleaser 2.18.1's directory names: metadata.json, and
// a directory per build carrying the build id and an architecture
// variant — and no artifacts.json, because goreleaser writes that at the
// end. That absence is why both packers glob. The windows arm64 build is
// there too, as goreleaser writes it, and neither bundle takes it.
//
// Every binary is an inert placeholder, for a test that never reaches
// versionsAgree's execution: a snapshot version, or a refusal before it.
func inertHookDist(t *testing.T, version string) string {
	t.Helper()
	dist := t.TempDir()
	if err := writeJSON(filepath.Join(dist, "metadata.json"), map[string]any{"version": version}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"universal_darwin_all/favro-mcp",
		"favro-mcp_darwin_amd64_v1/favro-mcp",
		"favro-mcp_darwin_arm64_v8.0/favro-mcp",
		"favro-mcp_windows_amd64_v1/favro-mcp.exe",
		"favro-mcp_windows_arm64_v8.0/favro-mcp.exe",
		"favro-mcp_linux_amd64_v1/favro-mcp",
		"favro-mcp_linux_arm64_v8.0/favro-mcp",
	} {
		full := filepath.Join(dist, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("not a real binary"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dist
}

// fakeHookDist is inertHookDist with the host's binary replaced by a real
// executable reporting version: the packer asks it what version it
// thinks it is, which is the one of a bundle's five version claims
// checkable at pack time. A shell script named favro-mcp.exe is not
// something Windows will run, so the stub is compiled.
func fakeHookDist(t *testing.T, version string) string {
	t.Helper()
	dist := inertHookDist(t, version)
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
		return filepath.Join("universal_darwin_all", "favro-mcp")
	case host == "windows-amd64":
		return filepath.Join("favro-mcp_windows_amd64_v1", "favro-mcp.exe")
	case host == "linux-amd64":
		return filepath.Join("favro-mcp_linux_amd64_v1", "favro-mcp")
	default:
		return filepath.Join("favro-mcp_linux_arm64_v8.0", "favro-mcp")
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
	t.Chdir(parityRepoRoot)
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
	t.Chdir(parityRepoRoot)
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
	t.Chdir(parityRepoRoot)
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

// TestReleaseWiringIsHeld breaks each requirement in the committed config,
// one at a time, as text the reader parses. The values it covers are
// invisible to every other check here: delete checksum.extra_files and the
// release succeeds, make check passes, and the bundle ships outside the
// signature while README says it is covered.
func TestReleaseWiringIsHeld(t *testing.T) {
	cfg := strings.ReplaceAll(string(repoFile(t, parityGoreleaser)), "\r\n", "\n")
	if problems := mcpbReleaseWiring(parseGoreleaser(cfg)); len(problems) > 0 {
		t.Fatalf("the committed %s does not satisfy its own wiring check:\n%s", parityGoreleaser, strings.Join(problems, "\n"))
	}
	const (
		hook          = `.goreleaser.yaml has no universal_binaries.hooks.post of "{{ if not .Prerelease }}go run ./scripts/gates plugin-pack dist{{ end }}"`
		pluginHookRaw = `        - "{{ if not .Prerelease }}go run ./scripts/gates plugin-pack dist{{ end }}"` + "\n"
		checksumGlobs = "    - glob: ./dist/*.mcpb\n    - glob: ./dist/*.plugin\n\n# One SBOM"
		releaseGlobs  = "never published.\n  extra_files:\n    - glob: ./dist/*.mcpb\n    - glob: ./dist/*.plugin\n"
	)
	for _, tc := range []struct{ name, old, new, want string }{
		{
			"the bundle's hook removed", "        - go run ./scripts/gates mcpb-pack {{ .Version }} dist\n", "",
			".goreleaser.yaml has no universal_binaries.hooks.post that runs mcpb-pack",
		},
		{"the plugin hook removed", pluginHookRaw, "", hook},
		{"the guard inverted", "{{ if not .Prerelease }}", "{{ if .Prerelease }}", hook},
		{"the guard missing", pluginHookRaw, "        - go run ./scripts/gates plugin-pack dist\n", hook},
		{
			"no checksum glob for the bundle", checksumGlobs, "    - glob: ./dist/*.plugin\n\n# One SBOM",
			".goreleaser.yaml has no checksum.extra_files.glob ending in .mcpb",
		},
		{
			"no checksum glob for the plugin", checksumGlobs, "    - glob: ./dist/*.mcpb\n\n# One SBOM",
			".goreleaser.yaml has no checksum.extra_files.glob ending in .plugin",
		},
		{
			"no release glob for the bundle", releaseGlobs, "never published.\n  extra_files:\n    - glob: ./dist/*.plugin\n",
			".goreleaser.yaml has no release.extra_files.glob ending in .mcpb",
		},
		{
			"no release glob for the plugin", releaseGlobs, "never published.\n  extra_files:\n    - glob: ./dist/*.mcpb\n",
			".goreleaser.yaml has no release.extra_files.glob ending in .plugin",
		},
		{"another signer", "  - cmd: cosign\n", "  - cmd: gpg\n", ".goreleaser.yaml has no signs.cmd of cosign"},
		{
			"a signature over every artifact", "    artifacts: checksum\n", "    artifacts: all\n",
			".goreleaser.yaml has no signs.artifacts of checksum",
		},
		// A value is the key's, not what a comment after it says.
		{
			"the signed artifacts named only in a comment", "    artifacts: checksum\n", "    artifacts: all # artifacts: checksum\n",
			".goreleaser.yaml has no signs.artifacts of checksum",
		},
		{
			"an SBOM per binary", "  - artifacts: archive\n", "  - artifacts: binary\n",
			".goreleaser.yaml has no sboms.artifacts of archive",
		},
		{
			"no build timestamp", "    mod_timestamp: \"{{ .CommitTimestamp }}\"\n", "",
			".goreleaser.yaml has no builds.mod_timestamp",
		},
		{"no archive ids", "    ids: [favro-mcp]\n    formats:", "    formats:", ".goreleaser.yaml has no archives.ids"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(cfg, tc.old) {
				t.Fatalf("%s does not say %q", parityGoreleaser, tc.old)
			}
			got := mcpbReleaseWiring(parseGoreleaser(strings.Replace(cfg, tc.old, tc.new, 1)))
			if len(got) != 1 || !strings.HasPrefix(got[0], tc.want) {
				t.Errorf("got %q, want the one problem %q", got, tc.want)
			}
		})
	}
}

// release.yml's provenance step names every file the release publishes in
// subject-path, as the goreleaser config names them: the archives, both
// bundles and the checksum file, each in an entry of its own. The README
// tells people to verify each one's attestation, and no other check reads
// the workflow's subjects. The step sits in each job that runs goreleaser,
// with a token that writes id-token and attestations. A row's cfg is the
// committed config unless it gives one.
func TestTheBundlesCarryProvenance(t *testing.T) {
	workflow := strings.ReplaceAll(string(repoFile(t, releaseWorkflow)), "\r\n", "\n")
	cfg := strings.ReplaceAll(string(repoFile(t, parityGoreleaser)), "\r\n", "\n")
	attested := func(workflow, cfg string) []string {
		return attestedBundles([]workflowFile{parseWorkflow(releaseWorkflow, workflow)}, parseGoreleaser(cfg))
	}
	if got := attested(workflow, cfg); len(got) > 0 {
		t.Fatalf("the committed %s: %q", releaseWorkflow, got)
	}
	if got, want := attestedBundles([]workflowFile{parseWorkflow(fxPublish, workflow)}, parseGoreleaser(cfg)),
		[]string{"no " + releaseWorkflow + ", so nothing attests what the release publishes"}; !slices.Equal(got, want) {
		t.Errorf("workflows without %s: %q, want %q", releaseWorkflow, got, want)
	}
	// A local workflow release.yml calls is held as release.yml is.
	calledRelease := textLines(
		"jobs:",
		"  goreleaser:",
		"    runs-on: ubuntu-latest",
		"    steps:",
		"      - uses: goreleaser/goreleaser-action@"+pinSHA+" # v7.3.0",
		"        with:",
		"          args: release --clean",
	)
	if got, want := attestedBundles([]workflowFile{parseWorkflow(releaseWorkflow, workflow), parseWorkflow(fxPublish, calledRelease)},
		parseGoreleaser(cfg)), []string{fxPublish + ": job goreleaser runs goreleaser and has no attest-build-provenance step, " +
		"so no release file carries provenance"}; !slices.Equal(got, want) {
		t.Errorf("a called workflow releasing with no attest step: %q, want %q", got, want)
	}
	const (
		block    = "          subject-path: |\n            dist/*.tar.gz\n            dist/*.zip\n            dist/*.mcpb\n            dist/*.plugin\n            dist/checksums.txt\n"
		all      = "dist/*.tar.gz\ndist/*.zip\ndist/*.mcpb\ndist/*.plugin\ndist/checksums.txt"
		unproven = ", so it ships with no provenance while the README says to verify it"
		unscoped = " which actions/attest needs to sign and store the provenance; " +
			"grant it in the job's permissions, or the workflow's when the job sets none"
		// jobPerms is actionRelease's job permissions, which the
		// permission rows replace.
		jobPerms = "    permissions:\n      id-token: write\n      attestations: write\n"
		registry = "  registry:\n"
	)
	missing := func(subjects, what string) string {
		return fmt.Sprintf("subject-path %q does not name %s%s", subjects, what, unproven)
	}
	// unattested is the problem of a job that runs goreleaser with no
	// attest step.
	unattested := func(job string) []string {
		return []string{releaseWorkflow + ": job " + job + " runs goreleaser and has no attest-build-provenance step, " +
			"so no release file carries provenance"}
	}
	// job is a job of one step, which a row puts before registry.
	job := func(name, step string) string {
		return "  " + name + ":\n    runs-on: ubuntu-latest\n    steps:\n" + step
	}
	const (
		runClean    = "      - run: goreleaser --clean\n"
		actionCheck = "      - uses: goreleaser/goreleaser-action@" + pinSHA + " # v7.3.0\n        with:\n          args: check\n"
	)
	// goreleaser-action releases with no run line, under a workflow token
	// that writes neither scope.
	actionRelease := textLines(
		"permissions:",
		"  contents: read",
		"jobs:",
		"  goreleaser:",
		"    runs-on: ubuntu-latest",
		"    permissions:",
		"      id-token: write",
		"      attestations: write",
		"    steps:",
		"      - uses: goreleaser/goreleaser-action@"+pinSHA+" # v7.3.0",
		"        with:",
		"          version: v2.18.1",
		"          args: release --clean",
		"      - uses: actions/attest-build-provenance@"+pinSHA+" # v4.2.2",
		"        with:",
		"          subject-path: dist/*.tar.gz,dist/*.zip,dist/*.mcpb,dist/*.plugin,dist/checksums.txt",
	)
	if strings.Count(actionRelease, jobPerms) != 1 {
		t.Fatalf("actionRelease does not say %q once", jobPerms)
	}
	noGoreleaser := textLines(
		"jobs:",
		"  release:",
		"    runs-on: ubuntu-latest",
		"    steps:",
		"      - run: make dist",
		"      - uses: actions/attest-build-provenance@"+pinSHA+" # v4.2.2",
		"        with:",
		"          subject-path: dist/*.tar.gz,dist/*.zip,dist/*.mcpb,dist/*.plugin,dist/checksums.txt",
	)
	for _, tc := range []struct {
		name, old, new string
		cfg            string
		want           []string
	}{
		{"goreleaser-action releasing", workflow, actionRelease, "", nil},
		{
			"no .mcpb", "            dist/*.mcpb\n", "", "",
			[]string{missing("dist/*.tar.gz\ndist/*.zip\ndist/*.plugin\ndist/checksums.txt", "dist/*.mcpb")},
		},
		{
			"no .plugin", "            dist/*.plugin\n", "", "",
			[]string{missing("dist/*.tar.gz\ndist/*.zip\ndist/*.mcpb\ndist/checksums.txt", "dist/*.plugin")},
		},
		{
			"no archives and no checksum file", "            dist/*.tar.gz\n            dist/*.zip\n", "", "",
			[]string{
				missing("dist/*.mcpb\ndist/*.plugin\ndist/checksums.txt", "dist/*.tar.gz"),
				missing("dist/*.mcpb\ndist/*.plugin\ndist/checksums.txt", "dist/*.zip"),
			},
		},
		{
			"a signature where the bundle goes", "            dist/*.mcpb\n", "            dist/*.mcpb.sigstore.json\n", "",
			[]string{missing("dist/*.tar.gz\ndist/*.zip\ndist/*.mcpb.sigstore.json\ndist/*.plugin\ndist/checksums.txt", "dist/*.mcpb")},
		},
		// actions/attest skips a glob that matches nothing, so an entry in
		// another folder leaves the bundle unattested.
		{
			"the bundle in another folder", "            dist/*.mcpb\n", "            dist/unused/*.mcpb\n", "",
			[]string{missing("dist/*.tar.gz\ndist/*.zip\ndist/unused/*.mcpb\ndist/*.plugin\ndist/checksums.txt", "dist/*.mcpb")},
		},
		{
			"the .plugin in another input", "            dist/*.plugin\n            dist/checksums.txt\n",
			"            dist/checksums.txt\n          subject-name: dist/*.plugin\n", "",
			[]string{missing("dist/*.tar.gz\ndist/*.zip\ndist/*.mcpb\ndist/checksums.txt", "dist/*.plugin")},
		},
		// actions/attest splits at commas and newlines only, so a folded
		// block is one glob that matches nothing.
		{"a folded block", "subject-path: |", "subject-path: >", "", []string{
			fmt.Sprintf("subject-path %q separates with a space, which is read as one glob; separate with commas or newlines",
				strings.ReplaceAll(all, "\n", " ")),
			missing(strings.ReplaceAll(all, "\n", " "), "dist/*.tar.gz"),
			missing(strings.ReplaceAll(all, "\n", " "), "dist/*.zip"),
			missing(strings.ReplaceAll(all, "\n", " "), "dist/*.mcpb"),
			missing(strings.ReplaceAll(all, "\n", " "), "dist/*.plugin"),
			missing(strings.ReplaceAll(all, "\n", " "), "dist/checksums.txt"),
		}},
		{
			"commas, a space after each", block,
			"          subject-path: dist/*.tar.gz, dist/*.zip, dist/*.mcpb, dist/*.plugin, dist/checksums.txt\n", "", nil,
		},
		{
			"another checksum file", "", "", strings.Replace(cfg, "name_template: checksums.txt", "name_template: '{{ .ProjectName }}_checksums.txt'", 1),
			[]string{missing(all, "dist/{{ .ProjectName }}_checksums.txt")},
		},
		{
			"no checksum name", "", "", strings.Replace(cfg, "  name_template: checksums.txt\n", "", 1),
			[]string{"checksum.name_template is empty, so nothing holds the subject-path to the file every other artifact is verified through"},
		},
		{
			"no subject-path", block, "", "",
			[]string{releaseWorkflow + ":101: the attestation names no subject-path"},
		},
		{
			"the step commented out", "      - uses: actions/attest-build-provenance@",
			"      # - uses: actions/attest-build-provenance@", "",
			unattested("goreleaser"),
		},
		// Another job can be skipped while the release ships, and has no
		// dist/ of its own.
		{
			"the step in a job of its own", "      - uses: actions/attest-build-provenance@",
			"  attest:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/attest-build-provenance@", "",
			unattested("goreleaser"),
		},
		{
			"no job running goreleaser", workflow, noGoreleaser, "",
			[]string{"no job of " + releaseWorkflow + ", or of a workflow it calls, runs goreleaser, so no attest step is held to the files the release publishes"},
		},
		// Any job that runs goreleaser is held, whatever its subcommand
		// and however the line spells it.
		{"a second job running goreleaser with no subcommand", registry, job("snapshot", runClean) + registry, "", unattested("snapshot")},
		{
			"a second job running goreleaser behind a variable", registry,
			job("snapshot", "      - run: GORELEASER_CURRENT_TAG=v1.0.0 goreleaser release\n") + registry, "", unattested("snapshot"),
		},
		{
			"a second job running goreleaser by its path", registry,
			job("snapshot", "      - run: /usr/local/bin/goreleaser release\n") + registry, "", unattested("snapshot"),
		},
		{
			"a second job running goreleaser by its module, as the Makefile does", registry,
			job("snapshot", "      - run: go run github.com/goreleaser/goreleaser/v2@v2.18.1 release --clean\n") + registry, "", unattested("snapshot"),
		},
		{"a second job checking with goreleaser-action", registry, job("check", actionCheck) + registry, "", unattested("check")},
		{
			"jobs running goreleaser either way, named in file order", registry,
			job("snapshot", runClean) + job("check", actionCheck) + job("nightly", runClean) + registry, "",
			slices.Concat(unattested("snapshot"), unattested("check"), unattested("nightly")),
		},
		{
			"no attestations: write", "      attestations: write\n", "", "",
			[]string{releaseWorkflow + ": job goreleaser attests without attestations: write," + unscoped},
		},
		{
			"no id-token: write", "      id-token: write\n", "", "",
			[]string{releaseWorkflow + ": job goreleaser attests without id-token: write," + unscoped},
		},
		// A job with no permissions takes the workflow's, which write neither.
		{
			"no permissions in the job", workflow, strings.Replace(actionRelease, jobPerms, "", 1), "",
			[]string{
				releaseWorkflow + ": job goreleaser attests without id-token: write," + unscoped,
				releaseWorkflow + ": job goreleaser attests without attestations: write," + unscoped,
			},
		},
		{"the job's token writing every scope", workflow, strings.Replace(actionRelease, jobPerms, "    permissions: write-all\n", 1), "", nil},
		{
			"no archive formats", "", "", strings.Replace(cfg, "    formats: [tar.gz]\n", "", 1),
			[]string{"archives.formats is not set, so nothing holds the subject-path to the archives"},
		},
		// A format two archives share is one file ending to name.
		{
			"a format two archives share, left out", "            dist/*.tar.gz\n", "",
			strings.Replace(cfg, "formats: [zip]", "formats: [tar.gz]", 1),
			[]string{missing("dist/*.zip\ndist/*.mcpb\ndist/*.plugin\ndist/checksums.txt", "dist/*.tar.gz")},
		},
		{
			"an archive format the attestation does not name", "", "",
			strings.Replace(cfg, "formats: [tar.gz]", "formats: [tar.gz, tar.zst]", 1),
			[]string{missing(all, "dist/*.tar.zst")},
		},
		// goreleaser writes every file into its dist directory, so a config
		// that moves it leaves each dist/ entry matching nothing.
		{
			"another dist directory", "", "", strings.Replace(cfg, "version: 2\n", "version: 2\ndist: ./out\n", 1),
			[]string{
				missing(all, "out/*.tar.gz"), missing(all, "out/*.zip"), missing(all, "out/*.mcpb"),
				missing(all, "out/*.plugin"), missing(all, "out/checksums.txt"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(workflow, tc.old) {
				t.Fatalf("%s does not say %q", releaseWorkflow, tc.old)
			}
			edited := workflow
			if tc.old != "" {
				edited = strings.Replace(workflow, tc.old, tc.new, 1)
			}
			got := attested(edited, cmp.Or(tc.cfg, cfg))
			if !slices.Equal(got, tc.want) {
				t.Errorf("problems:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(tc.want, "\n  "))
			}
		})
	}
}

// The release carries releaseMatrix's archives, both ways: a pair
// .goreleaser.yaml leaves out is an archive nobody can download.
func TestReleaseArchivesAreTheFamilys(t *testing.T) {
	release, err := readGoreleaser(parityRepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	built, err := goreleaserMatrix(release)
	if err != nil {
		t.Fatal(err)
	}
	if problems := releaseArchiveProblems(built); len(problems) > 0 {
		t.Errorf("the committed matrix: %q", problems)
	}
	short := slices.DeleteFunc(slices.Clone(built), func(p string) bool { return p == "windows-arm64" })
	want := []string{".goreleaser.yaml does not build windows-arm64, so the release carries no windows_arm64 " +
		"archive; the release matrix is darwin-amd64, darwin-arm64, linux-amd64, linux-arm64, " +
		"windows-amd64, windows-arm64"}
	if got := releaseArchiveProblems(short); !slices.Equal(got, want) {
		t.Errorf("a matrix without windows-arm64 gives %q, want %q", got, want)
	}
	if got := releaseArchiveProblems(nil); len(got) != len(releaseMatrix) {
		t.Errorf("an empty matrix gives %d problems, want one per pair: %q", len(got), got)
	}
	beyond := append(slices.Clone(built), "freebsd-amd64")
	want = []string{".goreleaser.yaml builds freebsd-amd64, which is not in the release matrix, " +
		"darwin-amd64, darwin-arm64, linux-amd64, linux-arm64, windows-amd64, windows-arm64; " +
		"add it to releaseMatrix, which both bundle gates read"}
	if got := releaseArchiveProblems(beyond); !slices.Equal(got, want) {
		t.Errorf("a matrix with freebsd-amd64 gives %q, want %q", got, want)
	}
}

// Every pair the release builds runs a binary the Desktop bundle stages:
// its own, the universal one on macOS, or the one releaseMatrix says it
// runs under emulation, which is the plugin gate's table too.
func TestTheDesktopBundleRunsOnEveryPair(t *testing.T) {
	if got := mcpbMatrixProblems(mcpbBinaries); len(got) > 0 {
		t.Fatalf("the committed table: %q", got)
	}
	without := func(drop func(mcpbBinary) bool) []mcpbBinary {
		return slices.DeleteFunc(slices.Clone(mcpbBinaries), drop)
	}
	for _, tc := range []struct {
		name     string
		binaries []mcpbBinary
		want     []string
	}{
		{"no universal binary", without(func(b mcpbBinary) bool { return b.universal }), []string{
			"the release builds darwin-amd64 and the Desktop bundle stages no binary that runs there",
			"the release builds darwin-arm64 and the Desktop bundle stages no binary that runs there",
		}},
		// windows-arm64 runs it, so it goes too.
		{"no windows binary", without(func(b mcpbBinary) bool { return b.goarch == "windows-amd64" }), []string{
			"the release builds windows-amd64 and the Desktop bundle stages no binary that runs there",
			"the release builds windows-arm64 and the Desktop bundle stages no binary that runs there",
		}},
	} {
		if got := mcpbMatrixProblems(tc.binaries); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	// And the gate runs it: a table without the universal binary fails
	// releaseProblems, which committedManifest runs.
	saved := mcpbBinaries
	t.Cleanup(func() { mcpbBinaries = saved })
	mcpbBinaries = without(func(b mcpbBinary) bool { return b.universal })
	if got := releaseProblems(parityRepoRoot); !has(got, "the release builds darwin-amd64 and the Desktop bundle stages no binary") {
		t.Errorf("releaseProblems without the universal binary: %q", got)
	}
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
		{
			"a credential shown as plain text", func(d map[string]any) { delete(userConfig(d, "api_token"), "sensitive") },
			"user_config.api_token is not sensitive; FAVRO_API_TOKEN is a credential",
		},
		{
			"a required switch", func(d map[string]any) { userConfig(d, "enable_destructive")["required"] = true },
			"user_config.enable_destructive is required; FAVRO_ENABLE_DESTRUCTIVE is a switch",
		},
		// The defaults check leaves out a required key with no default,
		// so this one is held by the rule alone.
		{"a required switch with no default", func(d map[string]any) {
			userConfig(d, "enable_destructive")["required"] = true
			delete(userConfig(d, "enable_destructive"), "default")
		}, "user_config.enable_destructive is required; FAVRO_ENABLE_DESTRUCTIVE is a switch"},
		// The server does not load the credentials, so the defaults check
		// starts it the same either way.
		{
			"a default holding a host variable", func(d map[string]any) { userConfig(d, "user_email")["default"] = "${HOME}" },
			`user_config.user_email defaults to "${HOME}", which holds a ${...} reference`,
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
	// A bundle asking for no secret holds no field to sensitive, which is
	// reported rather than passed.
	noToken := map[string]string{config.EnvAPIToken: "a reason"}
	if got := settingProblems(committed(t), noToken); !has(got,
		"the bundle asks for no variable config.Vars marks secret") {
		t.Errorf("a bundle asking for no secret gives %q", got)
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
		// Reported whatever else differs, not only alone.
		{cfg{2, 2, []string{"x"}, nil}, []string{"Shown", "hidden", "an unexported field"}},
	} {
		if got := fieldDiff(cfg{1, 1, nil, nil}, tc.b); !slices.Equal(got, tc.want) {
			t.Errorf("fieldDiff against %+v = %q, want %q", tc.b, got, tc.want)
		}
	}
	// A pointer compares by what it points at, not by its address.
	type pointing struct{ at *int }
	one, alsoOne, two := 1, 1, 2
	if got := fieldDiff(pointing{&one}, pointing{&alsoOne}); len(got) != 0 {
		t.Errorf("two pointers to equal values give %q, want nothing", got)
	}
	if got := fieldDiff(pointing{&one}, pointing{&two}); !slices.Equal(got, []string{"an unexported field"}) {
		t.Errorf("two pointers to different values give %q, want [an unexported field]", got)
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
// for, names the module go.mod declares, major version included, so a
// new major version cannot leave it behind.
func TestTheLauncherHintIsGoModsModule(t *testing.T) {
	const hint = `"Install with: go install example.test/m/v9/cmd/favro-mcp@latest" >&2`
	if script := mcpbLauncherScript("example.test/m/v9"); !strings.Contains(script, hint) {
		t.Errorf("the launcher for example.test/m/v9 does not say %s:\n%s", hint, script)
	}
	root := copiedRoot(t)
	put(t, root, "go.mod", strings.ReplaceAll(string(repoFile(t, "go.mod")), "/v4", "/v5"))
	b, problems, err := committedManifest(root)
	if err != nil || len(problems) > 0 {
		t.Fatalf("a go.mod at the next major version: %q, %v", problems, err)
	}
	if want := "go install github.com/mmedum/favro-mcp/v5/cmd/favro-mcp@latest"; !strings.Contains(b.launcher, want) {
		t.Errorf("the launcher committedManifest generates does not say %s", want)
	}
}

// modulePath reads the path after module and a space or a tab, without a
// trailing comment or quotes, under Windows line endings too, and refuses
// a go.mod that names none.
func TestModulePathReadsGoMod(t *testing.T) {
	for _, tc := range []struct{ goMod, module, err string }{
		{"module example.test/m/v9\n\ngo 1.27\n", "example.test/m/v9", ""},
		{"module example.test/m/v9\r\n\r\ngo 1.27\r\n", "example.test/m/v9", ""},
		{"module\texample.test/m // the server\r\n", "example.test/m", ""},
		{"// module example.test/old\nmodule \"example.test/q\"\n", "example.test/q", ""},
		{"modules example.test/m\ngo 1.27\n", "", "go.mod names no module"},
	} {
		root := t.TempDir()
		put(t, root, "go.mod", tc.goMod)
		module, err := modulePath(root)
		refused := ""
		if err != nil {
			refused = err.Error()
		}
		if module != tc.module || refused != tc.err {
			t.Errorf("go.mod %q: modulePath = %q, %q; want %q, %q", tc.goMod, module, refused, tc.module, tc.err)
		}
	}
}

// The license the manifest names is the one LICENSE carries, and one it
// cannot name is refused rather than holding the manifest to nothing.
func TestLicenseOfReadsTheLicense(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"                                 Apache License\n", "Apache-2.0"},
		{"MIT License\r\n", "MIT"},
	} {
		root := t.TempDir()
		put(t, root, "LICENSE", tc.text)
		if got, err := licenseOf(root); err != nil || got != tc.want {
			t.Errorf("licenseOf(%q) = %q, %v; want %q", tc.text, got, err, tc.want)
		}
	}
	root := t.TempDir()
	put(t, root, "LICENSE", "Some other terms\n")
	if got, err := licenseOf(root); err == nil {
		t.Errorf("a license it cannot name reads as %q", got)
	}
	if _, err := licenseOf(t.TempDir()); err == nil {
		t.Error("a root with no LICENSE gave no error")
	}
}
