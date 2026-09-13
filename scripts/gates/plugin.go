package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

// The plugin bundle: one zip that registers this server in a Claude Code
// or Cowork profile. It carries a binary per platform, a launcher that
// picks between them, and a manifest.
//
// This file holds both halves deliberately. The packer writes the
// launcher, so the launcher's binary names ARE the packer's names, and
// the gate reads the same table — which closes the hole the shared
// standard names last and most quietly: a manifest can pass every
// referential check while the launcher, which no manifest mentions,
// chooses between names nothing stages. Renaming a staged binary then
// ships a bundle that fails for every user of that platform, with the
// launcher's own "missing from the bundle" message.
//
// The gate needs no build. It validates the COMMITTED manifest against
// the NAMES the packer will stage, which are static, so a manifest that
// names a file nobody will stage is a fact about two files in the
// repository — caught on the commit that writes it rather than at the
// most expensive moment there is.

// placeholderVersion is what the committed manifest must say. The packer
// substitutes the real version and refuses any other value here, so a
// manifest in the tree cannot claim a stale version.
const placeholderVersion = "0.0.0-placeholder"

const (
	templateDir  = "plugin-template"
	manifestPath = templateDir + "/.claude-plugin/plugin.json"
	mcpPath      = templateDir + "/.mcp.json"
	bundleName   = "favro-mcp.plugin"
	launcherPath = "bin/favro-mcp"
	cmdShimPath  = "bin/favro-mcp.cmd"
)

// platform is one binary the bundle carries, the directory it is staged
// under, and how a launcher recognises the machine it belongs to.
//
// This is the one table. The packer stages from it, the launcher is
// generated from it, and the gate compares it against what GoReleaser
// builds — so the three cannot disagree.
var platforms = []platform{
	{goos: "darwin", goarch: "arm64", uname: []string{"Darwin-arm64"}},
	{goos: "darwin", goarch: "amd64", uname: []string{"Darwin-x86_64"}},
	{goos: "linux", goarch: "amd64", uname: []string{"Linux-x86_64"}},
	{goos: "linux", goarch: "arm64", uname: []string{"Linux-aarch64", "Linux-arm64"}},
	// Windows is reached by the .cmd shim, which PATHEXT resolves for a
	// host asking for `bin/favro-mcp`; `uname` is not a thing it has.
	{goos: "windows", goarch: "amd64"},
}

type platform struct {
	goos, goarch string
	uname        []string
}

func (p platform) dir() string { return p.goos + "-" + p.goarch }

func (p platform) binary() string {
	if p.goos == "windows" {
		return "favro-mcp.exe"
	}
	return "favro-mcp"
}

// staged is every path the packer puts in the bundle, which is what the
// gate holds the manifest against.
func staged() []string {
	out := []string{
		".claude-plugin/plugin.json", ".mcp.json", launcherPath, cmdShimPath,
		"LICENSE", "NOTICE", "README.md",
	}
	for _, p := range platforms {
		out = append(out, path.Join("bin", p.dir(), p.binary()))
	}
	slices.Sort(out)
	return out
}

