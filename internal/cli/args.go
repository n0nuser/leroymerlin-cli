package cli

import (
	"strconv"

	"github.com/spf13/cobra"

	"github.com/n0nuser/leroymerlin-cli/internal/output"
)

// Positional argument checks. cobra's own helpers return plain errors, which
// would exit 1; docs/cli-spec.md promises exit 2 for a bad argument, so these
// wrap the same checks in a usage error.

func anyArgs(*cobra.Command, []string) error { return nil }

func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return output.Usagef("%s takes no arguments", cmd.CommandPath())
	}
	return nil
}

func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return output.Usagef("%s wants %s, got %d", cmd.CommandPath(), count(n), len(args))
		}
		return nil
	}
}

func maxArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > n {
			return output.Usagef("%s takes at most %s, got %d", cmd.CommandPath(), count(n), len(args))
		}
		return nil
	}
}

func count(n int) string {
	if n == 1 {
		return "1 argument"
	}
	return strconv.Itoa(n) + " arguments"
}
