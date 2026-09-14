package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

// The Claude Desktop bundle.
//
// A .mcpb is a deflate zip with manifest.json at the root and the
// binaries under server/, which archive/zip writes in about forty lines.
// The official packer is Node, and reaching for it would make `make
// check` depend on an interpreter nobody declared.
//
// What the Node CLI buys is validating the manifest against its
// published schema, and that is not the validation worth having. A
// schema says the manifest is well formed. It cannot say that
// entry_point names a file the bundle carries, that a platform
// override's command points at something staged, or that a
// ${user_config.x} refers to a key user_config declares. Each of those
// packs, installs cleanly, and then does nothing.
//
// Three checks here are ones the shared standard names and the sibling
// implementation does not have, because a review found them after it
// shipped:
//
//   - Every platform_overrides entry names a platform
//     compatibility.platforms claims. An override nothing can reach is
//     the same defect as a command nobody staged.
//   - Every platform compatibility.platforms claims spawns the file
//     staged FOR it. Deleting the win32 override passes every other
//     check: Windows then runs the default command, which is the macOS
//     universal binary, and that file really is in the bundle.
//   - The launcher's binary names are the packer's. Where a platform
//     needs one binary per architecture the names live in a shell script
//     no manifest mentions, so nothing else here can see them. This file
//     generates the launcher from the same table it stages from, the way
//     plugin.go does, so the two cannot disagree.

const (
	mcpbTemplateDir  = "packaging/mcpb"
	mcpbManifestPath = mcpbTemplateDir + "/manifest.json"
	// mcpbManifestName is where the manifest sits inside the bundle.
	// The installer looks at the root and nowhere else.
	mcpbManifestName = "manifest.json"
	// mcpbServerDir holds everything the bundle runs.
	mcpbServerDir = "server/"
	// mcpbLauncher is the Linux launcher's name inside the bundle.
	mcpbLauncher = mcpbServerDir + "linux-launch.sh"
	// dirnameRef prefixes a manifest command naming a file inside the
	// installed bundle.
	dirnameRef = "${__dirname}/"
)

// mcpbBinary is one binary the bundle carries: the name it takes inside
// the bundle, the goreleaser artifact it comes from, and either the
// manifest platform it serves directly or the machines the launcher
// dispatches to it.
//
// This is the one table. The packer stages from it, the launcher is
// generated from it, the gate holds the manifest against it, and the
// goreleaser matrix is compared with it — so none of the four can drift
// from the others.
var mcpbBinaries = []mcpbBinary{
	{
		name: mcpbServerDir + "favro-mcp", glob: "*darwin_all*/favro-mcp",
		what: "darwin universal binary", platform: "darwin", goarch: "darwin-all", universal: true,
	},
	{
		name: mcpbServerDir + "favro-mcp.exe", glob: "*windows_amd64*/favro-mcp.exe",
		what: "windows amd64 binary", platform: "win32", goarch: "windows-amd64",
	},
	// Linux has neither a universal binary nor an emulator, and Claude
	// Desktop for Linux ships x64 and arm64 both. These two are reached
	// through the launcher rather than named by the manifest.
	{
		name: mcpbServerDir + "favro-mcp-amd64", glob: "*linux_amd64*/favro-mcp",
		what: "linux amd64 binary", uname: []string{"x86_64", "amd64"}, goarch: "linux-amd64",
	},
	{
		name: mcpbServerDir + "favro-mcp-arm64", glob: "*linux_arm64*/favro-mcp",
		what: "linux arm64 binary", uname: []string{"aarch64", "arm64"}, goarch: "linux-arm64",
	},
}

type mcpbBinary struct {
	name     string
	glob     string
	what     string
	platform string   // the manifest platform whose command names it, if any
	uname    []string // the `uname -m` values the launcher maps to it
	goarch   string   // the goreleaser goos-goarch pair this comes from
	// universal marks the macOS binary goreleaser assembles from two
	// builds. It is not in the goos × goarch matrix, so it is checked
	// against the universal_binaries block instead.
	universal bool
}

