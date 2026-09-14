package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// The mechanics both bundles share.
//
// This repository ships two: a `.plugin` for Claude Code and Cowork, and
// a `.mcpb` for Claude Desktop. Their manifests are different documents
// with different schemas and different platform tables, and those stay
// apart — forcing one checker over both would be worse code than two.
//
// What is identical is the mechanism: write a deflate zip with a fixed
// timestamp and a mode per entry, stamp a real version into a manifest
// that must be carrying the placeholder, and ask a staged binary whether
// it agrees. Those were written twice, and in the same commit they had
// already drifted — one packer substituted over bytes and the other
// re-encoded a decoded map, which is not a neutral act. They live here
// so there is one answer.

// bundleFile is one entry in a bundle: what it is called inside the
// archive, and where its bytes come from. Exactly one of from and body
// is set — body for generated content, from for a file on disk, which is
// streamed rather than read whole.
type bundleFile struct {
	name string
	from string
	body []byte
}

// zipModTime is a fixed timestamp for every entry. Identical inputs
// should give a byte-identical archive, and leaving it unset writes
// zeroes that display as the impossible 1980-00-00.
var zipModTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// writeZipAt writes entries as a deflate zip, in the order given.
//
// Through a temporary file and a rename, because the next thing a
// release does is checksum whatever is in dist/, and a half-written
// bundle would be hashed and signed as readily as a whole one.
func writeZipAt(out string, entries []bundleFile, mode func(string) fs.FileMode) error {
	tmp := out + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()

	if err := writeZipEntries(f, entries, mode); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, out)
}

func writeZipEntries(w io.Writer, entries []bundleFile, mode func(string) fs.FileMode) error {
	zw := zip.NewWriter(w)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate, Modified: zipModTime}
		header.SetMode(mode(entry.name))
		dst, err := zw.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("%s: %w", entry.name, err)
		}
		if entry.body != nil {
			if _, err := dst.Write(entry.body); err != nil {
				return fmt.Errorf("%s: %w", entry.name, err)
			}
			continue
		}
		src, err := os.Open(entry.from)
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, src)
		_ = src.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", entry.name, err)
		}
	}
	return zw.Close()
}

// stampVersion returns a manifest with a real version in it.
//
// Decoded, edited as text, then decoded again. The first decode is what
// makes this more than a text substitution: the file has to be JSON, and
// it has to be carrying the placeholder, which is what a manifest
// somebody edited by hand is not. The second proves the edit left JSON
// behind carrying the version asked for.
//
// The edit itself is on the bytes rather than on the decoded value,
// because re-encoding a map is not a neutral act. Go sorts map keys, so
// the shipped manifest would come out alphabetised and unreviewable
// against the source, and its encoder escapes `<`, `>` and `&`. Both are
// legal JSON and neither is what anyone wrote.
func stampVersion(raw []byte, version, path string) ([]byte, error) {
	semver := strings.TrimPrefix(version, "v")
	if semver == "" {
		return nil, fmt.Errorf("%s: empty version", path)
	}
	if semver == placeholderVersion {
		return nil, fmt.Errorf("%s: refusing to stamp the placeholder %q as a release version",
			path, placeholderVersion)
	}

	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if manifest.Version != placeholderVersion {
		return nil, fmt.Errorf("%s carries version %q, expected the placeholder %q; refusing to pack a "+
			"manifest that was edited by hand", path, manifest.Version, placeholderVersion)
	}

	// Exactly one, or the edit is ambiguous and the wrong string could
	// be the one replaced.
	quoted := []byte(`"` + placeholderVersion + `"`)
	if n := bytes.Count(raw, quoted); n != 1 {
		return nil, fmt.Errorf("%s spells %s %d times; the substitution needs exactly one", path, quoted, n)
	}
	out := bytes.Replace(raw, quoted, []byte(`"`+semver+`"`), 1)

	var check struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out, &check); err != nil {
		return nil, fmt.Errorf("%s: the substitution did not leave valid JSON: %w", path, err)
	}
	if check.Version != semver {
		return nil, fmt.Errorf("%s: the substitution set version to %q, wanted %q", path, check.Version, semver)
	}
	// The manifest is a text file, and the source ends with a newline.
	if !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, '\n')
	}
	return out, nil
}

// versionsAgree asks the staged binary for the host platform what
// version it thinks it is, and requires it to match the one being
// stamped into the manifest.
//
// A bundle claims a version in five places — its filename, the archive
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
