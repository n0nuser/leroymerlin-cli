package cli

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/leroymerlin-cli/internal/output"
	"github.com/n0nuser/leroymerlin-cli/internal/sink"
	"github.com/n0nuser/leroymerlin-cli/internal/store"
)

func (a *App) sinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sink",
		Short: "Sinks from config.toml: where watch events are copied",
		Long: `Sinks from config.toml: where watch events are copied.

By default events go to stdout. A watch can also name sinks with --notify; they
receive a copy of every event of that watch. Three types are supported:

  [sinks.topic]
  type = "ntfy"
  url = "https://ntfy.sh"
  topic = "leroymerlin"

  [sinks.hook]
  type = "webhook"
  url = "https://example.invalid/hooks/leroymerlin"

  [sinks.script]
  type = "exec"
  command = ["~/bin/on-leroymerlin-event"]`,
	}
	cmd.AddCommand(a.sinkListCmd(), a.sinkTestCmd())
	return cmd
}

func (a *App) sinkListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the sinks declared in config.toml",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			type row struct {
				Name    string `json:"name"`
				Type    string `json:"type"`
				Target  string `json:"target"`
				Invalid string `json:"invalid,omitempty"`
			}
			rows := []row{}
			names := make([]string, 0, len(a.Cfg.Sinks))
			for name := range a.Cfg.Sinks {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				cfg := a.Cfg.Sinks[name]
				r := row{Name: name, Type: cfg.Type, Target: cfg.URL}
				if cfg.Type == "exec" {
					r.Target = strings.Join(cfg.Command, " ")
				}
				if err := sink.Validate(name, cfg); err != nil {
					r.Invalid = err.Error()
				}
				rows = append(rows, r)
			}
			return a.Printer.Print(rows)
		},
	}
}

func (a *App) sinkTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test NAME",
		Short: "Send a test event to one sink",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, ok := a.Cfg.Sinks[args[0]]
			if !ok {
				return output.Usagef("no sink named %q in %s", args[0], a.Paths.ConfigFile)
			}
			if err := sink.Validate(args[0], cfg); err != nil {
				return output.Usagef("%v", err)
			}
			ev := store.Event{Type: "test", Watch: "test", Profile: a.Profile, At: time.Now().UTC(),
				Product: json.RawMessage(`{"ref":"00000000","name":"leroymerlin-cli sink test","url":"https://www.leroymerlin.es"}`)}
			if err := sink.Deliver(cmd.Context(), args[0], cfg, ev); err != nil {
				return err
			}
			return a.Printer.Print(map[string]any{"sink": args[0], "delivered": true})
		},
	}
}
