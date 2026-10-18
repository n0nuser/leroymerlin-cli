// Package cli wires cobra commands to the leroymerlin.es client and the output layer.
// WIP scaffold: only the root command exists so far.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Execute runs the CLI and returns the process exit code.
func Execute(version string, args []string) int {
	root := &cobra.Command{
		Use:           "leroymerlin",
		Short:         "Search leroymerlin.es and check per-store stock from the terminal (unofficial, read-only)",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}
	root.SetVersionTemplate("leroymerlin {{.Version}}\n")
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "leroymerlin:", err)
		return 1
	}
	return 0
}
