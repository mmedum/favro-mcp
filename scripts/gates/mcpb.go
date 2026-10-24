package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/mmedum/favro-mcp/v4/internal/config"
	"github.com/mmedum/favro-mcp/v4/internal/tools"
)

// The Claude Desktop bundle.
//
// A .mcpb is a deflate zip with manifest.json at the root and the
// binaries under server/, which archive/zip writes in about forty lines.
// The official packer is Node, and reaching for it would make `make
// check` depend on an interpreter nobody declared.
//
// What the Node CLI buys is validating the manifest against its
// published schema, and that is the lesser half. A schema says the
// manifest is well formed. It cannot say that entry_point names a
// program the bundle carries, that a platform override's command points
// at something staged, or that a ${user_config.x} refers to a key
// user_config declares. Each of those packs, installs cleanly, and then
// does nothing. The manifest is held to the vendored schema as well
// (schemavendor.go), and to the staged tree here.
//
// Three checks here are ones the shared standard names and the sibling
// implementation did not have, because a review found them after it
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
//
// The gate and the packer start from one function, committedManifest,
// which reads the manifest, go.mod, LICENSE and .goreleaser.yaml once
// from one root, generates the launcher for go.mod's module, and runs
// every check. A bundle cannot be packed past a rule the gate enforces.

