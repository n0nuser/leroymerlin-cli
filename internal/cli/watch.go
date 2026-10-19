package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/leroymerlin-cli/internal/output"
	"github.com/n0nuser/leroymerlin-cli/internal/sink"
	"github.com/n0nuser/leroymerlin-cli/internal/store"
	"github.com/n0nuser/leroymerlin-cli/internal/watch"
)

const (
	defaultWatchInterval = 5 * time.Minute
	minWatchInterval     = 30 * time.Second
)

// openStore opens the state database.
func (a *App) openStore() (*store.Store, error) {
	return store.Open(a.Paths.StateDB)
}

type watchAddFlags struct {
	name        string
	interval    time.Duration
	notify      []string
	pages       int
	emitInitial bool
}

func (a *App) bindWatchAddFlags(cmd *cobra.Command, f *watchAddFlags, pages bool) {
	cmd.Flags().StringVar(&f.name, "name", "", "watch name (required, used in events and commands)")
	cmd.Flags().DurationVar(&f.interval, "interval", 0, "how often this watch is due (default from config, 5m; floor 30s)")
	cmd.Flags().StringSliceVar(&f.notify, "notify", nil, "sink names from config.toml, repeatable")
	if pages {
		cmd.Flags().IntVar(&f.pages, "pages", 1, "result pages to diff per check")
	}
	cmd.Flags().BoolVar(&f.emitInitial, "emit-initial", false, "report everything that exists now as events instead of baselining silently")
}

func (a *App) configuredInterval(flag time.Duration) (time.Duration, error) {
	iv := flag
	if iv == 0 && a.Cfg.Watch.Interval != "" {
		d, err := time.ParseDuration(a.Cfg.Watch.Interval)
		if err != nil {
			return 0, fmt.Errorf("config watch.interval: %w", err)
		}
		iv = d
	}
	if iv == 0 {
		iv = defaultWatchInterval
	}
	if iv < minWatchInterval {
		return 0, output.Usagef("interval %s is below the floor of %s", iv, minWatchInterval)
	}
	return iv, nil
}

func validWatchName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// checkSinks fails early when a --notify name is not defined in config.toml.
func (a *App) checkSinks(names []string) error {
	for _, n := range names {
		cfg, ok := a.Cfg.Sinks[n]
		if !ok {
			return output.Usagef("sink %q is not in config. Define [sinks.%s] in %s", n, n, a.Paths.ConfigFile)
		}
		if err := sink.Validate(n, cfg); err != nil {
			return output.Usagef("%v", err)
		}
	}
	return nil
}

// addWatch stores a watch and baselines it, so the first check only reports
// real changes.
func (a *App) addWatch(cmd *cobra.Command, kind string, target any, f *watchAddFlags) error {
	if !validWatchName(f.name) {
		return output.Usagef("watch name %q must be letters, digits, - or _", f.name)
	}
	iv, err := a.configuredInterval(f.interval)
	if err != nil {
		return err
	}
	if err := a.checkSinks(f.notify); err != nil {
		return err
	}
	raw, err := json.Marshal(target)
	if err != nil {
		return err
	}
	st, err := a.openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	w, err := st.AddWatch(cmd.Context(), store.Watch{Name: f.name, Profile: a.Profile, Kind: kind, Target: raw, Sinks: f.notify, Interval: iv, Pages: max(f.pages, 1)})
	if errors.Is(err, store.ErrExists) {
		return output.Usagef("watch %q already exists. Remove it first or pick another name", f.name)
	}
	if err != nil {
		return err
	}
	res, err := watch.Check(cmd.Context(), a.Client, w, map[string]store.Seen{})
	if err != nil {
		return err
	}
	// Baseline silently: keep the state, drop the "everything is new" events,
	// unless --emit-initial asked for them.
	events := res.Events
	if !f.emitInitial {
		events = nil
	}
	if err := st.CommitCheck(cmd.Context(), w.ID, res.Upserts, res.Removed, events); err != nil {
		return err
	}
	a.deliver(cmd.Context(), w, events)
	if f.emitInitial {
		return a.Printer.Print(eventList(events))
	}
	return a.Printer.Print(watchView{Watch: w, Seen: len(res.Upserts)})
}

// deliver copies events to a watch's sinks. A failing sink is reported on
// stderr and never undoes the check.
func (a *App) deliver(ctx context.Context, w store.Watch, events []store.Event) {
	for _, ev := range events {
		for _, name := range w.Sinks {
			cfg := a.Cfg.Sinks[name]
			if err := sink.Deliver(ctx, name, cfg, ev); err != nil {
				fmt.Fprintf(a.Stderr, "leroymerlin: sink %s: %v\n", name, err)
			}
		}
	}
}

type watchView struct {
	store.Watch
	Seen int `json:"seen,omitempty"`
}