func pluginGate(w io.Writer, _ []string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	var problems []string

	manifest, err := readJSON(filepath.Join(root, manifestPath))
	if err != nil {
		return err
	}
	if v, _ := manifest["version"].(string); v != placeholderVersion {
		problems = append(problems, fmt.Sprintf(
			"%s says version %q; the committed manifest must carry the placeholder %q, "+
				"or a bundle built from the tree claims a stale version", manifestPath, v, placeholderVersion))
	}
	if name, _ := manifest["name"].(string); name != "favro-mcp" {
		problems = append(problems, fmt.Sprintf("%s names the plugin %q, not favro-mcp", manifestPath, name))
	}

	// Referential, not schematic: the command has to name a file that
	// will actually be in the archive. A schema cannot check this,
	// because the property is about the bundle rather than about the
	// document.
	mcp, err := readJSON(filepath.Join(root, mcpPath))
	if err != nil {
		return err
	}
	command, err := serverCommand(mcp)
	if err != nil {
		problems = append(problems, fmt.Sprintf("%s: %v", mcpPath, err))
	} else {
		want := strings.TrimPrefix(strings.TrimPrefix(command, "${CLAUDE_PLUGIN_ROOT}"), "/")
		if !slices.Contains(staged(), want) {
			problems = append(problems, fmt.Sprintf(
				"%s spawns %q, which the packer does not stage; the bundle would install and then do nothing",
				mcpPath, command))
		}
	}

	// Every platform the bundle claims is a platform GoReleaser builds,
	// and every platform it builds is one a launcher can reach. Both
	// directions: an unreachable binary is dead weight, and a launcher
	// arm with no binary is the failure mode with the worst error
	// message a user can get.
	built, err := goreleaserMatrix(root)
	if err != nil {
		return err
	}
	for _, p := range platforms {
		if !slices.Contains(built, p.dir()) {
			problems = append(problems, fmt.Sprintf(
				"the bundle stages bin/%s, which .goreleaser.yaml does not build", p.dir()))
		}
	}
	for _, b := range built {
		if !slices.ContainsFunc(platforms, func(p platform) bool { return p.dir() == b }) {
			problems = append(problems, fmt.Sprintf(
				"GoReleaser builds %s and no launcher arm reaches it; that binary ships unusable", b))
		}
	}

	// The launcher the packer will write must name every directory it
	// stages, and nothing else. This is the check that reads the
	// generated text rather than trusting the table it came from.
	launcher := launcherScript()
	for _, p := range platforms {
		if p.goos == "windows" {
			continue
		}
		if !strings.Contains(launcher, p.dir()+"/"+p.binary()) {
			problems = append(problems, "the launcher does not dispatch to bin/"+p.dir())
		}
		for _, u := range p.uname {
			if !strings.Contains(launcher, u) {
				problems = append(problems, "the launcher recognises no machine reporting "+u)
			}
		}
	}
	if !strings.Contains(cmdShim(), `windows-amd64\favro-mcp.exe`) {
		problems = append(problems, "the Windows shim does not spawn the staged windows-amd64 binary")
	}

	problems = append(problems, onlyTheServerShips(root)...)

	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	_, err = fmt.Fprintf(w, "plugin manifest ok: %d files staged across %d platforms\n",
		len(staged()), len(platforms))
	return err
}

// onlyTheServerShips holds the premise everything else in this
// repository rests on: the release builds the server and nothing else.
//
// It is why `scripts/gates` may shell out to git and read the working
// tree — the lint config excuses exactly those rules there on the
// grounds that the code never leaves a maintainer's machine — and why
// nothing under scripts/ is held to the tool surface's compatibility
// promise. That premise was true and held by nothing; a second `main:`
// in the build config would quietly ship the gates.
func onlyTheServerShips(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yaml"))
	if err != nil {
		return []string{err.Error()}
	}
	mains := buildMain.FindAllStringSubmatch(code(string(data)), -1)
	if len(mains) == 0 {
		return []string{".goreleaser.yaml names no `main:`; this check is reading the wrong shape"}
	}
	var problems []string
	for _, m := range mains {
		if m[1] != "./cmd/favro-mcp" {
			problems = append(problems, fmt.Sprintf(
				".goreleaser.yaml builds %s as well as the server; everything outside ./cmd is "+
					"maintainer tooling and must not ship", m[1]))
		}
	}
	return problems
}

func readJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// serverCommand digs the one command out of an .mcp.json.
func serverCommand(mcp map[string]any) (string, error) {
	servers, ok := mcp["mcpServers"].(map[string]any)
	if !ok || len(servers) == 0 {
		return "", fmt.Errorf("no mcpServers")
	}
	if len(servers) != 1 {
		return "", fmt.Errorf("%d servers declared; the bundle registers one", len(servers))
	}
	for _, v := range servers {
		entry, ok := v.(map[string]any)
		if !ok {
			return "", fmt.Errorf("the server entry is not an object")
		}
		command, ok := entry["command"].(string)
		if !ok || command == "" {
			return "", fmt.Errorf("the server entry has no command")
		}
		return command, nil
	}
	return "", fmt.Errorf("unreachable")
}

var (
	goosKey    = regexp.MustCompile(`(?m)^\s*goos:\s*$`)
	goarchKey  = regexp.MustCompile(`(?m)^\s*goarch:\s*$`)
	listItem   = regexp.MustCompile(`^\s*-\s*([a-z0-9]+)\s*$`)
	buildMain  = regexp.MustCompile(`(?m)^\s*main:\s*(\S+)`)
	ignorePair = regexp.MustCompile(`(?m)^\s*-\s*goos:\s*([a-z0-9]+)\s*\n\s*goarch:\s*([a-z0-9]+)`)
)