const (
	mcpbTemplateDir  = "packaging/mcpb"
	mcpbManifestPath = mcpbTemplateDir + "/manifest.json"
	// mcpbManifestName is where the manifest sits inside the bundle.
	// The installer looks at the root and nowhere else.
	mcpbManifestName = "manifest.json"
	// mcpbServerDir holds everything the bundle runs.
	mcpbServerDir = "server/"
	// mcpbLauncher is the Linux launcher's name inside the bundle.
	mcpbLauncher = mcpbServerDir + "launch-linux.sh"
	// dirnameRef starts every command: ${__dirname} is the one path the
	// specification fixes to the bundle. It shows a relative command
	// once and never says what it is resolved against, and the reference
	// host resolves no path.
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
		name: mcpbServerDir + "favro-mcp-darwin", glob: "*darwin_all*/favro-mcp",
		what: "darwin universal binary", platform: "darwin", goarch: "darwin-all", universal: true,
	},
	// Windows on arm64 runs this amd64 binary under emulation, as
	// releaseMatrix says. The release carries a windows_arm64 archive
	// too; the bundle does not need it, since a manifest names one
	// command per platform.
	{
		name: mcpbServerDir + "favro-mcp.exe", glob: "*windows_amd64*/favro-mcp.exe",
		what: "windows amd64 binary", platform: "win32", goarch: "windows-amd64",
	},
	// Linux has neither a universal binary nor an emulator, and Claude
	// Desktop for Linux ships x64 and arm64 both. These two are reached
	// through the launcher rather than named by the manifest.
	{
		name: mcpbServerDir + "favro-mcp-linux-amd64", glob: "*linux_amd64*/favro-mcp",
		what: "linux amd64 binary", uname: []string{"x86_64", "amd64"}, goarch: "linux-amd64",
	},
	{
		name: mcpbServerDir + "favro-mcp-linux-arm64", glob: "*linux_arm64*/favro-mcp",
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

// mcpbDocs are the files the bundle carries beside the binaries, to be
// read. Nothing may spawn them.
var mcpbDocs = []string{"LICENSE", "NOTICE", "README.md"}

// mcpbStaged is every path the packer puts in the bundle.
func mcpbStaged() []string {
	out := append([]string{mcpbManifestName}, mcpbDocs...)
	out = append(out, mcpbPrograms()...)
	slices.Sort(out)
	return out
}

// mcpbPrograms is every staged file the bundle runs: the binaries and
// the launcher. An entry_point or a command resolves against these
// alone, never against the manifest, LICENSE, NOTICE or README.md.
func mcpbPrograms() []string {
	out := []string{mcpbLauncher}
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
// JSON-RPC session before the client's first request completes. Its
// install hint names module, go.mod's, rather than a path typed here:
// the path moves with every major version.
func mcpbLauncherScript(module string) string {
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
		`         "Install with: go install ` + module + "/cmd/" + tools.ServerName + `@latest" >&2` + "\n" +
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
	b, problems, err := committedManifest(root)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	m := b.manifest
	_, err = fmt.Fprintf(w, "mcpb ok: %d files staged, %d platforms, %d user_config keys from the %d variables "+
		"the server reads\n", len(mcpbStaged()), len(m.Compatibility.Platforms), len(m.UserConfig), len(config.Vars()))
	return err
}

// committedBundle is what the gate and the packer read from one root:
// the committed manifest as written and as decoded, and the launcher
// generated for the module go.mod declares.
type committedBundle struct {
	raw      []byte
	manifest bundleManifest
	launcher string
}

// committedManifest is where the gate and the packer both start: the
// committed manifest under root, read once and decoded once, the
// launcher for the module go.mod under root declares, and every problem
// manifestProblems finds in them. The packer stamps the version into
// these bytes and holds what it stamps to the vendored schema too.
//
// A key repeated in one object is refused before anything else is
// checked, and so are two that differ only in case. The decode into
// bundleManifest merges a repeated object into the first and matches a
// struct field's key in any case, while the schema validator and the
// host each keep the last and match exactly, so every other check would
// read a document that is not the one shipped.
func committedManifest(root string) (b committedBundle, problems []string, err error) {
	b.raw, err = os.ReadFile(filepath.Join(root, mcpbManifestPath))
	if err != nil {
		return committedBundle{}, nil, err
	}
	if err := json.Unmarshal(b.raw, &b.manifest); err != nil {
		return b, []string{fmt.Sprintf("%s does not decode: %v", mcpbManifestPath, err)}, nil
	}
	repeated, err := repeatedKeys(b.raw)
	if err != nil {
		return b, []string{fmt.Sprintf("%s cannot be read key by key, so a repeated key cannot be ruled out: %v",
			mcpbManifestPath, err)}, nil
	}
	if len(repeated) > 0 {
		for _, at := range repeated {
			problems = append(problems, fmt.Sprintf("%s has %s more than once, counting keys that differ only in "+
				"case; the gate's decode merges them, and the schema and the host keep the last, so they would "+
				"read different documents", mcpbManifestPath, at))
		}
		return b, problems, nil
	}
	module, err := modulePath(root)
	if err != nil {
		problems = append(problems, fmt.Sprintf("%v, so the launcher can say nothing of how to install", err))
	}
	b.launcher = mcpbLauncherScript(module)
	problems = append(problems, manifestProblems(b, root)...)
	slices.Sort(problems)
	return b, problems, nil
}

// repeatedKeys is the path of every key an object in raw has more than
// once, each once, in document order. Two keys are one when they are
// equal under Unicode case folding, since Go's decode matches a struct
// field's key that way: the path names the second spelling. A path is
// "server" at the top level, "server.mcp_config.env.FAVRO_UPLOAD_DIR"
// inside, [i] for an element of a list, and ["a.b"] for a key holding a
// dot, a bracket or a quote, so no two keys read as one path. A document
// the walk cannot read is an error rather than one with no repeats.
func repeatedKeys(raw []byte) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var repeated []string
	var walk func(path string) error
	walk = func(path string) error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch tok {
		case json.Delim('{'):
			// seen is each spelling met so far, and reported whether its
			// fold has been named already.
			var seen []string
			reported := map[int]bool{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return err
				}
				key, _ := keyTok.(string)
				at := keyPath(path, key)
				if i := slices.IndexFunc(seen, func(k string) bool { return strings.EqualFold(k, key) }); i < 0 {
					seen = append(seen, key)
				} else if !reported[i] {
					reported[i] = true
					repeated = append(repeated, at)
				}
				if err := walk(at); err != nil {
					return err
				}
			}
		case json.Delim('['):
			for i := 0; dec.More(); i++ {
				if err := walk(fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		default:
			return nil
		}
		_, err = dec.Token() // the closing brace or bracket
		return err
	}
	if err := walk(""); err != nil {
		return nil, err
	}
	return repeated, nil
}

// keyPath is the path of key inside the object at path: joined by a dot,
// or quoted in brackets when the key holds a character a path uses.
func keyPath(path, key string) string {
	switch {
	case strings.ContainsAny(key, `.[]"`) || key == "":
		return path + "[" + strconv.Quote(key) + "]"
	case path == "":
		return key
	}
	return path + "." + key
}

// manifestProblems is every check the gate and the packer both run, so
// the release packs no manifest the gate would refuse: the referential
// checks against the staged programs, the manifest's own declaration,
// the placeholder version, what the bundle says it is, the settings it
// asks for, the launcher, and the repository files beside the manifest,
// read from root.
func manifestProblems(b committedBundle, root string) []string {
	m := b.manifest
	problems := checkBundleManifest(m, mcpbPrograms())
	if m.Version != placeholderVersion {
		problems = append(problems, fmt.Sprintf(
			"%s says version %q; the committed manifest must carry the placeholder %q, or a "+
				"bundle built from the tree claims a stale version", mcpbManifestPath, m.Version, placeholderVersion))
	}
	problems = append(problems, identityProblems(m)...)
	problems = append(problems, settingProblems(m, bundleLeftOut)...)
	problems = append(problems, mcpbLauncherAgrees(b.launcher)...)
	problems = append(problems, repositoryProblems(m, root)...)
	// And against the schema the manifest cites, which the declaration
	// checks only name: a document can cite the right file and not
	// satisfy it.
	if err := validateDocument(mcpbSchemaFile, mcpbManifestPath, b.raw); err != nil {
		problems = append(problems, err.Error())
	}
	return problems
}

// checkBundleManifest holds a manifest to the programs the packer
// stages, and reports everything wrong rather than the first thing.
func checkBundleManifest(m bundleManifest, programs []string) []string {
	isProgram := make(map[string]bool, len(programs))
	for _, name := range programs {
		isProgram[name] = true
	}

	// Floors first. Every loop below iterates a collection the manifest
	// supplies, so a manifest that declares almost nothing would produce
	// no problems at all. The counts are not typed here; each is "more
	// than none".
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

	problems = append(problems, m.declarationProblems()...)

	switch {
	case m.Server.EntryPoint == "":
		problems = append(problems, "the manifest declares no entry_point")
	case !isProgram[m.Server.EntryPoint]:
		problems = append(problems, fmt.Sprintf(
			"entry_point names %s, which is not a program the packer stages", m.Server.EntryPoint))
	}

	// Every command starts with ${__dirname}/ and names a program the
	// packer stages. runs is the program each one spawns; a command that
	// names none is reported here and left out, so the platform check
	// below does not report it again.
	byPlatform := m.commands()
	runs := map[string]string{}
	for _, platform := range slices.Sorted(maps.Keys(byPlatform)) {
		command := byPlatform[platform]
		rel, prefixed := strings.CutPrefix(command, dirnameRef)
		switch {
		case !prefixed:
			problems = append(problems, fmt.Sprintf("the %s command is %q; a command starts with %s, the one "+
				"path the specification fixes to the bundle", platform, command, dirnameRef))
		case !isProgram[rel]:
			problems = append(problems, fmt.Sprintf(
				"the %s command names %s, which is not a program the packer stages", platform, rel))
		default:
			runs[platform] = rel
		}
	}
	// entry_point is the file the default command runs, so the bundle
	// does not name one file as the server and spawn another.
	if rel, ok := runs["default"]; ok && isProgram[m.Server.EntryPoint] && rel != m.Server.EntryPoint {
		problems = append(problems, fmt.Sprintf("entry_point is %s, and the default command runs %s",
			m.Server.EntryPoint, rel))
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
	// command names a program the bundle carries, and Windows runs the
	// macOS binary.
	want := mcpbPlatformFile()
	for _, platform := range claimed {
		expected, known := want[platform]
		if !known {
			problems = append(problems, fmt.Sprintf(
				"compatibility.platforms claims %q and the packer stages nothing for it", platform))
			continue
		}
		from := platform
		if _, ok := byPlatform[platform]; !ok {
			from = "default"
		}
		if got, ok := runs[from]; ok && got != expected {
			problems = append(problems, fmt.Sprintf(
				"on %s the bundle spawns %s, and the file staged for %s is %s — the command names a program "+
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

// identityProblems holds what the manifest says it is: the server's own
// name, a binary, each platform once, and a privacy policy. The manifest
// specification requires privacy_policies of a server that connects to
// a service processing user data, as this one does to Favro, and neither
// the schema nor `mcpb validate` enforces it. That the platforms are
// exactly the ones the packer stages for is checkBundleManifest's, both
// ways.
func identityProblems(m bundleManifest) []string {
	var problems []string
	if m.Name != tools.ServerName {
		problems = append(problems, fmt.Sprintf("the manifest's name is %q and the server's is %q",
			m.Name, tools.ServerName))
	}
	if m.Server.Type != "binary" {
		problems = append(problems, fmt.Sprintf("server.type is %q; the bundle carries a binary", m.Server.Type))
	}
	sorted := slices.Sorted(slices.Values(m.Compatibility.Platforms))
	if len(slices.Compact(slices.Clone(sorted))) != len(sorted) {
		problems = append(problems, fmt.Sprintf("compatibility.platforms is %v, which claims a platform twice", sorted))
	}
	if len(m.PrivacyPolicies) == 0 {
		problems = append(problems, "privacy_policies is empty; the manifest specification requires one for a "+
			"server that connects to a service processing user data, as this one does to Favro")
	}
	return problems
}

// mcpbLauncherAgrees reads the generated launcher, script, rather than
// trusting the table it came from. This is the check that closes the
// standard's last and quietest gap: the launcher is a shell script no
// manifest mentions, so every other check here passes while it
// dispatches to a name nobody stages.
func mcpbLauncherAgrees(script string) []string {
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
				problems = append(problems, "the launcher recognizes no machine reporting "+u)
			}
		}
	}
	if dispatched == 0 {
		problems = append(problems, "no binary in the table is reached through the launcher; "+
			"this check is reading nothing")
	}
	return problems
}

// repositoryProblems holds the manifest to the files beside it under
// root: the license to LICENSE, and the staged table and the release
// wiring to .goreleaser.yaml.
func repositoryProblems(m bundleManifest, root string) []string {
	var problems []string
	switch want, err := licenseOf(root); {
	case err != nil:
		problems = append(problems, err.Error())
	case m.License != want:
		problems = append(problems, fmt.Sprintf("the manifest says the license is %q and LICENSE is %s", m.License, want))
	}
	return append(problems, releaseProblems(root)...)
}

// modulePath is the module go.mod under root declares: the path after
// `module` and a space or a tab, without a trailing // comment or the
// quotes go.mod allows around it, on a line that may end in a carriage
// return, since Git checks go.mod out with CRLF on Windows. Every gate
// that needs the module path reads it here.
func modulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		code, _, _ := strings.Cut(line, "//")
		rest, ok := strings.CutPrefix(strings.TrimSpace(code), "module")
		if !ok || rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		if path := strings.Trim(strings.TrimSpace(rest), `"`); path != "" {
			return path, nil
		}
	}
	return "", errors.New("go.mod names no module")
}

// licenseOf reads the SPDX identifier the LICENSE file under root
// carries, so the manifest cannot claim a different one, and refuses a
// license it does not recognize: one it cannot name holds the manifest
// to nothing.
func licenseOf(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "LICENSE"))
	if err != nil {
		return "", fmt.Errorf("cannot read LICENSE: %w", err)
	}
	switch text := string(data); {
	case strings.Contains(text, "Apache License"):
		return "Apache-2.0", nil
	case strings.Contains(text, "MIT License"):
		return "MIT", nil
	}
	return "", errors.New("LICENSE is neither the Apache License nor the MIT License, so nothing holds the " +
		"manifest's license to it; teach licenseOf the new one")
}

