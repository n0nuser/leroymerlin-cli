package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/n0nuser/leroymerlin-cli/internal/config"
	"github.com/n0nuser/leroymerlin-cli/internal/output"
)

type profileRow struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

type profileList []profileRow

func (l profileList) Pretty(w io.Writer, color bool) {
	rows := [][]string{}
	for _, p := range l {
		mark := ""
		if p.Default {
			mark = "*"
		}
		rows = append(rows, []string{mark, p.Name})
	}
	output.Table(w, color, nil, rows)
}

func (a *App) profileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Several sessions side by side, one per store",
		Long: `A profile is one imported session. Since stock follows the store chosen in
that session, one profile per store lets you compare stores:

  leroymerlin auth login --cookies madrid.json --profile madrid
  leroymerlin auth login --cookies getafe.json --profile getafe
  leroymerlin profile use madrid`,
	}
	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "List profiles that have a session", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			names, err := config.Profiles(a.Paths)
			if err != nil {
				return err
			}
			def := config.ResolveProfile("", a.Cfg)
			out := profileList{}
			for _, n := range names {
				out = append(out, profileRow{Name: n, Default: n == def})
			}
			return a.Printer.Print(out)
		}},
		&cobra.Command{Use: "use NAME", Short: "Make NAME the default profile", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.ValidProfile(args[0]); err != nil {
				return output.Usagef("%v", err)
			}
			a.Cfg.DefaultProfile = args[0]
			if err := config.Save(a.Paths.ConfigFile, a.Cfg); err != nil {
				return err
			}
			return a.Printer.Print(map[string]string{"default_profile": args[0]})
		}},
	)
	var yes bool
	rm := &cobra.Command{Use: "remove NAME", Short: "Delete a profile's session", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := a.confirm(yes, "Delete the session of profile "+args[0]+"?"); err != nil {
			return err
		}
		removed, err := config.RemoveSession(a.Paths, args[0])
		if err != nil {
			return err
		}
		if args[0] == a.Profile {
			a.Session = nil
		}
		return a.Printer.Print(map[string]any{"profile": args[0], "removed": removed})
	}}
	rm.Flags().BoolVar(&yes, "yes", false, "do not ask")
	cmd.AddCommand(rm)
	return cmd
}
