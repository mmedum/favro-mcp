package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// The MCP registry entry. Without one, the only way to find this server
// is to already know the repository exists.
//
// In mcpregistry.go rather than registry.go: registry_test.go already
// belongs to this binary's own COMMAND registry, and two different
// things called registry in one package is how a test file gets
// overwritten.
//
// The rules below are read from the registry's own validator rather than
// from its schema, because the schema does not carry them: the
// identifier must be HTTPS, must be a GitHub release asset URL, must
// contain "mcp" somewhere, must not sit beside a registryBaseUrl, and
// the bundle's hash must be 64 hex characters. Every one of those is
// refused at publish time — after the login has succeeded — which is the
// worst moment to find out.
//
// Adopted from google-drive-mcp, the one sibling that publishes to the
// registry. The committed entry carries a placeholder version and a
// placeholder hash on purpose: a real version in the tree is a stale
// version the moment the next release goes out, and `registry-publish`
// is what fills both in from the signed checksums at tag time.
const (
	registryFile        = "packaging/registry/server.json"
	registryPlaceholder = "0.0.0-dev"
	registryServerName  = "io.github.mmedum/favro-mcp"
	registryReleaseHost = "github.com"
	registryBundleOwner = "mmedum/favro-mcp"
)

// registryNamePattern is the schema's own pattern for a server name.
var registryNamePattern = regexp.MustCompile(`^[a-zA-Z0-9.\-]+/[a-zA-Z0-9.\-_]+$`)

type registryEntry struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Packages    []struct {
		RegistryType    string `json:"registryType"`
		Identifier      string `json:"identifier"`
		FileSha256      string `json:"fileSha256"`
		RegistryBaseURL string `json:"registryBaseUrl"`
	} `json:"packages"`
}

// registry checks the committed entry against the rules the registry
// enforces in code.
func registry(out io.Writer, _ []string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(root + "/" + registryFile)
	if err != nil {
		return fmt.Errorf("%w (the registry entry is %s)", err, registryFile)
	}
	entry, problems := checkRegistryEntry(raw, registryPlaceholder)
	if len(problems) > 0 {
		for _, p := range problems {
			_, _ = fmt.Fprintln(out, "  "+p)
		}
		return fmt.Errorf("%d problem(s) in %s", len(problems), registryFile)
	}
	_, err = fmt.Fprintf(out, "registry entry ok: %s, %d package, placeholder version %q\n",
		entry.Name, len(entry.Packages), entry.Version)
	return err
}

func checkRegistryEntry(raw []byte, wantVersion string) (registryEntry, []string) {
	var entry registryEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return entry, []string{"decode " + registryFile + ": " + err.Error()}
	}
	var problems []string
	if entry.Name != registryServerName {
		problems = append(problems, fmt.Sprintf(
			"name is %q, want %q — the io.github. namespace is what GitHub OIDC proves at publish time",
			entry.Name, registryServerName))
	}
	if entry.Version != wantVersion {
		problems = append(problems, fmt.Sprintf("version is %q, want %q", entry.Version, wantVersion))
	}
	problems = append(problems, checkRegistryLengths(entry)...)
	if len(entry.Packages) != 1 {
		problems = append(problems, fmt.Sprintf(
			"%d packages; this server publishes exactly one, the .mcpb bundle", len(entry.Packages)))
		return entry, problems
	}

	pkg := entry.Packages[0]
	if pkg.RegistryType != "mcpb" {
		problems = append(problems, fmt.Sprintf("registryType is %q, want \"mcpb\"", pkg.RegistryType))
	}
	// Every one of these is refused by the registry's validator, and
	// none of them by its schema.
	if pkg.RegistryBaseURL != "" {
		problems = append(problems, "registryBaseUrl is set, and an MCPB package must not have one: "+
			"the full download URL goes in identifier")
	}
	// Subsumed in practice by the .mcpb suffix check below — any name
	// ending in .mcpb contains "mcp" — and kept anyway, because both are
	// rules the registry applies and a reader should not have to work
	// out that one cannot fail without the other.
	if !strings.Contains(strings.ToLower(pkg.Identifier), "mcp") {
		problems = append(problems, "the identifier must contain \"mcp\" somewhere: "+pkg.Identifier)
	}
	if len(pkg.FileSha256) != 64 || strings.Trim(pkg.FileSha256, "0123456789abcdef") != "" {
		problems = append(problems, "fileSha256 is not 64 hex characters: "+pkg.FileSha256)
	}
	problems = append(problems, checkRegistryIdentifier(pkg.Identifier)...)
	return entry, problems
}

