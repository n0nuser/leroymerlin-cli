// Command leroymerlin is an unofficial, read-only terminal client for
// leroymerlin.es. See docs/cli-spec.md. It lives under cmd/ so that
// `go install .../cmd/leroymerlin` names the binary leroymerlin.
package main

import (
	"os"
	"runtime/debug"
	"strings"

	"github.com/n0nuser/leroymerlin-cli/internal/cli"
)

// version is stamped by goreleaser: -ldflags "-X main.version=...".
var version string

func main() {
	os.Exit(cli.Execute(buildVersion(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// buildVersion falls back to the module version Go records for
// `go install ...@vX.Y.Z`, then to "dev".
func buildVersion() string {
	if version != "" {
		return strings.TrimPrefix(version, "v")
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return "dev"
}
