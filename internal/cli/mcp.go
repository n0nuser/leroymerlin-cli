package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/n0nuser/leroymerlin-cli/internal/mcp"
	"github.com/n0nuser/leroymerlin-cli/internal/output"
)

func (a *App) mcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the read commands to an agent over MCP (stdio)",
		Long: `Run a Model Context Protocol server on stdin/stdout. Each tool runs the
command it is named after and returns the same JSON. Everything is read-only.

Tools: ` + mcp.Describe() + `

Examples:
  leroymerlin mcp install claude-code
  leroymerlin mcp install codex --profile madrid
  leroymerlin mcp                 # normally started by the harness`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := &mcp.Server{Version: a.Version, Run: a.runCommand}
			return srv.Serve(cmd.Context(), a.Stdin, a.Stdout)
		},
	}
	install := &cobra.Command{
		Use:       "install " + strings.Join(mcp.Harnesses, "|"),
		Short:     "Register this binary in an agent harness's MCP config (idempotent)",
		Long:      "Add this binary as the leroymerlin MCP server in ~/.claude.json (claude-code), ~/.codex/config.toml (codex) or ~/.cursor/mcp.json (cursor). Other entries are kept; running it twice reports \"unchanged\".",
		Args:      exactArgs(1),
		ValidArgs: mcp.Harnesses,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !slices.Contains(mcp.Harnesses, args[0]) {
				return output.Usagef("unknown harness %q. Choose %s", args[0], strings.Join(mcp.Harnesses, ", "))
			}
			exe, err := os.Executable()
			if err != nil {
				exe = "leroymerlin"
			}
			serverArgs := []string{"mcp"}
			if a.Profile != "default" {
				serverArgs = append(serverArgs, "--profile", a.Profile)
			}
			ch, err := mcp.Install(args[0], exe, serverArgs)
			if err != nil {
				return err
			}
			return a.Printer.Print(installView(ch))
		},
	}
	cmd.AddCommand(install)
	return cmd
}

type installView mcp.Change

func (v installView) Pretty(w io.Writer, color bool) {
	fmt.Fprintf(w, "%s %s\n", v.Action, v.Path)
	fmt.Fprintln(w, output.Dim(v.Command+" "+strings.Join(v.Args, " "), color))
}

// runCommand runs one tool's argv in a fresh App, so config and session are
// re-read on every call and a rotated cookie is saved as usual.
func (a *App) runCommand(ctx context.Context, argv []string) mcp.Result {
	var out, errBuf bytes.Buffer
	global := []string{"--no-input", "--error-format", "json"}
	if a.flagProfile != "" {
		global = append(global, "--profile", a.flagProfile)
	}
	// Diagnostics would corrupt the stdio transport, so stderr is captured;
	// on failure the envelope goes back to the agent.
	code := run(ctx, a.Version, append(global, argv...), strings.NewReader(""), &out, &errBuf)
	r := mcp.Result{Stdout: out.String()}
	if code != 0 {
		r.ErrorJSON = lastLine(errBuf.String())
	}
	return r
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}