func describeTarget(w store.Watch) string {
	switch w.Kind {
	case store.KindSearch:
		var t watch.SearchTarget
		if json.Unmarshal(w.Target, &t) == nil {
			q := t.Query
			if q.Keywords != "" {
				return fmt.Sprintf("search %q", q.Keywords)
			}
			if q.Category != "" {
				return "category " + q.Category
			}
		}
	case store.KindProduct:
		var t watch.ProductTarget
		if json.Unmarshal(w.Target, &t) == nil {
			return "product " + t.Ref
		}
	}
	return ""
}

func (v watchView) Pretty(w io.Writer, color bool) {
	fmt.Fprintf(w, "watch %s (%s) saved\n", v.Name, describeTarget(v.Watch))
}

type watchList []store.Watch

func (l watchList) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(l))
	for _, wt := range l {
		last := "never"
		if !wt.LastCheckAt.IsZero() {
			last = wt.LastCheckAt.Format(time.RFC3339)
		}
		rows = append(rows, []string{wt.Name, wt.Kind, output.Truncate(describeTarget(wt), 40), wt.Interval.String(), strings.Join(wt.Sinks, ","), last, output.Dim(wt.Profile, color)})
	}
	output.Table(w, color, []string{"WATCH", "KIND", "TARGET", "EVERY", "SINKS", "CHECKED", "PROFILE"}, rows)
}

// eventList prints events as one JSON document, or one per line with jsonl.
type eventList []store.Event

func (l eventList) Pretty(w io.Writer, color bool) {
	if len(l) == 0 {
		fmt.Fprintln(w, output.Dim("no changes", color))
		return
	}
	rows := make([][]string, 0, len(l))
	for _, ev := range l {
		var p struct {
			Ref   string `json:"ref"`
			Name  string `json:"name"`
			Price *struct {
				Amount   float64 `json:"amount"`
				Currency string  `json:"currency"`
			} `json:"price"`
		}
		_ = json.Unmarshal(ev.Product, &p)
		price := "-"
		if p.Price != nil {
			price = fmt.Sprintf("%.2f %s", p.Price.Amount, p.Price.Currency)
		}
		change := ""
		if len(ev.Change) > 0 {
			change = string(ev.Change)
		}
		rows = append(rows, []string{ev.At.Format("15:04:05"), ev.Type, ev.Watch, p.Ref, price, output.Truncate(p.Name, 40), output.Truncate(change, 24)})
	}
	output.Table(w, color, []string{"AT", "TYPE", "WATCH", "REF", "PRICE", "NAME", "CHANGE"}, rows)
}

func (a *App) watchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Track a search or a product and report only what changed",
		Long: `Track a search or a product and report only what changed.

A watch is local. "check" fetches every watch once, compares it with the last
check and prints the differences as events; "run" loops in the foreground.
Watches and their history live in the state database, never on leroymerlin.es.

Examples:
  leroymerlin watch add search "cemento cola" --max-price 20 --name cola
  leroymerlin watch add product 81866477 --name pegoland
  leroymerlin watch list
  leroymerlin watch check --all
  leroymerlin watch check --all --format jsonl | jq -r 'select(.type=="product.price_changed") | .product.url'
  leroymerlin watch events cola --since 24h`,
	}
	add := &cobra.Command{Use: "add", Short: "Create a watch"}
	add.AddCommand(a.watchAddSearchCmd(), a.watchAddProductCmd())
	cmd.AddCommand(add, a.watchListCmd(), a.watchRemoveCmd(), a.watchCheckCmd(false), a.watchRunCmd(), a.watchEventsCmd())
	return cmd
}

func (a *App) watchAddSearchCmd() *cobra.Command {
	var f searchFlags
	var wf watchAddFlags
	cmd := &cobra.Command{
		Use:   "search [keywords...]",
		Short: "Watch a search for new products and price changes",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !validWatchName(wf.name) {
				return output.Usagef("watch name %q must be letters, digits, - or _", wf.name)
			}
			q, err := searchQuery(args, f)
			if err != nil {
				return err
			}
			if err := a.requireSession(); err != nil {
				return err
			}
			return a.addWatch(cmd, store.KindSearch, watch.SearchTarget{Query: q}, &wf)
		},
	}
	a.bindSearchFlags(cmd, &f)
	a.bindWatchAddFlags(cmd, &wf, true)
	return cmd
}

func (a *App) watchAddProductCmd() *cobra.Command {
	var wf watchAddFlags
	cmd := &cobra.Command{
		Use:   "product REF",
		Short: "Watch one product for price and stock changes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !validWatchName(wf.name) {
				return output.Usagef("watch name %q must be letters, digits, - or _", wf.name)
			}
			if err := a.requireSession(); err != nil {
				return err
			}
			p, err := a.Client.Product(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.addWatch(cmd, store.KindProduct, watch.ProductTarget{Ref: p.Ref, URL: p.URL}, &wf)
		},
	}
	a.bindWatchAddFlags(cmd, &wf, false)
	return cmd
}