func checkRegistryIdentifier(identifier string) []string {
	parsed, err := url.Parse(identifier)
	if err != nil {
		return []string{"identifier is not a URL: " + err.Error()}
	}
	var problems []string
	if parsed.Scheme != "https" {
		problems = append(problems, "identifier must use https: "+identifier)
	}
	if strings.ToLower(parsed.Host) != registryReleaseHost {
		problems = append(problems, fmt.Sprintf("identifier host is %q, want %q", parsed.Host, registryReleaseHost))
	}
	// The shape the registry's release-URL check insists on.
	if !strings.Contains(parsed.Path, "/releases/download/") {
		problems = append(problems, "identifier is not a release asset URL (no /releases/download/): "+identifier)
	}
	if !strings.HasSuffix(parsed.Path, ".mcpb") {
		problems = append(problems, "identifier does not name a .mcpb: "+identifier)
	}
	return problems
}

func checkRegistryLengths(entry registryEntry) []string {
	var problems []string
	limit := func(field, value string, minLen, maxLen int) {
		switch {
		case len(value) < minLen:
			problems = append(problems, fmt.Sprintf("%s is %d characters, and the schema wants at least %d",
				field, len(value), minLen))
		case len(value) > maxLen:
			problems = append(problems, fmt.Sprintf(
				"%s is %d characters, and the schema allows at most %d. The registry refuses this at "+
					"publish time, after the login has succeeded", field, len(value), maxLen))
		}
	}
	limit("description", entry.Description, 1, 100)
	limit("title", entry.Title, 1, 100)
	limit("name", entry.Name, 3, 200)
	limit("version", entry.Version, 1, 255)
	if !registryNamePattern.MatchString(entry.Name) {
		problems = append(problems, "name does not match the schema's pattern: "+entry.Name)
	}
	return problems
}

// registryPublish prints the entry to publish for one version, with the
// bundle's hash taken from the signed checksums rather than recomputed.
//
// It runs after the release exists, because the identifier has to be a
// URL that answers — which is also why the committed entry can never
// carry a real one.
func registryPublish(out io.Writer, args []string) error {
	if len(args) == 0 || args[0] == "" {
		return fmt.Errorf("usage: gates registry-publish VERSION [DIST]")
	}
	version := strings.TrimPrefix(args[0], "v")
	dist := "dist"
	if len(args) > 1 && args[1] != "" {
		dist = args[1]
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(root + "/" + registryFile)
	if err != nil {
		return err
	}
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		return fmt.Errorf("decode %s: %w", registryFile, err)
	}
	if got, _ := entry["version"].(string); got != registryPlaceholder {
		return fmt.Errorf("%s carries version %q; the committed entry must carry %q, so that a version "+
			"in the tree can never be a stale one", registryFile, got, registryPlaceholder)
	}

	name := fmt.Sprintf("favro-mcp_%s.mcpb", version)
	sum, err := sumFromChecksums(dist+"/checksums.txt", name)
	if err != nil {
		return err
	}
	entry["version"] = version
	packages, _ := entry["packages"].([]any)
	if len(packages) != 1 {
		return fmt.Errorf("%s has %d packages; this server publishes exactly one", registryFile, len(packages))
	}
	pkg, _ := packages[0].(map[string]any)
	pkg["identifier"] = fmt.Sprintf("https://%s/%s/releases/download/v%s/%s",
		registryReleaseHost, registryBundleOwner, version, name)
	pkg["fileSha256"] = sum

	written, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	// The same rules the gate holds the committed entry to, applied to
	// what is about to be published. Printing something the registry
	// will refuse is the failure this whole file exists to prevent.
	if _, problems := checkRegistryEntry(written, version); len(problems) > 0 {
		for _, p := range problems {
			_, _ = fmt.Fprintln(out, "  "+p)
		}
		return fmt.Errorf("%d problem(s) in the entry to publish", len(problems))
	}
	_, err = fmt.Fprintln(out, string(written))
	return err
}

// sumFromChecksums reads one file's sha256 out of a goreleaser
// checksums.txt, which is the file the release signs.
func sumFromChecksums(path, name string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("%w — run this after the release has been built", err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return fields[0], nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s names no %s; the bundle was not built into this release", path, name)
}