// mcpbDocs are the files the bundle carries beside the binaries.
var mcpbDocs = []string{"LICENSE", "NOTICE", "README.md"}

// mcpbStaged is every path the packer puts in the bundle, which is what
// the gate holds the manifest against.
func mcpbStaged() []string {
	out := []string{mcpbManifestName, mcpbLauncher}
	out = append(out, mcpbDocs...)
	for _, b := range mcpbBinaries {
		out = append(out, b.name)
	}
	slices.Sort(out)
	return out
}

// launcherPlatform is the platform reached through the launcher rather
// than by naming a binary directly.
const launcherPlatform = "linux"

// mcpbPlatformFile maps each platform the bundle supports to the file
// that platform must end up running. This is the table behind the check
// the standard names last: a manifest can name a staged file for every
// platform and still hand Windows the macOS binary.
//
// Derived from the one table rather than written out: a row naming a
// platform serves it directly, and any row carrying uname values is
// reached through the launcher, which serves launcherPlatform.
func mcpbPlatformFile() map[string]string {
	out := map[string]string{}
	for _, b := range mcpbBinaries {
		if b.platform != "" {
			out[b.platform] = b.name
		}
		if len(b.uname) > 0 {
			out[launcherPlatform] = mcpbLauncher
		}
	}
	return out
}