func (a *App) watchListCmd() *cobra.Command {
	var allProfiles bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List watches for the active profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			profile := a.Profile
			if allProfiles {
				profile = ""
			}
			ws, err := st.Watches(cmd.Context(), profile)
			if err != nil {
				return err
			}
			if ws == nil {
				ws = []store.Watch{}
			}
			return a.Printer.Print(watchList(ws))
		},
	}
	cmd.Flags().BoolVar(&allProfiles, "all-profiles", false, "include every profile's watches")
	return cmd
}

func (a *App) watchRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove NAME",
		Short: "Delete a watch and its history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(yes, fmt.Sprintf("Remove watch %q and its history?", args[0])); err != nil {
				return err
			}
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			removed, err := st.RemoveWatch(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if !removed {
				return output.Usagef("no watch named %q", args[0])
			}
			return a.Printer.Print(map[string]any{"removed": args[0]})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return cmd
}

// selectWatches resolves the watches a check should run.
func (a *App) selectWatches(ctx context.Context, st *store.Store, name string, all bool) ([]store.Watch, error) {
	if name != "" {
		w, err := st.WatchByName(ctx, name)
		if err != nil {
			return nil, output.Usagef("no watch named %q", name)
		}
		return []store.Watch{w}, nil
	}
	if !all {
		return nil, output.Usagef("name a watch or pass --all")
	}
	ws, err := st.Watches(ctx, a.Profile)
	if err != nil {
		return nil, err
	}
	if len(ws) == 0 {
		return nil, output.Usagef("no watches for this profile. Create one with `leroymerlin watch add search ...`")
	}
	return ws, nil
}

// checkWatches runs one check per watch and prints the events.
func (a *App) checkWatches(ctx context.Context, ws []store.Watch) (eventList, error) {
	st, err := a.openStore()
	if err != nil {
		return nil, err
	}
	defer st.Close()
	all := eventList{}
	for _, w := range ws {
		seen, err := st.Seen(ctx, w.ID)
		if err != nil {
			return nil, err
		}
		res, err := watch.Check(ctx, a.Client, w, seen)
		if err != nil {
			return nil, err
		}
		if err := st.CommitCheck(ctx, w.ID, res.Upserts, res.Removed, res.Events); err != nil {
			return nil, err
		}
		a.deliver(ctx, w, res.Events)
		all = append(all, res.Events...)
	}
	return all, nil
}

func (a *App) watchCheckCmd(run bool) *cobra.Command {
	var all bool
	use := "check [NAME]"
	short := "Fetch each watch once and report what changed"
	if run {
		use = "run [NAME]"
		short = "Check every watch in a loop until interrupted"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireSession(); err != nil {
				return err
			}
			st, err := a.openStore()
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			ws, err := a.selectWatches(cmd.Context(), st, name, all)
			st.Close()
			if err != nil {
				return err
			}
			if !run {
				events, err := a.checkWatches(cmd.Context(), ws)
				if err != nil {
					return err
				}
				return a.Printer.Print(events)
			}
			for {
				events, err := a.checkWatches(cmd.Context(), ws)
				if err != nil {
					return err
				}
				if err := a.Printer.Print(events); err != nil {
					return err
				}
				iv := ws[0].Interval
				select {
				case <-cmd.Context().Done():
					return nil
				case <-time.After(iv):
				}
			}
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "every watch of this profile")
	return cmd
}

func (a *App) watchRunCmd() *cobra.Command { return a.watchCheckCmd(true) }

func (a *App) watchEventsCmd() *cobra.Command {
	var since string
	var limit int
	cmd := &cobra.Command{
		Use:   "events [NAME]",
		Short: "Print stored events",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			var after time.Time
			if since != "" {
				d, err := time.ParseDuration(since)
				if err != nil {
					return output.Usagef("--since wants a duration like 24h")
				}
				after = time.Now().Add(-d)
			}
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			events, err := st.Events(cmd.Context(), name, after, limit)
			if err != nil {
				return err
			}
			if events == nil {
				events = []store.Event{}
			}
			return a.Printer.Print(eventList(events))
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "only events newer than this duration, e.g. 24h")
	cmd.Flags().IntVar(&limit, "limit", 0, "at most N events (0: all)")
	return cmd
}

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
	list := &cobra.Command{
		Use:   "list",
		Short: "List the sinks declared in config.toml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			type row struct {
				Name    string `json:"name"`
				Type    string `json:"type"`
				Target  string `json:"target"`
				Invalid string `json:"invalid,omitempty"`
			}
			rows := []row{}
			for name, cfg := range a.Cfg.Sinks {
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
	test := &cobra.Command{
		Use:   "test NAME",
		Short: "Send a test event to one sink",
		Args:  cobra.ExactArgs(1),
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
	cmd.AddCommand(list, test)
	return cmd
}