// goreleaserMatrix is every goos-goarch pair the release actually
// builds: the cross product of the two lists, less the ignore entries.
//
// Read with regular expressions rather than a YAML parser, and that is a
// deliberate limit rather than laziness: a parser is a dependency this
// program does not otherwise need, and the file it reads is one this
// repository writes. The floors below are what keep a changed layout
// from reading as an empty matrix.
func goreleaserMatrix(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yaml"))
	if err != nil {
		return nil, err
	}
	text := code(string(data))
	oses := listAfter(text, goosKey)
	arches := listAfter(text, goarchKey)
	if len(oses) < 2 || len(arches) < 2 {
		return nil, fmt.Errorf(".goreleaser.yaml parsed as %d platforms and %d architectures; "+
			"the build block's shape has changed and this check is reading nothing", len(oses), len(arches))
	}
	ignored := map[string]bool{}
	for _, m := range ignorePair.FindAllStringSubmatch(text, -1) {
		ignored[m[1]+"-"+m[2]] = true
	}
	var out []string
	for _, o := range oses {
		for _, a := range arches {
			if pair := o + "-" + a; !ignored[pair] {
				out = append(out, pair)
			}
		}
	}
	slices.Sort(out)
	return out, nil
}

// listAfter returns the YAML list immediately following the first match
// of key: the run of `- value` items that follows it, stopping at the
// first line that is not one.
func listAfter(text string, key *regexp.Regexp) []string {
	loc := key.FindStringIndex(text)
	if loc == nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(text[loc[1]:], "\n") {
		// withoutComment, because listItem anchors on the end of the
		// line: a trailing `# x86-64 only` on a matrix entry would
		// otherwise end the list early, and the gate would then report a
		// platform the build config plainly names as one it does not
		// build.
		entry := withoutComment(line)
		if entry == "" {
			continue
		}
		m := listItem.FindStringSubmatch(entry)
		if m == nil {
			break
		}
		out = append(out, m[1])
	}
	return out
}

// launcherScript is the shim the bundle carries for macOS and Linux. It
// execs rather than calls: the server talks MCP over that process's
// stdio, and a shell left in the middle owns the pipes. On an unknown
// machine it writes to stderr and exits non-zero, because a line of
// English on stdout corrupts the JSON-RPC session before the client's
// first request completes.
func launcherScript() string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n" +
		"# Generated by `gates plugin-pack`. Do not edit.\n" +
		"set -euo pipefail\n" +
		`here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"` + "\n" +
		`case "$(uname -s)-$(uname -m)" in` + "\n")
	for _, p := range platforms {
		if len(p.uname) == 0 {
			continue
		}
		fmt.Fprintf(&b, "    %s) exec \"$here/%s/%s\" \"$@\" ;;\n",
			strings.Join(p.uname, "|"), p.dir(), p.binary())
	}
	b.WriteString("    *)\n" +
		`        echo "favro-mcp: unsupported platform $(uname -s)-$(uname -m); ` +
		`on Windows the .cmd shim should run instead." >&2` + "\n" +
		"        exit 1\n" +
		"        ;;\n" +
		"esac\n")
	return b.String()
}

// cmdShim is the Windows half. CRLF, because cmd.exe parses it that way
// even when the zip was extracted on a Unix host first, and %~dp0 so the
// shim stays relocatable.
func cmdShim() string {
	return "@echo off\r\n" + `"%~dp0windows-amd64\favro-mcp.exe" %*` + "\r\n"
}

// pluginPack assembles the bundle from a GoReleaser dist directory.
//
// The version comes from one place — dist/metadata.json — and goes in
// through a JSON decode and encode, never a substitution over text: the
// manifest is JSON, and a `sed` over JSON is how a quote ends up inside
// a string.
func pluginPack(w io.Writer, args []string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	dist := filepath.Join(root, "dist")
	if len(args) == 1 {
		dist = args[0]
	}
	files, executable, version, err := stageBundle(root, dist)
	if err != nil {
		return err
	}
	out := filepath.Join(root, bundleName)
	if err := writeZip(out, files, executable); err != nil {
		return err
	}
	info, err := os.Stat(out)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "packed %s: version %s, %d files, %d bytes\n",
		bundleName, version, len(files), info.Size())
	return err
}

