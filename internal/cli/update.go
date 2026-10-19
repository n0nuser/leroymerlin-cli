package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/leroymerlin-cli/internal/output"
	"github.com/n0nuser/leroymerlin-cli/internal/updater"
)

func (a *App) updateCmd() *cobra.Command {
	var yes, check bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update leroymerlin to the latest stable release",
		Long: `Check GitHub for the latest stable release and replace this binary with it,
after verifying the archive against the release's checksums.txt. --check only
reports; --yes skips the prompt. Development builds (go install from a
branch, local builds) are never replaced: reinstall with go install instead.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !updater.Stable(a.Version) {
				return output.Usagef("this is a development build (%s); install a release or run `go install github.com/n0nuser/leroymerlin-cli/cmd/leroymerlin@latest`", a.Version)
			}
			r, err := updater.Latest(cmd.Context(), updater.Client())
			if err != nil {
				return err
			}
			newer := updater.Newer(r.Tag, a.Version)
			if check || !newer {
				return a.Printer.Print(map[string]any{"current_version": a.Version, "latest_version": strings.TrimPrefix(r.Tag, "v"), "update_available": newer})
			}
			if !yes {
				if !a.Interactive {
					return output.Usagef("%s is available; run `leroymerlin update --yes`", r.Tag)
				}
				if !a.askUpdate(r.Tag) {
					return nil
				}
			}
			return a.install(cmd.Context(), r)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "update without asking")
	cmd.Flags().BoolVar(&check, "check", false, "only report whether an update exists")
	return cmd
}

func (a *App) askUpdate(tag string) bool {
	fmt.Fprintf(a.Stderr, "leroymerlin %s is available (you have %s). Update? [Y/n] ", strings.TrimPrefix(tag, "v"), a.Version)
	// Byte by byte, so nothing past this line is consumed from stdin.
	var line []byte
	b := make([]byte, 1)
	for {
		if _, err := io.ReadFull(a.Stdin, b); err != nil {
			return false
		}
		if b[0] == '\n' {
			break
		}
		line = append(line, b[0])
	}
	ans := strings.ToLower(strings.TrimSpace(string(line)))
	return ans == "" || ans == "y" || ans == "yes"
}

func (a *App) install(ctx context.Context, r updater.Release) error {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return err
	}
	if err := updater.Install(ctx, updater.Client(), r, exe); err != nil {
		return err
	}
	fmt.Fprintf(a.Stderr, "updated to leroymerlin %s\n", strings.TrimPrefix(r.Tag, "v"))
	return nil
}

// maybeUpdate checks for a release at most once a day after an interactive
// command succeeds. Scripts, agents and --no-input never check or prompt;
// LEROYMERLIN_NO_UPDATE_CHECK=1 turns it off. Failures are silent.
func (a *App) maybeUpdate(ctx context.Context, cmd *cobra.Command) {
	if !a.ready || !a.Interactive || !updater.Stable(a.Version) || os.Getenv("LEROYMERLIN_NO_UPDATE_CHECK") != "" {
		return
	}
	top := cmd
	for top.HasParent() && top.Parent().HasParent() {
		top = top.Parent()
	}
	switch top.Name() {
	case "update", "mcp", "completion", "help", "auth", "login":
		return
	}
	stamp := filepath.Join(a.Paths.StateDir, "update-check")
	if fi, err := os.Stat(stamp); err == nil && time.Since(fi.ModTime()) < 24*time.Hour {
		return
	}
	// Record the attempt first, so being offline does not slow every command.
	if os.MkdirAll(a.Paths.StateDir, 0o700) != nil || os.WriteFile(stamp, nil, 0o600) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	r, err := updater.Latest(ctx, updater.Client())
	if err != nil || !updater.Newer(r.Tag, a.Version) {
		return
	}
	if a.askUpdate(r.Tag) {
		if err := a.install(context.Background(), r); err != nil {
			fmt.Fprintln(a.Stderr, "leroymerlin: update failed:", err)
		}
	}
}
