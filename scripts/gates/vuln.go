package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// vuln runs govulncheck under the exact Go release go.mod names, and
// refuses to run it under any other.
//
// govulncheck checks the standard library of the go command it runs
// under. A distribution build reports a version such as
// "go1.27.3-X:nodwarf5", which govulncheck cannot read, so it skips the
// standard library and reports clean. Its reference does not say so
// (https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck). Setting
// GOTOOLCHAIN to the release makes the go command fetch the official
// build, and GOVERSION is then checked to be exactly that release.
// https://go.dev/doc/toolchain#GOTOOLCHAIN
func vuln(w io.Writer, args []string) error {
	if len(args) != 1 || pinsModuleAt.FindString(args[0]) != args[0] {
		return couldNotRun{fmt.Errorf("usage: gates vuln MODULE@VERSION, govulncheck as the Makefile pins it")}
	}
	gomod, err := os.ReadFile("go.mod")
	if err != nil {
		return couldNotRun{err}
	}
	toolchain, err := vulnToolchain(string(gomod))
	if err != nil {
		return err
	}
	env := append(os.Environ(), "GOTOOLCHAIN="+toolchain)
	version := exec.Command("go", "env", "GOVERSION")
	// The first run may download the release, so its progress and any
	// error reach the person.
	version.Env, version.Stderr = env, os.Stderr
	out, err := version.Output()
	if err != nil {
		return couldNotRun{fmt.Errorf("go env GOVERSION under GOTOOLCHAIN=%s: %w", toolchain, err)}
	}
	// A suffix after the release, such as -X:nodwarf5, is a build other
	// than the official one.
	if got := strings.TrimSpace(string(out)); got != toolchain {
		return fmt.Errorf("GOTOOLCHAIN=%s ran %s, not the official %s; govulncheck would skip the standard library",
			toolchain, got, toolchain)
	}
	// Installed and run rather than `go run`, which exits 1 whatever the
	// program's status: govulncheck exits 3 when it finds a vulnerability.
	bin, err := os.MkdirTemp("", "gates-vuln-")
	if err != nil {
		return couldNotRun{err}
	}
	defer func() { _ = os.RemoveAll(bin) }()
	install := exec.Command("go", "install", args[0])
	install.Env, install.Stdout, install.Stderr = slices.Concat(env, []string{"GOBIN=" + bin}), w, os.Stderr
	if err := install.Run(); err != nil {
		return couldNotRun{fmt.Errorf("go install %s under %s: %w", args[0], toolchain, err)}
	}
	scan := exec.Command(filepath.Join(bin, "govulncheck"+exeSuffix()), "./...")
	scan.Env, scan.Stdout, scan.Stderr = env, w, os.Stderr
	var exit *exec.ExitError
	switch err := scan.Run(); {
	case errors.As(err, &exit) && exit.ExitCode() == vulnFound:
		return fmt.Errorf("govulncheck under %s found a vulnerability the code reaches", toolchain)
	case err != nil:
		return couldNotRun{fmt.Errorf("govulncheck under %s: %w", toolchain, err)}
	}
	_, err = fmt.Fprintf(w, "scanned under %s, the official release go.mod names\n", toolchain)
	return err
}

// vulnFound is govulncheck's exit status for a vulnerability the code
// reaches. Its reference says only "unsuccessfully"; the code is v1.8.0's
// (https://go.googlesource.com/vuln/+/refs/tags/v1.8.0/internal/scan/errors.go#17).
const vulnFound = 3

// vulnRelease is a release's toolchain name: go1.N.P. A go line without
// the patch, go1.N, names a language version, not a toolchain to fetch.
var vulnRelease = regexp.MustCompile(`^go1\.\d+\.\d+$`)

// vulnToolchain is the toolchain go.mod builds with, as GOTOOLCHAIN names
// it.
func vulnToolchain(gomod string) (string, error) {
	v := pinsGoVersion(gomod)
	if v == "" {
		return "", couldNotRun{fmt.Errorf("go.mod has no go line")}
	}
	name := "go" + v
	if !vulnRelease.MatchString(name) {
		return "", fmt.Errorf("go.mod names %s, not a release such as go1.27.4; govulncheck needs one to fetch", name)
	}
	return name, nil
}
