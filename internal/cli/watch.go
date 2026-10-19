package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// watchAdd is a validated add request.
type watchAdd struct {
	name     string
	interval time.Duration
	sinks    []string
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

// prepareWatchAdd validates the add flags before any session or network work,
// so a typo fails with exit 2 even when there is no session.
func (a *App) prepareWatchAdd(f *watchAddFlags) (watchAdd, error) {
	if !validWatchName(f.name) {
		return watchAdd{}, output.Usagef("watch name %q must be letters, digits, - or _", f.name)
	}
	iv, err := a.configuredInterval(f.interval)
	if err != nil {
		return watchAdd{}, err
	}
	if err := a.checkSinks(f.notify); err != nil {
		return watchAdd{}, err
	}
	return watchAdd{name: f.name, interval: iv, sinks: f.notify}, nil
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
func (a *App) addWatch(ctx context.Context, kind string, target any, add watchAdd, pages int, emitInitial bool) error {
	raw, err := json.Marshal(target)
	if err != nil {
		return err
	}
	st, err := a.openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	w, err := st.AddWatch(ctx, store.Watch{Name: add.name, Profile: a.Profile, Kind: kind, Target: raw, Sinks: add.sinks, Interval: add.interval, Pages: max(pages, 1)})
	if errors.Is(err, store.ErrExists) {
		return output.Usagef("watch %q already exists. Remove it first or pick another name", add.name)
	}
	if err != nil {
		return err
	}
	res, err := watch.Check(ctx, a.Client, w, map[string]store.Seen{})
	if err != nil {
		return err
	}
	// Baseline silently: keep the state, drop the "everything is new" events,
	// unless --emit-initial asked for them.
	events := res.Events
	if !emitInitial {
		events = nil
	}
	if err := st.CommitCheck(ctx, w.ID, res.Upserts, res.Removed, events); err != nil {
		return err
	}
	a.deliver(ctx, w, events)
	if emitInitial {
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
	cmd.AddCommand(add, a.watchListCmd(), a.watchRemoveCmd(), a.watchCheckCmd(), a.watchRunCmd(), a.watchEventsCmd())
	return cmd
}

func (a *App) watchAddSearchCmd() *cobra.Command {
	var f searchFlags
	var wf watchAddFlags
	cmd := &cobra.Command{
		Use:   "search [keywords...]",
		Short: "Watch a search for new products and price changes",
		Args:  anyArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			add, err := a.prepareWatchAdd(&wf)
			if err != nil {
				return err
			}
			if f.page > 1 {
				return output.Usagef("--page is not used for a watch; use --pages N to diff several result pages")
			}
			q, err := searchQuery(args, f)
			if err != nil {
				return err
			}
			if err := a.requireSession(); err != nil {
				return err
			}
			return a.addWatch(cmd.Context(), store.KindSearch, watch.SearchTarget{Query: q}, add, wf.pages, wf.emitInitial)
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
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			add, err := a.prepareWatchAdd(&wf)
			if err != nil {
				return err
			}
			if err := a.requireSession(); err != nil {
				return err
			}
			p, err := a.Client.Product(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.addWatch(cmd.Context(), store.KindProduct, watch.ProductTarget{Ref: p.Ref, URL: p.URL}, add, 1, wf.emitInitial)
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
		Args:  noArgs,
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
		Args:  exactArgs(1),
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
		switch {
		case errors.Is(err, store.ErrNotFound):
			return nil, output.Usagef("no watch named %q", name)
		case err != nil:
			return nil, err
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

// checkWatches runs one check per watch and returns the events. The store is
// owned by the caller, so a check does not reopen it.
func (a *App) checkWatches(ctx context.Context, st *store.Store, ws []store.Watch) (eventList, error) {
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

// fastestInterval is the cadence of a run loop over several watches: the
// shortest interval among them, never zero.
func fastestInterval(ws []store.Watch) time.Duration {
	iv := time.Duration(0)
	for _, w := range ws {
		if w.Interval > 0 && (iv == 0 || w.Interval < iv) {
			iv = w.Interval
		}
	}
	if iv == 0 {
		return defaultWatchInterval
	}
	return iv
}

// bindWatchSelection adds --all and resolves the positional watch name.
func (a *App) watchSelection(cmd *cobra.Command, args []string, all bool) (*store.Store, []store.Watch, error) {
	if err := a.requireSession(); err != nil {
		return nil, nil, err
	}
	st, err := a.openStore()
	if err != nil {
		return nil, nil, err
	}
	name := ""
	if len(args) == 1 {
		name = args[0]
	}
	ws, err := a.selectWatches(cmd.Context(), st, name, all)
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	return st, ws, nil
}

func (a *App) watchCheckCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "check [NAME]",
		Short: "Fetch each watch once and report what changed",
		Args:  maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, ws, err := a.watchSelection(cmd, args, all)
			if err != nil {
				return err
			}
			defer st.Close()
			events, err := a.checkWatches(cmd.Context(), st, ws)
			if err != nil {
				return err
			}
			return a.Printer.Print(events)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "every watch of this profile")
	return cmd
}

func (a *App) watchRunCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "run [NAME]",
		Short: "Check every watch in a loop until interrupted",
		Args:  maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, ws, err := a.watchSelection(cmd, args, all)
			if err != nil {
				return err
			}
			defer st.Close()
			iv := fastestInterval(ws)
			for {
				events, err := a.checkWatches(cmd.Context(), st, ws)
				if err != nil {
					return err
				}
				if err := a.Printer.Print(events); err != nil {
					return err
				}
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

func (a *App) watchEventsCmd() *cobra.Command {
	var since string
	var limit int
	cmd := &cobra.Command{
		Use:   "events [NAME]",
		Short: "Print stored events",
		Args:  maxArgs(1),
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