// stageBundle assembles what the bundle will contain, without writing
// it. Separated from pluginPack so the staging rules — the version
// stamp, the refusal to pack a hand-edited manifest, the mode on every
// binary — can be tested against a synthetic dist directory rather than
// only by packing a real release.
func stageBundle(root, dist string) (files map[string][]byte, executable map[string]bool, version string, err error) {
	meta, err := readJSON(filepath.Join(dist, "metadata.json"))
	if err != nil {
		return nil, nil, "", fmt.Errorf("%w (run `goreleaser release --snapshot --clean --skip=publish` first)", err)
	}
	version, _ = meta["version"].(string)
	if version == "" || version == placeholderVersion {
		return nil, nil, "", fmt.Errorf("dist/metadata.json carries version %q; the packer will not "+
			"stamp a placeholder", version)
	}

	binaries, err := builtBinaries(filepath.Join(dist, "artifacts.json"))
	if err != nil {
		return nil, nil, "", err
	}

	manifest, err := readJSON(filepath.Join(root, manifestPath))
	if err != nil {
		return nil, nil, "", err
	}
	if v, _ := manifest["version"].(string); v != placeholderVersion {
		return nil, nil, "", fmt.Errorf("%s says version %q, not the placeholder; refusing to pack a "+
			"manifest that was edited by hand", manifestPath, v)
	}
	manifest["version"] = version
	stampedManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, nil, "", err
	}

	files = map[string][]byte{}
	files[".claude-plugin/plugin.json"] = append(stampedManifest, '\n')
	for _, name := range []string{".mcp.json", "LICENSE", "NOTICE", "README.md"} {
		src := filepath.Join(root, name)
		if name == ".mcp.json" {
			src = filepath.Join(root, mcpPath)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return nil, nil, "", err
		}
		files[name] = data
	}
	files[launcherPath] = []byte(launcherScript())
	files[cmdShimPath] = []byte(cmdShim())

	executable = map[string]bool{launcherPath: true, cmdShimPath: true}
	for _, p := range platforms {
		src, ok := binaries[p.dir()]
		if !ok {
			return nil, nil, "", fmt.Errorf("dist carries no %s binary; the bundle would ship a "+
				"launcher arm pointing at nothing", p.dir())
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return nil, nil, "", err
		}
		name := path.Join("bin", p.dir(), p.binary())
		files[name] = data
		// The mode has to be set on every staged binary, not only on the
		// entry point: a zip carries the mode, and the Windows binary
		// arrives unrunnable without it.
		executable[name] = true
	}

	if err := versionsAgree(binaries, version); err != nil {
		return nil, nil, "", err
	}
	return files, executable, version, nil
}

// versionsAgree asks the staged binary for the host platform what
// version it thinks it is, and requires it to match the one being
// stamped into the manifest.
//
// The bundle claims a version in five places — its filename, the archive
// filenames, the manifest inside it, the binary's own --version, and
// checksums.txt — and a sibling shipped one that agreed in four of them.
// Only one of the five can be checked from here, and it is the one a
// user actually sees at runtime.
//
// A snapshot build is exempt because goreleaser stamps the binary from
// the last tag and names the archive after the next patch, so the two
// legitimately differ; on a real tag they are the same string. Skipping
// when no staged binary matches the host is deliberate too: a foreign
// architecture cannot be executed, and refusing to pack for that reason
// would be a gate failing on where it ran.
func versionsAgree(binaries map[string]string, version string) error {
	if strings.Contains(version, "snapshot") {
		return nil
	}
	host := runtime.GOOS + "-" + runtime.GOARCH
	path, ok := binaries[host]
	if !ok {
		return nil
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return fmt.Errorf("%s --version: %w", path, err)
	}
	if reported := strings.TrimSpace(string(out)); !strings.Contains(reported, version) {
		return fmt.Errorf("the manifest would say %s and the %s binary reports %q; a bundle whose "+
			"binary disagrees with its manifest is indistinguishable from a correct one until "+
			"somebody runs it", version, host, reported)
	}
	return nil
}

// builtBinaries maps goos-goarch to the path GoReleaser built, read from
// its own manifest rather than by globbing: the layout under dist/
// carries a build id and an amd64 variant suffix, and a glob that
// quietly matched two would pack whichever sorted first — a bundle built
// from the wrong binary is not something a checksum catches, because the
// checksum is of whatever was built.
func builtBinaries(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w (run goreleaser first)", err)
	}
	var artifacts []struct {
		Path   string `json:"path"`
		Goos   string `json:"goos"`
		Goarch string `json:"goarch"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal(data, &artifacts); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := map[string]string{}
	for _, a := range artifacts {
		if a.Type != "Binary" {
			continue
		}
		key := a.Goos + "-" + a.Goarch
		if existing, ok := out[key]; ok {
			return nil, fmt.Errorf("dist carries two %s binaries (%s and %s); packing one of them "+
				"at random is how a bundle ships the wrong build", key, existing, a.Path)
		}
		out[key] = a.Path
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s lists no binaries", path)
	}
	return out, nil
}

// zipModTime is a fixed timestamp for every entry. Identical inputs
// should give a byte-identical archive, and leaving it unset writes
// zeroes that display as the impossible 1980-00-00.
var zipModTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

func writeZip(out string, files map[string][]byte, executable map[string]bool) error {
	if err := os.Remove(out); err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: zipModTime}
		mode := os.FileMode(0o644)
		if executable[name] {
			mode = 0o755
		}
		header.SetMode(mode)
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := w.Write(files[name]); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}