// mcpbLauncherScript is the Linux launcher. It execs rather than calls:
// the server talks MCP over this process's stdio, and a shell left in
// the middle owns the pipes. On an unknown machine it writes to stderr
// and exits non-zero, because a line of English on stdout corrupts the
// JSON-RPC session before the client's first request completes.
func mcpbLauncherScript() string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n" +
		"# Generated by `gates mcpb-pack`. Do not edit.\n" +
		"#\n" +
		"# A bundle manifest names a command per platform and has no key for\n" +
		"# the architecture, so the Linux entry would otherwise have to be one\n" +
		"# binary and be wrong for everyone else. macOS solves this with a\n" +
		"# universal binary and Windows by running amd64 under emulation;\n" +
		"# Linux has neither, so the choice is made here.\n" +
		"set -eu\n\n" +
		`dir=$(dirname "$0")` + "\n\n" +
		"case $(uname -m) in\n")
	for _, bin := range mcpbBinaries {
		if len(bin.uname) == 0 {
			continue
		}
		fmt.Fprintf(&b, "  %s)\n    bin=$dir/%s\n    ;;\n",
			strings.Join(bin.uname, " | "), strings.TrimPrefix(bin.name, mcpbServerDir))
	}
	b.WriteString("  *)\n" +
		"    # stderr, because stdout is the JSON-RPC channel.\n" +
		`    echo "favro-mcp: no binary in this bundle for $(uname -m)." \` + "\n" +
		`         "Install with: go install github.com/mmedum/favro-mcp/cmd/favro-mcp@latest" >&2` + "\n" +
		"    exit 1\n" +
		"    ;;\n" +
		"esac\n\n" +
		`if [ ! -x "$bin" ]; then` + "\n" +
		`  echo "favro-mcp: $bin is missing from the bundle or is not executable." >&2` + "\n" +
		"  exit 1\n" +
		"fi\n\n" +
		`exec "$bin" "$@"` + "\n")
	return b.String()
}

// mcpbGate holds the committed manifest against the names the packer
// will stage, which are static — so a manifest that names a file nobody
// will stage is a fact about two files in the repository, caught on the
// commit that writes it rather than at the most expensive moment there
// is. It needs no build and no network.
func mcpbGate(w io.Writer, _ []string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(root, mcpbManifestPath))
	if err != nil {
		return err
	}
	var m bundleManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("%s: %w", mcpbManifestPath, err)
	}

	problems := checkBundleManifest(m, mcpbStaged())

	if m.Version != placeholderVersion {
		problems = append(problems, fmt.Sprintf(
			"%s says version %q; the committed manifest must carry the placeholder %q, or a "+
				"bundle built from the tree claims a stale version", mcpbManifestPath, m.Version, placeholderVersion))
	}
	if m.Name != "favro-mcp" {
		problems = append(problems, fmt.Sprintf("%s names the bundle %q, not favro-mcp", mcpbManifestPath, m.Name))
	}
	problems = append(problems, mcpbLauncherAgrees()...)
	problems = append(problems, mcpbEnvIsReal(root, m)...)

	built, err := goreleaserMatrix(root)
	if err != nil {
		return err
	}
	blocks, err := goreleaserBlocks(root)
	if err != nil {
		return err
	}
	wiring, wiringChecks := mcpbReleaseWiring(blocks)
	problems = append(problems, wiring...)

	_, universal := blocks["universal_binaries"]
	for _, b := range mcpbBinaries {
		switch {
		case b.universal:
			if !universal {
				problems = append(problems, ".goreleaser.yaml has no universal_binaries block, so there is "+
					"no darwin universal binary to stage; a manifest has no key for the architecture, and "+
					"one arch-specific macOS binary is wrong for half of macOS")
			}
		case !slices.Contains(built, b.goarch):
			problems = append(problems, fmt.Sprintf(
				"the bundle stages %s, which .goreleaser.yaml does not build", b.goarch))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	_, err = fmt.Fprintf(w, "mcpb ok: %d files staged, %d platforms, %d user_config keys, "+
		"%d release-wiring lines held\n",
		len(mcpbStaged()), len(m.Compatibility.Platforms), len(m.UserConfig), wiringChecks)
	return err
}

// checkBundleManifest holds a manifest to a staged tree, and reports
// everything wrong rather than the first thing. Shared by the gate,
// which runs against the committed manifest, and the packer, which runs
// against the rendered one — so a bundle cannot be packed past a rule
// the gate enforces.
func checkBundleManifest(m bundleManifest, staged []string) []string {
	packed := make(map[string]bool, len(staged))
	for _, name := range staged {
		packed[name] = true
	}

	// Floors first. checkBundleManifest is what the PACKER runs at
	// release time, where no test is present, and every loop below
	// iterates a collection the manifest supplies — so a manifest that
	// declares almost nothing produces no problems at all and packs. The
	// counts are not typed here; each is "more than none".
	var problems []string
	for _, floor := range []struct {
		n    int
		what string
	}{
		{len(m.Compatibility.Platforms), "compatibility.platforms claims no platform"},
		{len(m.UserConfig), "user_config declares no key, so the install collects nothing"},
		{len(m.Server.MCPConfig.Env), "mcp_config sets no environment, so the server starts unconfigured"},
		{len(m.Server.MCPConfig.PlatformOverrides), "platform_overrides is empty, so every platform runs the default command"},
	} {
		if floor.n == 0 {
			problems = append(problems, floor.what+"; this manifest configures nothing and the checks "+
				"below would each iterate an empty collection")
		}
	}

	switch {
	case m.Server.EntryPoint == "":
		problems = append(problems, "the manifest declares no entry_point")
	case !packed[m.Server.EntryPoint]:
		problems = append(problems, fmt.Sprintf(
			"entry_point names %s, which the bundle does not carry", m.Server.EntryPoint))
	}

	// Every command names a staged file, after stripping ${__dirname}.
	byPlatform := m.commands()
	for _, platform := range slices.Sorted(maps.Keys(byPlatform)) {
		command := byPlatform[platform]
		rel, ok := strings.CutPrefix(command, dirnameRef)
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf(
				"the %s command is %q, which is not under %s, so it names a file outside the bundle",
				platform, command, dirnameRef))
		case !packed[rel]:
			problems = append(problems, fmt.Sprintf(
				"the %s command names %s, which the bundle does not carry", platform, rel))
		}
	}

	// Every ${user_config.x} spent is a key user_config declares. Every
	// reference inside a value, not the value when it is nothing but
	// one: a composed "${user_config.dir}/sub" would otherwise skip the
	// check and the server would start with the variable unsubstituted.
	for _, key := range m.references() {
		if _, ok := m.UserConfig[key]; !ok {
			problems = append(problems, fmt.Sprintf(
				"${user_config.%s} is substituted into the server's configuration and user_config does not declare it", key))
		}
	}

	claimed := m.Compatibility.Platforms
	// An override for a platform the bundle does not claim is
	// unreachable: well formed, packs, and never runs.
	for _, platform := range slices.Sorted(maps.Keys(m.Server.MCPConfig.PlatformOverrides)) {
		if !slices.Contains(claimed, platform) {
			problems = append(problems, fmt.Sprintf(
				"platform_overrides declares %q and compatibility.platforms does not claim it, so nothing "+
					"can ever reach that override", platform))
		}
	}

	// And the other direction, which is the one a staged-file check
	// cannot see: each claimed platform has to spawn the file staged FOR
	// it. Deleting the win32 override leaves a manifest where every
	// command names a file the bundle carries, and Windows runs the
	// macOS binary.
	want := mcpbPlatformFile()
	for _, platform := range claimed {
		expected, known := want[platform]
		if !known {
			problems = append(problems, fmt.Sprintf(
				"compatibility.platforms claims %q and the packer stages nothing for it", platform))
			continue
		}
		command, ok := byPlatform[platform]
		if !ok {
			command = byPlatform["default"]
		}
		if got := strings.TrimPrefix(command, dirnameRef); got != expected {
			problems = append(problems, fmt.Sprintf(
				"on %s the bundle spawns %s, and the file staged for %s is %s — the command names a file "+
					"the bundle carries, which is why nothing else here catches this",
				platform, got, platform, expected))
		}
	}

	// And the last direction: the packer stages a binary for each of
	// these, so a platform the manifest stops claiming ships as dead
	// weight and cannot be installed on. Dropping "darwin" is the exact
	// mirror of dropping the win32 override, and every check above
	// passes for it.
	for _, platform := range slices.Sorted(maps.Keys(want)) {
		if !slices.Contains(claimed, platform) {
			problems = append(problems, fmt.Sprintf(
				"the packer stages %s for %s and compatibility.platforms does not claim it; that binary "+
					"ships as dead weight and the bundle cannot be installed there",
				want[platform], platform))
		}
	}
	return problems
}

// mcpbLauncherAgrees reads the generated launcher rather than trusting
// the table it came from. This is the check that closes the standard's
// last and quietest gap: the launcher is a shell script no manifest
// mentions, so every other check here passes while it dispatches to a
// name nobody stages.
func mcpbLauncherAgrees() []string {
	script := mcpbLauncherScript()
	var problems []string
	dispatched := 0
	for _, b := range mcpbBinaries {
		if len(b.uname) == 0 {
			continue
		}
		dispatched++
		if !strings.Contains(script, "$dir/"+strings.TrimPrefix(b.name, mcpbServerDir)) {
			problems = append(problems, "the launcher does not dispatch to "+b.name)
		}
		for _, u := range b.uname {
			if !strings.Contains(script, u) {
				problems = append(problems, "the launcher recognises no machine reporting "+u)
			}
		}
	}
	if dispatched == 0 {
		problems = append(problems, "no binary in the table is reached through the launcher; "+
			"this check is reading nothing")
	}
	return problems
}

// mcpbEnvIsReal holds the manifest's environment block to the code. The
// bundle is how most people will configure this server, and an env name
// the server stopped reading produces an install that asks for a value,
// stores it, and starts a server that never sees it.
//
// The expected set is derived from the source rather than typed here;
// sourceEnvNames owns both the scan and the floor under it.
func mcpbEnvIsReal(root string, m bundleManifest) []string {
	known, err := sourceEnvNames(root)
	if err != nil {
		return []string{err.Error()}
	}

	var problems []string
	for _, name := range slices.Sorted(maps.Keys(m.env())) {
		if !known[name] {
			problems = append(problems, fmt.Sprintf(
				"the manifest sets %s and no non-test source file reads it; the install would collect a "+
					"value the server never sees", name))
		}
	}
	return problems
}

// topLevelKey matches a goreleaser top-level block header.
var topLevelKey = regexp.MustCompile(`(?m)^([a-z_]+):`)

// goreleaserBlocks splits .goreleaser.yaml into its top-level blocks.
//
// Regular expressions rather than a YAML parser, for the reason
// goreleaserMatrix gives: a parser is a dependency this program does not
// otherwise need, and the file it reads is one this repository writes.
// The floor below is what keeps a changed layout from reading as an
// empty set of blocks, which would pass every check that follows.
func goreleaserBlocks(root string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yaml"))
	if err != nil {
		return nil, err
	}
	text := code(string(data))
	locs := topLevelKey.FindAllStringSubmatchIndex(text, -1)
	blocks := map[string]string{}
	for i, loc := range locs {
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		blocks[text[loc[2]:loc[3]]] = text[loc[1]:end]
	}
	if len(blocks) < 6 {
		return nil, fmt.Errorf(".goreleaser.yaml parsed as %d top-level blocks; its shape has changed "+
			"and these checks are reading nothing", len(blocks))
	}
	return blocks, nil
}

// mcpbReleaseWiring holds the four lines that make the bundle exist, be
// hashed, be signed and be published.
//
// Nothing else here can see them, and that is the point. Delete
// `checksum.extra_files` and the release still succeeds, `make check`
// still passes, the gate above still reports a valid manifest — and the
// bundle ships absent from checksums.txt and therefore outside the
// signature, while README's "Verifying a download" goes on telling
// people it is covered. The standard says both `checksum.extra_files`
// and `release.extra_files`, "or the bundle ships unsigned, or is hashed
// and never published"; this is what holds that sentence.
func mcpbReleaseWiring(blocks map[string]string) (problems []string, checked int) {
	require := func(block, needle, why string) {
		checked++
		body, ok := blocks[block]
		if !ok {
			problems = append(problems, fmt.Sprintf(".goreleaser.yaml has no %s block; %s", block, why))
			return
		}
		if !strings.Contains(body, needle) {
			problems = append(problems, fmt.Sprintf(".goreleaser.yaml's %s block does not mention %q; %s",
				block, needle, why))
		}
	}

	require("universal_binaries", "mcpb-pack",
		"the post hook is the one point where every binary exists and checksums.txt is unwritten, "+
			"which is what makes it possible for the bundle to be hashed with the archives")
	require("checksum", ".mcpb",
		"goreleaser hashes the artifacts IT built, and a file a hook dropped into dist/ is not one of "+
			"them — without this glob the bundle is absent from checksums.txt and so outside the signature")
	require("release", ".mcpb",
		"without this glob the bundle is hashed and never uploaded")
	require("signs", "cosign",
		"the checksum file is signed with a keyless certificate; without it every download is unverifiable")
	require("signs", "artifacts: checksum",
		"the signature has to be over the checksum file, which is what covers the bundle too")
	require("sboms", "archive",
		"one SBOM per archive, so a deployer can see what is inside a binary they did not build")
	require("builds", "mod_timestamp",
		"without it two builds of the same tag differ and `sha256sum -c` on a rebuild means nothing")
	require("archives", "ids:",
		"without an explicit id list the universal binary is archived too, and the release page offers "+
			"a fourth macOS download nobody should have to choose between")
	return problems, checked
}

// mcpbPack writes the bundle from the binaries goreleaser just built.
//
// It runs as the universal binary's post hook, which is the one point in
// the pipeline where every binary exists and checksums.txt has not been
// written. That is what MAKES it possible for the bundle to be in that
// file, and therefore under the signature — it is not what puts it
// there: goreleaser hashes the artifacts it built, and checksum
// extra_files is what covers a file a hook dropped into dist/.
func mcpbPack(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: gates mcpb-pack VERSION [DIST]")
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	version := strings.TrimPrefix(args[0], "v")
	dist := filepath.Join(root, "dist")
	if len(args) > 1 {
		dist = args[1]
	}

	manifest, err := renderBundleManifest(version, filepath.Join(root, mcpbManifestPath))
	if err != nil {
		return err
	}
	var m bundleManifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		return fmt.Errorf("the rendered manifest: %w", err)
	}
	if problems := checkBundleManifest(m, mcpbStaged()); len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}

	files, binaries, err := mcpbLayout(root, dist)
	if err != nil {
		return err
	}
	// The fifth place. A bundle claims a version in its filename, the
	// archive names, the manifest inside it, checksums.txt and the
	// binary's own --version, and a sibling shipped one that agreed in
	// four of them. This is the only one checkable from here, and it is
	// the one a user actually sees at runtime.
	if err := versionsAgree(binaries, version); err != nil {
		return err
	}
	// The manifest first, where an installer reading the archive in
	// order finds it before anything it describes.
	files = append([]bundleFile{{name: mcpbManifestName, body: manifest}}, files...)

	out := filepath.Join(dist, fmt.Sprintf("favro-mcp_%s.mcpb", version))
	if err := writeZipAt(out, files, mcpbMode); err != nil {
		return err
	}
	info, err := os.Stat(out)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "packed %s: version %s, %d files, %d bytes\n",
		filepath.Base(out), version, len(files), info.Size())
	return err
}

// mcpbLayout is everything the bundle carries beside the manifest,
// ordered by the name each file takes inside it — so the archive's order
// is a property of the bundle rather than of how the table happens to be
// arranged.
func mcpbLayout(root, dist string) ([]bundleFile, map[string]string, error) {
	files := []bundleFile{{name: mcpbLauncher, body: []byte(mcpbLauncherScript())}}
	for _, name := range mcpbDocs {
		files = append(files, bundleFile{name: name, from: filepath.Join(root, name)})
	}
	binaries := map[string]string{}
	for _, b := range mcpbBinaries {
		from, err := onlyMatch(dist, b.glob, b.what)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, bundleFile{name: b.name, from: from})
		binaries[b.goarch] = from
		// The universal binary runs on both macOS architectures, so on a
		// macOS host it IS the host binary — under a key versionsAgree
		// would never look up otherwise, leaving the check silently
		// skipped on exactly the platform the bundle's default command
		// points at.
		if b.universal && runtime.GOOS == "darwin" {
			binaries[runtime.GOOS+"-"+runtime.GOARCH] = from
		}
	}
	slices.SortFunc(files, func(a, b bundleFile) int { return strings.Compare(a.name, b.name) })
	return files, binaries, nil
}

// onlyMatch is the one file under dist matching the glob.
//
// Globbing rather than reading dist/artifacts.json, which is what
// plugin.go does and what its comment argues for. The reason is
// ordering, not preference: mcpb-pack runs as the universal binary's
// post hook, and goreleaser writes artifacts.json after the archives —
// measured on a snapshot run, the bundle is written a full second
// before that file exists. The hook is also the only place the bundle
// CAN be written if it is to be in checksums.txt, so the glob is
// forced.
//
// One match or nothing, then: the layout under dist/ carries a build id
// and an amd64 variant, and a glob that quietly matched two would pack
// whichever sorted first. A bundle built from the wrong binary is not
// something a checksum catches — the checksum is of whatever was built.
func onlyMatch(dist, glob, what string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dist, glob))
	if err != nil {
		return "", fmt.Errorf("looking for the %s: %w", what, err)
	}
	matches = slices.DeleteFunc(matches, func(p string) bool {
		info, err := os.Stat(p)
		return err != nil || !info.Mode().IsRegular()
	})
	if len(matches) != 1 {
		found := ""
		if len(matches) > 0 {
			found = ": " + strings.Join(matches, " ")
		}
		return "", fmt.Errorf("expected exactly one %s under %s, found %d%s (run goreleaser first)",
			what, dist, len(matches), found)
	}
	return matches[0], nil
}

// renderBundleManifest is the committed manifest with a real version
// in it. The discipline lives in stampVersion, which both bundles use.
func renderBundleManifest(version, path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return stampVersion(raw, version, path)
}

// bundleManifest is the part of the manifest these gates read.
type bundleManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Server      struct {
		Type       string `json:"type"`
		EntryPoint string `json:"entry_point"`
		MCPConfig  struct {
			Command           string            `json:"command"`
			Args              []string          `json:"args"`
			Env               map[string]string `json:"env"`
			PlatformOverrides map[string]struct {
				Command string            `json:"command"`
				Args    []string          `json:"args"`
				Env     map[string]string `json:"env"`
			} `json:"platform_overrides"`
		} `json:"mcp_config"`
	} `json:"server"`
	UserConfig map[string]struct {
		Type      string `json:"type"`
		Required  bool   `json:"required"`
		Sensitive bool   `json:"sensitive"`
		Default   any    `json:"default"`
	} `json:"user_config"`
	Compatibility struct {
		Platforms []string `json:"platforms"`
	} `json:"compatibility"`
}

// commands is every place the manifest names a file to run, keyed by the
// platform it applies to. Not named `commands` as a package-level
// identifier: that is the registry in main.go.
func (m bundleManifest) commands() map[string]string {
	out := map[string]string{"default": m.Server.MCPConfig.Command}
	for platform, o := range m.Server.MCPConfig.PlatformOverrides {
		if o.Command != "" {
			out[platform] = o.Command
		}
	}
	return out
}

// env is every environment variable the manifest sets, default and
// overrides together.
func (m bundleManifest) env() map[string]string {
	out := map[string]string{}
	maps.Copy(out, m.Server.MCPConfig.Env)
	for _, o := range m.Server.MCPConfig.PlatformOverrides {
		maps.Copy(out, o.Env)
	}
	return out
}

// userConfigRef matches a ${user_config.KEY} substitution.
var userConfigRef = regexp.MustCompile(`\$\{user_config\.([^}]+)\}`)

// references is every ${user_config.KEY} the manifest substitutes into a
// command, an argument or an environment variable.
func (m bundleManifest) references() []string {
	var refs []string
	collect := func(vals ...string) {
		for _, v := range vals {
			for _, match := range userConfigRef.FindAllStringSubmatch(v, -1) {
				refs = append(refs, match[1])
			}
		}
	}
	c := m.Server.MCPConfig
	collect(c.Command)
	collect(c.Args...)
	for _, v := range c.Env {
		collect(v)
	}
	for _, o := range c.PlatformOverrides {
		collect(o.Command)
		collect(o.Args...)
		for _, v := range o.Env {
			collect(v)
		}
	}
	slices.Sort(refs)
	return slices.Compact(refs)
}

// mcpbMode decides the mode from the entry's name rather than from the
// file it came from. The source mode is not the answer: goreleaser
// leaves a binary executable on Linux and the Windows build has no such
// bit to carry, and a checkout on a filesystem without modes has none at
// all. The packer forces the execute bit on the entry point alone, so
// everything else under server/ arrives unrunnable unless it is staged
// 0755 here.
func mcpbMode(name string) fs.FileMode {
	if strings.HasPrefix(name, mcpbServerDir) {
		return 0o755
	}
	return 0o644
}