// Why bundleLeftOut leaves a setting out.
const (
	diagnostics = "diagnostics, set by hand while chasing a problem"
)

// bundleLeftOut is every variable the server reads that the bundle does
// not ask for, with why. A removed setting is left out without an entry,
// because config says which it is.
var bundleLeftOut = map[string]string{
	config.EnvLogLevel:     diagnostics,
	config.EnvSkipValidate: "a development override, for protocol-only tests that never reach Favro",
}

// bundleVarsFrom is the settings the bundle asks for at install: every
// variable the server reads, vars, less the removed ones and less each
// one leftOut gives a reason for. A setting added later is asked for
// until the manifest asks for it or a reason is written. It also refuses
// an exclusion the server has no use for: one naming a variable it does
// not read, or one config already says is removed.
func bundleVarsFrom(vars []config.Var, leftOut map[string]string) (asked []config.Var, problems []string) {
	byName := make(map[string]config.Var, len(vars))
	for _, v := range vars {
		byName[v.Name] = v
		if !v.Removed && leftOut[v.Name] == "" {
			asked = append(asked, v)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(leftOut)) {
		v, read := byName[name]
		switch {
		case !read:
			problems = append(problems, fmt.Sprintf("bundleLeftOut names %s, which the server does not read", name))
		case v.Removed:
			problems = append(problems, fmt.Sprintf("bundleLeftOut names %s, which a release removed, "+
				"and so left out already", name))
		}
	}
	return asked, problems
}

// bundleKey is the user_config key a setting is asked under: its name
// without the prefix, lowercased, which gives the family's bundles the
// same keys.
func bundleKey(name string) string {
	return strings.ToLower(strings.TrimPrefix(name, config.EnvPrefix))
}

// settingProblems holds what the bundle asks for to the server's own
// settings, and reports everything wrong rather than the first thing.
//
// Every setting bundleVarsFrom derives is asked for and reaches its
// variable, and nothing else is. A switch is asked as a boolean, so
// Claude Desktop shows it as one, and never as required: it has the
// server's default, and a required key with no default would leave its
// variable out of the defaults check. A credential config marks secret
// is asked as sensitive, so the form does not show it, and the bundle
// asks for at least one. An optional key the environment spends has a
// default, because the reference host passes ${user_config.x} to the
// server as written when it has none and the person leaves the field
// blank, and no default holds a ${...} reference. The environment the defaults give the server configures it as
// no environment does: a bundle install may turn on nothing an
// unconfigured server leaves off, because in Claude Desktop the only
// other guard is its approval of each tool, which a person can set to
// always allow. The sets of switches the server refuses together are
// the ones bundleRefused declares. mcp_config.args is there and empty,
// and a platform override carries neither env nor args, an empty one
// included: the reference host replaces the base's with an override's
// rather than merging, and an argument would configure the server past
// every check on env. leftOut is bundleLeftOut, or a test's.
func settingProblems(m bundleManifest, leftOut map[string]string) []string {
	vars := config.Vars()
	asked, problems := bundleVarsFrom(vars, leftOut)

	const keepArgs = "keep it as [], since no Claude Desktop launch has shown the key optional"
	switch raw, args := m.Server.MCPConfig.Args, m.args(); {
	case raw == nil:
		problems = append(problems, "mcp_config.args is missing; "+keepArgs)
	case string(raw) == "null":
		problems = append(problems, "mcp_config.args is null; "+keepArgs)
	case len(args) > 0:
		problems = append(problems, fmt.Sprintf("mcp_config.args is %q; the bundle passes every setting "+
			"through mcp_config.env, and an argument would configure the server past these checks", args))
	}
	for _, platform := range slices.Sorted(maps.Keys(m.Server.MCPConfig.PlatformOverrides)) {
		over := m.Server.MCPConfig.PlatformOverrides[platform]
		if over.Env != nil {
			problems = append(problems, fmt.Sprintf("platform_overrides.%s.env is set; a host replaces "+
				"mcp_config.env with it, an empty one too, so put every variable in mcp_config.env", platform))
		}
		if over.Args != nil {
			problems = append(problems, fmt.Sprintf("platform_overrides.%s.args is set; a host replaces "+
				"mcp_config.args with it, an empty one too, and the bundle passes every setting through "+
				"mcp_config.env", platform))
		}
	}

	var switches []string
	secrets := 0
	askedName, askedKey := map[string]bool{}, map[string]bool{}
	for _, v := range asked {
		key := bundleKey(v.Name)
		askedName[v.Name], askedKey[key] = true, true
		opt, ok := m.UserConfig[key]
		if !ok {
			problems = append(problems, fmt.Sprintf("user_config does not ask for %s, which sets %s", key, v.Name))
			continue
		}
		if got, want := m.Server.MCPConfig.Env[v.Name], "${user_config."+key+"}"; got != want {
			problems = append(problems, fmt.Sprintf("mcp_config.env.%s is %q, want %q", v.Name, got, want))
		}
		if v.Switch {
			switches = append(switches, key)
			if opt.Type != "boolean" {
				problems = append(problems, fmt.Sprintf("user_config.%s is a %s; %s is a switch, so ask for a boolean",
					key, cmp.Or(opt.Type, "field with no type"), v.Name))
			}
			if opt.Required {
				problems = append(problems, fmt.Sprintf("user_config.%s is required; %s is a switch, which has the "+
					"server's default, so it is never required", key, v.Name))
			}
		}
		if v.Secret {
			secrets++
			if !opt.Sensitive {
				problems = append(problems, fmt.Sprintf("user_config.%s is not sensitive; %s is a credential, so "+
					"Claude Desktop must store it as a secret and never show it", key, v.Name))
			}
		}
	}
	if secrets == 0 {
		problems = append(problems, "the bundle asks for no variable config.Vars marks secret, so nothing holds "+
			"a credential's field to sensitive")
	}
	for _, key := range slices.Sorted(maps.Keys(m.UserConfig)) {
		if !askedKey[key] {
			problems = append(problems, fmt.Sprintf("user_config asks for %s, which is no setting's key", key))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(m.Server.MCPConfig.Env)) {
		switch {
		case askedName[name]:
		case slices.ContainsFunc(vars, func(v config.Var) bool { return v.Name == name && v.Removed }):
			problems = append(problems, fmt.Sprintf("mcp_config.env sets %s, which a release removed", name))
		case leftOut[name] != "":
			problems = append(problems, fmt.Sprintf("mcp_config.env sets %s, which the bundle leaves out: %s",
				name, leftOut[name]))
		default:
			problems = append(problems, fmt.Sprintf("mcp_config.env sets %s, which the server does not read", name))
		}
	}
	problems = append(problems, mcpbSpentOptionProblems(m)...)
	problems = append(problems, referencingDefaults(m)...)
	slices.Sort(switches)
	defaults, defaultsErr := defaultsProblems(m)
	problems = append(problems, defaults...)
	return append(problems, refusalProblems(m, switches, bundleRefused, defaultsErr)...)
}

// referencingDefaults refuses a user_config default holding a ${...}
// reference, naming the key. The host expands ${HOME}, ${DOCUMENTS},
// ${__dirname}, ${/} and the rest in a default as well as
// ${user_config.x}, and the defaults check substitutes only the last, so
// it would start the server from a value no install gives it. Every env
// value is exactly ${user_config.<key>}, so a default has no reason to
// hold one.
func referencingDefaults(m bundleManifest) []string {
	var problems []string
	for _, key := range slices.Sorted(maps.Keys(m.UserConfig)) {
		if d, ok := m.UserConfig[key].defaultValue().(string); ok && strings.Contains(d, "${") {
			problems = append(problems, fmt.Sprintf("user_config.%s defaults to %q, which holds a ${...} reference "+
				"the host expands and the defaults check does not; give it a plain value", key, d))
		}
	}
	return problems
}

// mcpbSpentOptionProblems holds every key an environment value spends to
// one a host substitutes. The reference host leaves ${user_config.x} as
// written when the key is optional with no default, so a blank field
// would reach the server as that text, and it does not substitute an
// array into a string. A key nothing declares is checkBundleManifest's
// own rule.
func mcpbSpentOptionProblems(m bundleManifest) []string {
	env := m.Server.MCPConfig.Env
	var problems []string
	for _, name := range slices.Sorted(maps.Keys(env)) {
		for _, match := range userConfigRef.FindAllStringSubmatch(env[name], -1) {
			opt, ok := m.UserConfig[match[1]]
			switch {
			case !ok:
			case opt.Multiple:
				problems = append(problems, fmt.Sprintf("mcp_config.env.%s spends ${user_config.%s}, which takes "+
					"multiple values; a host does not substitute an array into a string", name, match[1]))
			case !opt.Required && opt.defaultValue() == nil:
				problems = append(problems, fmt.Sprintf("mcp_config.env.%s spends ${user_config.%s}, which is optional "+
					"with no default; a host leaves it as written when the person sets nothing", name, match[1]))
			}
		}
	}
	return problems
}

// hostEnv is the environment the reference host builds from the
// bundle's defaults, with each key in turned set to the value turnedTo
// gives it, as the person changing that field would. A boolean becomes
// "true" or "false", a "" default stays "", a number is written out,
// and a reference with no default is passed through as written.
//
// A variable spending a required key with no default is left out, since
// the person supplies it and it is theirs either way: the credentials
// are those. One with a default is not: Claude Desktop's form may fill
// it in from the default, and a person may accept it unread. A default
// that is a list or an object is not guessed at: its variable is left
// out, and its key is in unreadable with what it is.
//
// It reads mcp_config.env alone. That is sound only because
// settingProblems refuses an argument and an override's env; relaxing
// either needs this to read the same.
func hostEnv(m bundleManifest, turned ...string) (env, unreadable map[string]string) {
	env, unreadable = map[string]string{}, map[string]string{}
	for name, value := range m.Server.MCPConfig.Env {
		var b strings.Builder
		skip, last := false, 0
		for _, at := range userConfigRef.FindAllStringSubmatchIndex(value, -1) {
			key, sub := value[at[2]:at[3]], value[at[0]:at[1]]
			opt := m.UserConfig[key]
			def := opt.defaultValue()
			if slices.Contains(turned, key) {
				def = opt.turnedTo()
			}
			switch d := def.(type) {
			case bool:
				sub = strconv.FormatBool(d)
			case string:
				sub = d
			case float64:
				sub = strconv.FormatFloat(d, 'f', -1, 64)
			case nil:
				skip = skip || opt.Required
			case []any:
				unreadable[key], skip = "a list", true
			case map[string]any:
				unreadable[key], skip = "an object", true
			}
			b.WriteString(value[last:at[0]] + sub)
			last = at[1]
		}
		if !skip {
			env[name] = b.String() + value[last:]
		}
	}
	return env, unreadable
}

// loadEnv starts the server's configuration from an environment alone.
// The server refuses to start while Refusals holds anything, so that is
// the error.
func loadEnv(env map[string]string) (config.Config, error) {
	c := config.LoadFrom(func(name string) string { return env[name] })
	if len(c.Refusals) > 0 {
		return c, errors.New(strings.Join(c.Refusals, "; "))
	}
	return c, nil
}

// defaultsProblems reports a bundle whose defaults configure the server
// otherwise than no environment does, naming the fields that differ. A
// default that is a list or an object is reported rather than guessed
// at, every one in key order, and the comparison still runs over the
// rest. defaultsErr is what the server refuses the defaults with, which
// decides whether refusalProblems starts the sets of switches.
func defaultsProblems(m bundleManifest) (problems []string, defaultsErr error) {
	env, unreadable := hostEnv(m)
	for _, key := range slices.Sorted(maps.Keys(unreadable)) {
		problems = append(problems, fmt.Sprintf("user_config.%s defaults to %s, which the defaults check cannot "+
			"build an environment from: give it a boolean, a string or a number", key, unreadable[key]))
	}
	got, defaultsErr := loadEnv(env)
	if defaultsErr != nil {
		problems = append(problems, fmt.Sprintf("the environment the bundle's defaults give the server is refused: %v",
			defaultsErr))
		return problems, defaultsErr
	}
	want, err := loadEnv(nil)
	if err != nil {
		return append(problems, fmt.Sprintf("an unconfigured server is refused: %v", err)), nil
	}
	if diff := fieldDiff(got, want); len(diff) > 0 {
		var set []string
		for _, name := range slices.Sorted(maps.Keys(env)) {
			set = append(set, fmt.Sprintf("%s=%q", name, env[name]))
		}
		problems = append(problems, fmt.Sprintf("the bundle's defaults configure the server otherwise than no "+
			"environment does, in %s, from %s; a bundle install may turn on nothing an unconfigured server leaves off",
			strings.Join(diff, ", "), strings.Join(set, " ")))
	}
	return problems, nil
}

// fieldDiff names the fields of two values of one struct type that
// differ. An unexported field cannot be read out through Interface, so
// it is compared with Value.Equal where that compares what its type
// holds, and named; any other that differs, whatever else does, is
// reported unnamed, once.
func fieldDiff(a, b any) []string {
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	var fields []string
	unnamed := false
	for i := range va.NumField() {
		f := va.Type().Field(i)
		switch {
		case f.IsExported():
			if !reflect.DeepEqual(va.Field(i).Interface(), vb.Field(i).Interface()) {
				fields = append(fields, f.Name)
			}
		case strictlyComparable(f.Type):
			if !va.Field(i).Equal(vb.Field(i)) {
				fields = append(fields, f.Name)
			}
		default:
			unnamed = unnamed || !sameValue(va.Field(i), vb.Field(i))
		}
	}
	if unnamed {
		fields = append(fields, "an unexported field")
	}
	return fields
}

// strictlyComparable reports whether Value.Equal compares every value of
// t by what it holds. An interface type compares as a type, but Equal
// panics on a dynamic value that does not; a pointer, a channel, a func
// and an unsafe pointer compare by address. One anywhere inside t rules
// it out.
func strictlyComparable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return false
	case reflect.Struct:
		for i := range t.NumField() {
			if !strictlyComparable(t.Field(i).Type) {
				return false
			}
		}
		return true
	case reflect.Array:
		return strictlyComparable(t.Elem())
	}
	return t.Comparable()
}

