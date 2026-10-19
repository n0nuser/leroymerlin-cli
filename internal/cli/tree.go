package cli

import "github.com/spf13/cobra"

// CommandTree returns the wired command tree without running it, so the docs
// generator can render the reference from the binary itself.
func CommandTree(version string) *cobra.Command {
	return (&App{Version: version}).rootCmd()
}
