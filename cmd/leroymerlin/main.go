// Command leroymerlin is an unofficial, read-only terminal client for leroymerlin.es.
// It lives under cmd/ so that `go install .../cmd/leroymerlin` names the binary leroymerlin.
package main

import (
	"os"

	"github.com/n0nuser/leroymerlin-cli/internal/cli"
)

// version is set by goreleaser through -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.Execute(version, os.Args[1:]))
}