// sameValue is reflect.DeepEqual for two values of one type, read
// through the accessors an unexported field allows, since DeepEqual
// cannot be handed one. A configuration holds no cycle, so none is
// followed.
func sameValue(x, y reflect.Value) bool {
	switch x.Kind() {
	case reflect.Pointer, reflect.Interface:
		if x.IsNil() || y.IsNil() {
			return x.IsNil() == y.IsNil()
		}
		return x.Elem().Type() == y.Elem().Type() && sameValue(x.Elem(), y.Elem())
	case reflect.Slice:
		if x.IsNil() != y.IsNil() || x.Len() != y.Len() {
			return false
		}
		fallthrough
	case reflect.Array:
		for i := range x.Len() {
			if !sameValue(x.Index(i), y.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Map:
		if x.IsNil() != y.IsNil() || x.Len() != y.Len() {
			return false
		}
		for _, key := range x.MapKeys() {
			if v := y.MapIndex(key); !v.IsValid() || !sameValue(x.MapIndex(key), v) {
				return false
			}
		}
		return true
	case reflect.Struct:
		for i := range x.NumField() {
			if !sameValue(x.Field(i), y.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Func:
		// As DeepEqual has it: two funcs are equal only when both are nil.
		return x.IsNil() && y.IsNil()
	case reflect.Chan, reflect.UnsafePointer:
		return x.Pointer() == y.Pointer()
	}
	return x.Equal(y)
}

// bundleRefused are the sets of the bundle's switches the server refuses
// at start, each sorted, each switch in a set turned from its default.
// Each field in one names the others' titles in its description. The
// server refuses none: the bundle's one switch is enable_destructive,
// and config.LoadFrom refuses only a removed setting still turned on,
// which the bundle never sets. The gate starts every set of the switches
// and holds the refused ones to this list, both ways, so a refusal added
// to LoadFrom fails here until it is declared and the fields say so.
var bundleRefused [][]string

// turnedTo is the value a switch takes when the person changes it from
// its default: off for one that defaults to true, and on for any other,
// a missing or unreadable default included.
func (o bundleOption) turnedTo() bool {
	on, _ := o.defaultValue().(bool)
	return !on
}

// turnedNames is keys as a set gives them, each with the value turnedTo
// gives it: [enable_destructive on].
func turnedNames(m bundleManifest, keys []string) string {
	named := make([]string, len(keys))
	for i, key := range keys {
		named[i] = key + " off"
		if m.UserConfig[key].turnedTo() {
			named[i] = key + " on"
		}
	}
	return "[" + strings.Join(named, ", ") + "]"
}

// refusalProblems starts every set of the bundle's switches over the
// defaults, each switch in it turned from its default, and holds the
// ones the server refuses to declared: a refusal it does not declare,
// which no description then warns of, and a declared one the server
// starts with, each reported for the smallest set that shows it.
// switches is the user_config key of every switch the bundle asks for,
// sorted. A declared key that is not one of them is refused too, and so
// is one with no title, which the others' descriptions would name, each
// once. When the defaults alone do not start, which defaultsErr says, it
// starts no set: the defaults check reports that, and every set would
// fail with it.
func refusalProblems(m bundleManifest, switches []string, declared [][]string, defaultsErr error) []string {
	problems := declaredProblems(m, switches, declared)
	if defaultsErr != nil {
		return problems
	}
	return append(problems, startProblems(m, switches, declared)...)
}

// declaredProblems holds each key in a declared set to a switch with a
// title, each once, and each switch's description to the titles of the
// others in its sets, trimmed.
func declaredProblems(m bundleManifest, switches []string, declared [][]string) []string {
	title := func(key string) string { return strings.TrimSpace(m.UserConfig[key].Title) }
	var problems []string
	inSets := map[string]bool{}
	for _, set := range declared {
		for _, key := range set {
			inSets[key] = true
		}
	}
	titled := map[string]bool{}
	for _, key := range slices.Sorted(maps.Keys(inSets)) {
		switch {
		case !slices.Contains(switches, key):
			problems = append(problems, fmt.Sprintf("bundleRefused names %s, which is not a switch the bundle asks for", key))
		case title(key) == "":
			problems = append(problems, fmt.Sprintf("user_config.%s has no title, which the fields the server "+
				"refuses it with must name", key))
		default:
			titled[key] = true
		}
	}
	for _, set := range declared {
		for _, key := range set {
			if !slices.Contains(switches, key) {
				continue
			}
			description := m.UserConfig[key].Description
			for _, other := range set {
				if other != key && titled[other] && !strings.Contains(description, title(other)) {
					problems = append(problems, fmt.Sprintf("user_config.%s does not name %q, which the server "+
						"refuses it with", key, title(other)))
				}
			}
		}
	}
	return problems
}

// startProblems starts every set of the switches over the defaults,
// each switch in the set turned from its default, and holds the
// server's refusals to declared, both ways. Every subset of a set comes
// before it in this order, so the first set to show a defect is the
// smallest, and a superset is not reported again.
func startProblems(m bundleManifest, switches []string, declared [][]string) []string {
	var problems []string
	var undeclared []int
	startsDeclared := map[int]bool{}
	for bits := 1; bits < 1<<len(switches); bits++ {
		var turned []string
		for i, key := range switches {
			if bits&(1<<i) != 0 {
				turned = append(turned, key)
			}
		}
		env, _ := hostEnv(m, turned...)
		_, err := loadEnv(env)
		var within []int
		for i, set := range declared {
			if !slices.ContainsFunc(set, func(key string) bool { return !slices.Contains(turned, key) }) {
				within = append(within, i)
			}
		}
		switch {
		case err != nil && len(within) == 0:
			if slices.ContainsFunc(undeclared, func(shown int) bool { return shown&bits == shown }) {
				continue
			}
			undeclared = append(undeclared, bits)
			problems = append(problems, fmt.Sprintf("the server refuses %s, and bundleRefused does not "+
				"declare it, so no field's description warns of it: %v", turnedNames(m, turned), err))
		case err == nil:
			for _, i := range within {
				if !startsDeclared[i] {
					startsDeclared[i] = true
					problems = append(problems, fmt.Sprintf("bundleRefused declares that the server refuses %s, "+
						"and it starts with them", turnedNames(m, declared[i])))
				}
			}
		}
	}
	return problems
}

// topLevelKey matches a goreleaser top-level block header.
var topLevelKey = regexp.MustCompile(`(?m)^([a-z_]+):`)

// goreleaserBlocks splits .goreleaser.yaml, as goreleaserText read it,
// into its top-level blocks.
//
// Regular expressions rather than a YAML parser, for the reason
// goreleaserMatrix gives: a parser is a dependency this program does not
// otherwise need, and the file it reads is one this repository writes.
// The floor below is what keeps a changed layout from reading as an
// empty set of blocks, which would pass every check that follows.
func goreleaserBlocks(text string) (map[string]string, error) {
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

// releaseProblems holds .goreleaser.yaml under root, read once, to the
// bundle: it builds every binary the table stages and the family's
// archives, the bundle reaches every one of them, and the lines that
// make the bundle exist, be hashed, be signed and be published are there.
func releaseProblems(root string) []string {
	text, err := goreleaserText(root)
	if err != nil {
		return []string{err.Error()}
	}
	built, err := goreleaserMatrix(text)
	if err != nil {
		return []string{err.Error()}
	}
	blocks, err := goreleaserBlocks(text)
	if err != nil {
		return []string{err.Error()}
	}
	problems, _ := mcpbReleaseWiring(blocks)
	problems = append(problems, releaseArchiveProblems(built)...)
	problems = append(problems, mcpbMatrixProblems(mcpbBinaries)...)

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
	return problems
}

// releaseArchiveProblems holds the build matrix to the family's
// archives, releaseMatrix, both ways. A pair left out is a machine whose
// owner has nothing to download; a bundle reaching it under emulation
// does not help someone who installs from an archive. A pair beyond it
// is an archive no sibling's release carries.
func releaseArchiveProblems(built []string) []string {
	family := releasePairs()
	var problems []string
	for _, pair := range family {
		if !slices.Contains(built, pair) {
			problems = append(problems, fmt.Sprintf(".goreleaser.yaml does not build %s, so the release carries "+
				"no %s archive; the family's releases carry %s", pair, strings.ReplaceAll(pair, "-", "_"),
				strings.Join(family, ", ")))
		}
	}
	for _, pair := range built {
		if !slices.Contains(family, pair) {
			problems = append(problems, fmt.Sprintf(".goreleaser.yaml builds %s, which is not one of the "+
				"family's archives, %s", pair, strings.Join(family, ", ")))
		}
	}
	return problems
}

// mcpbMatrixProblems holds the Desktop bundle's binaries to
// releaseMatrix: every pair the release builds runs a binary the bundle
// stages, its own, the universal one on macOS, or the one releaseMatrix
// says it runs under emulation. The plugin gate reads the same table, so
// the two bundles cannot disagree about a platform.
func mcpbMatrixProblems(binaries []mcpbBinary) []string {
	staged := func(pair string) bool {
		goos, _, _ := strings.Cut(pair, "-")
		return slices.ContainsFunc(binaries, func(b mcpbBinary) bool {
			return b.goarch == pair || (b.universal && strings.HasPrefix(b.goarch, goos+"-"))
		})
	}
	var problems []string
	for _, p := range releaseMatrix {
		if !staged(p.pair) && (p.runs == "" || !staged(p.runs)) {
			problems = append(problems, fmt.Sprintf("the release builds %s and the Desktop bundle stages no "+
				"binary that runs there", p.pair))
		}
	}
	return problems
}

// mcpbReleaseWiring holds the lines that make the bundle exist, be
// hashed, be signed and be published.
//
// Nothing else here can see them, and that is the point. Delete
// `checksum.extra_files` and the release still succeeds, `make check`
// still passes, the checks above still report a valid manifest — and
// the bundle ships absent from checksums.txt and therefore outside the
// signature, while README's "Verifying a download" goes on telling
// people it is covered. The standard says both `checksum.extra_files`
// and `release.extra_files`, "or the bundle ships unsigned, or is hashed
// and never published"; this is what holds that sentence, and seven more
// beside it.
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

	// The gate's own start, read once: a bundle cannot be packed past a
	// rule the gate enforces.
	b, problems, err := committedManifest(root)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("the manifest does not describe the bundle this would pack:\n%s",
			strings.Join(problems, "\n"))
	}
	manifest, err := stampedManifest(b.raw, version)
	if err != nil {
		return err
	}

	files, binaries, err := mcpbLayout(root, dist, b.launcher)
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

// stampedManifest is the committed manifest raw with version written in,
// as the bundle carries it, held to the vendored schema again: what
// ships is checked, not only what is committed.
func stampedManifest(raw []byte, version string) ([]byte, error) {
	stamped, err := stampVersion(raw, version, mcpbManifestPath)
	if err != nil {
		return nil, err
	}
	if err := validateDocument(mcpbSchemaFile, "the stamped manifest", stamped); err != nil {
		return nil, err
	}
	return stamped, nil
}

// mcpbLayout is everything the bundle carries beside the manifest, the
// launcher committedManifest generated included, ordered by the name
// each file takes inside it — so the archive's order is a property of
// the bundle rather than of how the table happens to be arranged.
func mcpbLayout(root, dist, launcher string) ([]bundleFile, map[string]string, error) {
	files := []bundleFile{{name: mcpbLauncher, body: []byte(launcher)}}
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

// bundleManifest is the part of the manifest these gates read.
type bundleManifest struct {
	Schema          string   `json:"$schema"`
	ManifestVersion string   `json:"manifest_version"`
	Support         string   `json:"support"`
	Name            string   `json:"name"`
	Version         string   `json:"version"`
	License         string   `json:"license"`
	Description     string   `json:"description"`
	PrivacyPolicies []string `json:"privacy_policies"`
	Server          struct {
		Type       string `json:"type"`
		EntryPoint string `json:"entry_point"`
		MCPConfig  struct {
			Command string `json:"command"`
			// Args is raw, so that a key set to null is told apart from
			// one that is missing.
			Args              json.RawMessage   `json:"args"`
			Env               map[string]string `json:"env"`
			PlatformOverrides map[string]struct {
				Command string            `json:"command"`
				Args    []string          `json:"args"`
				Env     map[string]string `json:"env"`
			} `json:"platform_overrides"`
		} `json:"mcp_config"`
	} `json:"server"`
	UserConfig    map[string]bundleOption `json:"user_config"`
	Compatibility struct {
		ClaudeDesktop string   `json:"claude_desktop"`
		Platforms     []string `json:"platforms"`
	} `json:"compatibility"`
}

// bundleOption is one user_config entry. Its default stays JSON, so a
// missing one is told apart from any value.
type bundleOption struct {
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Required    bool            `json:"required"`
	Sensitive   bool            `json:"sensitive"`
	Multiple    bool            `json:"multiple"`
	Default     json.RawMessage `json:"default"`
}

// defaultValue is the default decoded, or nil when there is none or it
// does not decode.
func (o bundleOption) defaultValue() any {
	var v any
	if len(o.Default) == 0 || json.Unmarshal(o.Default, &v) != nil {
		return nil
	}
	return v
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

// args is mcp_config.args as strings: none when the key is missing, null
// or not a list of strings. settingProblems reports the first two, and
// the schema the third.
func (m bundleManifest) args() []string {
	var args []string
	if json.Unmarshal(m.Server.MCPConfig.Args, &args) != nil {
		return nil
	}
	return args
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
	collect(m.args()...)
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

// upstreamSchema matches the host and path the manifest schema is
// published at, capturing the ref it is served from and the format
// version in its filename.
//
// An allow-list over the whole URL. Refusing the branch NAMES `main`,
// `master` and `HEAD` is the obvious version, and it passes a branch
// called anything else, a partial tag like `v2.1` that upstream
// re-points as it releases, and the right filename served by somebody
// who is not upstream.
var upstreamSchema = regexp.MustCompile(
	`^https://raw\.githubusercontent\.com/anthropics/mcpb/([^/]+)/schemas/mcpb-manifest-v(\d+\.\d+)\.schema\.json$`)

// immutableRef is a ref that cannot be moved under the document: a full
// release tag, or a commit SHA. A partial tag is a branch with a version
// number in it.
var immutableRef = regexp.MustCompile(`^(v[0-9]+\.[0-9]+\.[0-9]+|[0-9a-f]{40})$`)

// minManifestVersion is the format version this repository has checked
// against the published schemas.
//
// Checked 2026-09-17: v0.2, v0.3 and v0.4 are served and v0.5 is not,
// and 0.4's only difference from 0.3 is a `uv` value in the
// `server.type` enum — which a `binary` server gains nothing from. Raise
// it after checking what a newer format changes AND that a desktop
// installs a bundle declaring it, not because another repository moved.
// mcpbSchemaFile moves with it.
const minManifestVersion = "0.3"

// declarationProblems holds what the manifest claims about itself: which
// format version it is, which document defines that version, where a
// failing install is reported, and which Claude Desktop it needs.
//
// The rest of checkBundleManifest holds the manifest against the staged
// tree, which is the half a schema cannot do. This half is not the
// schema's either — a schema cannot say whether the reference to it is
// pinned to bytes or to a branch, and it cannot say whether the version
// is one anybody checked. The floor is the claim the others structurally
// cannot make: they hold the document against ITSELF, and 0.2 beside a
// 0.2 schema is stale and entirely self-consistent.
func (m bundleManifest) declarationProblems() []string {
	var problems []string

	switch {
	case m.Schema == "":
		problems = append(problems,
			"the manifest has no $schema, so nothing says which version of the format it is")
	case !upstreamSchema.MatchString(m.Schema):
		problems = append(problems, fmt.Sprintf(
			"$schema is %q, which is not upstream's published schemas/mcpb-manifest-v<version>.schema.json "+
				"path; a schema fetched from anywhere else is not the document this format is defined by",
			m.Schema))
	default:
		parts := upstreamSchema.FindStringSubmatch(m.Schema)
		if !immutableRef.MatchString(parts[1]) {
			problems = append(problems, fmt.Sprintf(
				"$schema is served from %q, which can be re-pointed — a branch, or a partial tag. The path "+
					"pins the format and the ref pins the bytes, so an amendment upstream changes what "+
					"this document validates against. Name a full tag or a commit SHA", parts[1]))
		}
		if parts[2] != m.ManifestVersion {
			problems = append(problems, fmt.Sprintf(
				"manifest_version is %q and $schema pins v%s; a document cannot claim one version and "+
					"validate against another", m.ManifestVersion, parts[2]))
		}
	}

	if olderThan(m.ManifestVersion, minManifestVersion) {
		problems = append(problems, fmt.Sprintf(
			"manifest_version is %q and this repository has checked %s; a manifest that agrees with its "+
				"own $schema is still a manifest a version behind", m.ManifestVersion, minManifestVersion))
	}
	if m.Support == "" {
		problems = append(problems, "the manifest has no support URL, so a bundle that fails on "+
			"somebody's desktop does not say where to report it")
	}
	if m.Compatibility.ClaudeDesktop == "" {
		problems = append(problems, "compatibility.claude_desktop is empty, so the bundle names no Claude "+
			"Desktop it was made for; the family claims >=0.10.0")
	}
	return problems
}

// olderThan compares major.minor numerically. As text "0.10" sorts
// before "0.3", which stays right until the tenth minor version.
func olderThan(version, floor string) bool {
	major, minor, ok := majorMinor(version)
	floorMajor, floorMinor, floorOK := majorMinor(floor)
	if !ok || !floorOK {
		return !ok
	}
	if major != floorMajor {
		return major < floorMajor
	}
	return minor < floorMinor
}

func majorMinor(v string) (int, int, bool) {
	before, after, found := strings.Cut(v, ".")
	if !found {
		return 0, 0, false
	}
	major, err := strconv.Atoi(before)
	if err != nil {
		return 0, 0, false
	}
	minor, err := strconv.Atoi(after)
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}
