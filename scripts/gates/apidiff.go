package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The Favro API reference, and where the snapshot of it lives.
//
// There is no discovery document — §17b's second deviation — so the
// surface is scraped from the published reference. That makes the
// parsing rules below load-bearing, and it is why every one of them
// asserts a floor: a page restructure has to fail this command loudly
// rather than quietly produce a shorter list that then "matches" a
// shorter set of verdicts.
const (
	apiReferenceURL = "https://favro.com/developer/"
	apiSurfacePath  = "testdata/api-surface.json"
	apiCoveragePath = "testdata/api-coverage.tsv"
	apiFetchTimeout = 60 * time.Second
	apiMinEndpoints = 80 // 88 at the last fetch; a page that lost a tenth of them is a parse failure
	apiMinSections  = 10
	apiMinResources = 12        // 15 at the last fetch
	apiMinPageBytes = 200 << 10 // the page was 492 KB
)

// endpointPattern matches the one shape the reference uses to state an
// endpoint: an HTTP Request heading followed by a paragraph holding a
// method and an absolute URL.
var endpointPattern = regexp.MustCompile(
	`<p><code>(GET|POST|PUT|PATCH|DELETE) (https://favro\.com/api/[^<]+)</code></p>`)

// sectionPattern matches the top-level headings, which name the
// resource a run of endpoints belongs to.
var sectionPattern = regexp.MustCompile(`<h1 id="([a-z0-9-]+)">([^<]+)</h1>`)

// fieldTablePattern matches a resource's field table — the first table
// in a section whose header is Field / Type / Description. Later tables
// in the same section describe nested objects and request parameters,
// which are not the resource.
var fieldTablePattern = regexp.MustCompile(
	`(?s)<thead>\s*<tr>\s*<th>Field</th>\s*<th>Type</th>.*?</table>`)

// fieldRowPattern matches the field name in one row of such a table.
var fieldRowPattern = regexp.MustCompile(`<tr>\s*<td>([A-Za-z0-9_]+)</td>`)

// apiEndpoint is one documented operation.
type apiEndpoint struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Section string `json:"section"`
}

// apiResource is one documented resource's field list — the table the
// reference prints above its endpoints.
type apiResource struct {
	Section string   `json:"section"`
	Fields  []string `json:"fields"`
}

// apiSurface is the committed snapshot.
type apiSurface struct {
	Source    string        `json:"source"`
	FetchedAt string        `json:"fetched_at"`
	Endpoints []apiEndpoint `json:"endpoints"`
	Resources []apiResource `json:"resources"`
}

// apiDiff fetches the reference and rewrites the committed snapshot.
//
// Not a gate: it reaches the network, so it runs when a maintainer asks
// and never in `make check`. What runs in check is `api-coverage`,
// offline, against whatever this last wrote.
//
// On any failure it writes nothing. A refresh that half-writes is worse
// than one nobody runs, because the next check then holds the verdicts
// to a snapshot that is neither the old truth nor the new one.
func apiDiff(w io.Writer, _ []string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}

	page, err := fetchReference()
	if err != nil {
		return err
	}

	endpoints, sections, err := parseReference(page)
	if err != nil {
		return err
	}
	resources, err := parseResources(page)
	if err != nil {
		return err
	}

	surface := apiSurface{
		Source:    apiReferenceURL,
		FetchedAt: time.Now().UTC().Format("2006-01-02"),
		Endpoints: endpoints,
		Resources: resources,
	}
	body, err := json.MarshalIndent(surface, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(root, filepath.FromSlash(apiSurfacePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return err
	}

	_, err = fmt.Fprintf(w,
		"api-diff: %d endpoints across %d sections and %d resource field lists written to %s; run `gates api-coverage` and `gates api-fields`\n",
		len(endpoints), sections, len(resources), apiSurfacePath)
	return err
}

// fetchReference downloads the reference page.
func fetchReference() ([]byte, error) {
	client := &http.Client{Timeout: apiFetchTimeout}
	resp, err := client.Get(apiReferenceURL) //nolint:noctx // a maintainer command with its own timeout
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", apiReferenceURL, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", apiReferenceURL, resp.StatusCode)
	}
	page, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", apiReferenceURL, err)
	}
	if len(page) < apiMinPageBytes {
		return nil, fmt.Errorf("%s returned %d bytes, expected at least %d — that is not the reference page",
			apiReferenceURL, len(page), apiMinPageBytes)
	}
	return page, nil
}

// parseResources extracts each section's field table.
func parseResources(page []byte) ([]apiResource, error) {
	text := string(page)
	bounds := sectionPattern.FindAllStringSubmatchIndex(text, -1)

	var out []apiResource
	for i, m := range bounds {
		end := len(text)
		if i+1 < len(bounds) {
			end = bounds[i+1][0]
		}
		body := text[m[0]:end]
		table := fieldTablePattern.FindString(body)
		if table == "" {
			continue // prose sections, and the ones documented elsewhere
		}
		var fields []string
		for _, row := range fieldRowPattern.FindAllStringSubmatch(table, -1) {
			fields = append(fields, row[1])
		}
		if len(fields) == 0 {
			continue
		}
		out = append(out, apiResource{Section: text[m[4]:m[5]], Fields: fields})
	}

	if len(out) < apiMinResources {
		return nil, fmt.Errorf("found %d resource field tables, expected at least %d — the page's shape changed and this parser needs rewriting",
			len(out), apiMinResources)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Section < out[j].Section })
	return out, nil
}

// parseReference extracts the endpoints and the section each sits in.
func parseReference(page []byte) ([]apiEndpoint, int, error) {
	text := string(page)

	// Section headings, by the offset they start at, so each endpoint
	// can be attributed to the last one before it.
	type marker struct {
		at   int
		name string
	}
	var markers []marker
	for _, m := range sectionPattern.FindAllStringSubmatchIndex(text, -1) {
		markers = append(markers, marker{at: m[0], name: text[m[4]:m[5]]})
	}
	if len(markers) < apiMinSections {
		return nil, 0, fmt.Errorf("found %d section headings in the reference, expected at least %d — the page's shape changed and this parser needs rewriting",
			len(markers), apiMinSections)
	}

	seen := map[string]bool{}
	var out []apiEndpoint
	for _, m := range endpointPattern.FindAllStringSubmatchIndex(text, -1) {
		method := text[m[2]:m[3]]
		rawURL := text[m[4]:m[5]]
		path := strings.TrimPrefix(rawURL, "https://favro.com")

		section := "unknown"
		for _, mk := range markers {
			if mk.at > m[0] {
				break
			}
			section = mk.name
		}

		key := method + " " + path
		if seen[key] {
			continue // the reference repeats an endpoint in its examples
		}
		seen[key] = true
		out = append(out, apiEndpoint{Method: method, Path: path, Section: section})
	}

	if len(out) < apiMinEndpoints {
		return nil, 0, fmt.Errorf("found %d endpoints in the reference, expected at least %d — the page's shape changed and this parser needs rewriting",
			len(out), apiMinEndpoints)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out, len(markers), nil
}
