// Package cli wires cobra commands to the leroymerlin client, config and
// output. One file per noun; handlers parse flags, call the client, print.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/leroymerlin-cli/internal/config"
	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
	"github.com/n0nuser/leroymerlin-cli/internal/output"
)

// App is the state of one invocation.
type App struct {
	Version string
	Paths   config.Paths
	Cfg     config.Config
	Profile string
	Client  *leroymerlin.Client
	Session *config.StoredSession
	Printer output.Printer

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// Interactive is true only when stdin, stdout and stderr are TTYs and
	// --no-input is absent. Prompts happen only then.
	Interactive bool

	flagProfile     string
	flagFormat      string
	flagNoColor     bool
	flagErrorFormat string
	flagNoInput     bool
	flagDebug       bool

	ready   bool
	started time.Time
}

// minInterval is the request spacing; tests set it to zero.
var minInterval = leroymerlin.DefaultMinInterval

// lastRequestFile carries the rate gate across invocations, so a shell loop
// is spaced like one long session.
const lastRequestFile = "last-request"

// Execute runs the CLI and returns the process exit code.
func Execute(version string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx, version, args, stdin, stdout, stderr)
}

// run is one invocation under ctx; the MCP server calls it per tool call.
func run(ctx context.Context, version string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &App{Version: version, Stdin: stdin, Stdout: stdout, Stderr: stderr}
	root := a.rootCmd()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	cmd, err := root.ExecuteContextC(ctx)
	// Persist cookie rotations whether or not the command succeeded: a
	// challenged request still rotates the cookie the next run must send.
	a.persistSession()
	a.persistGate()
	if err == nil {
		if cmd != nil {
			a.maybeUpdate(ctx, cmd)
		}
		return 0
	}
	if errors.Is(err, context.Canceled) || ctx.Err() != nil {
		fmt.Fprintln(stderr, "leroymerlin: interrupted")
		return 130
	}
	path := "leroymerlin"
	if cmd != nil {
		path = cmd.CommandPath()
	}
	ef := output.ErrorFormat(a.flagErrorFormat)
	if v := os.Getenv("LEROYMERLIN_ERROR_FORMAT"); v != "" && a.flagErrorFormat == "text" {
		ef = output.ErrorFormat(v)
	}
	return output.PrintError(stderr, err, ef, version, path)
}

func (a *App) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "leroymerlin",
		Short: "Search leroymerlin.es and check stock at your store, from the terminal",
		Long: `Search leroymerlin.es and check stock at your store, from the terminal.
Unofficial and read-only: no cart, no checkout, no account changes.

The site is behind bot protection, so the cli replays the session of your own
browser: export leroymerlin.es cookies (or a HAR) and run ` + "`leroymerlin auth login`" + `.
Store and category lists come from the public sitemaps and work without it.

Output is JSON by default so every command pipes into jq. Use --format pretty
for humans. Errors go to stderr; exit codes are in docs/cli-spec.md.

Examples:
  leroymerlin store list --format pretty
  leroymerlin auth login --cookies ~/Downloads/leroymerlin-cookies.json
  leroymerlin doctor

Agent usage: leroymerlin skills get leroymerlin-usage`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       a.Version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return a.setup(cmd)
		},
	}
	root.SetVersionTemplate("leroymerlin {{.Version}}\n")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return output.Usagef("%s. See `%s --help`", err.Error(), cmd.CommandPath())
	})

	pf := root.PersistentFlags()
	pf.StringVar(&a.flagProfile, "profile", "", "profile (one session, one store) to act as (env LEROYMERLIN_PROFILE)")
	pf.StringVar(&a.flagFormat, "format", "json", "output format: json, jsonl, pretty")
	pf.BoolVar(&a.flagNoColor, "no-color", false, "disable colour in pretty output (also NO_COLOR)")
	pf.StringVar(&a.flagErrorFormat, "error-format", "text", "error rendering on stderr: text or json")
	pf.BoolVar(&a.flagNoInput, "no-input", false, "never prompt; fail when input is missing")
	pf.BoolVar(&a.flagDebug, "debug", false, "log every http request to stderr (cookies never printed)")
	_ = root.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"json", "jsonl", "pretty"}, cobra.ShellCompDirectiveNoFileComp))
	_ = root.RegisterFlagCompletionFunc("error-format", cobra.FixedCompletions([]string{"text", "json"}, cobra.ShellCompDirectiveNoFileComp))

	root.AddCommand(
		a.authCmd(), a.loginCmd(), a.profileCmd(),
		a.storeCmd(), a.categoryCmd(),
		a.configCmd(), a.doctorCmd(),
		a.mcpCmd(), a.skillsCmd(), a.updateCmd(),
	)
	return root
}

// setup loads config and session and builds the client. No network.
func (a *App) setup(cmd *cobra.Command) error {
	if cmd.Name() == "help" || cmd.Name() == "completion" || (cmd.Parent() != nil && cmd.Parent().Name() == "completion") {
		return nil
	}
	format, err := output.ParseFormat(a.flagFormat)
	if err != nil {
		return err
	}
	if a.flagErrorFormat != "text" && a.flagErrorFormat != "json" {
		return output.Usagef("--error-format must be text or json")
	}
	stdinF, _ := a.Stdin.(*os.File)
	stdoutF, _ := a.Stdout.(*os.File)
	stderrF, _ := a.Stderr.(*os.File)
	a.Interactive = !a.flagNoInput && output.IsTerminal(stdinF) && output.IsTerminal(stdoutF) && output.IsTerminal(stderrF)
	a.Printer = output.Printer{W: a.Stdout, Format: format, Color: output.ColorEnabled(a.flagNoColor, stdoutF)}

	a.Paths = config.DefaultPaths()
	if a.Cfg, err = config.Load(a.Paths.ConfigFile); err != nil {
		if underCommand(cmd, "config") {
			fmt.Fprintln(a.Stderr, "leroymerlin: warning:", err)
		} else {
			return output.Usagef("%v. Fix it with `leroymerlin config` or by hand", err)
		}
	}
	a.Profile = config.ResolveProfile(a.flagProfile, a.Cfg)
	if err := config.ValidProfile(a.Profile); err != nil {
		return output.Usagef("%v", err)
	}
	a.Client = leroymerlin.New()
	a.Client.MinInterval = minInterval
	if a.flagDebug {
		a.Client.Debug = a.Stderr
	}
	if raw, err := os.ReadFile(filepath.Join(a.Paths.StateDir, lastRequestFile)); err == nil {
		if t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(raw))); err == nil {
			leroymerlin.SeedLastRequest(t)
		}
	}
	a.started = time.Now()
	if a.Session, err = config.LoadSession(a.Paths, a.Profile); err != nil {
		return err
	}
	if a.Session != nil {
		a.Client.Session = &a.Session.Session
	}
	a.ready = true
	return nil
}

// requireSession is called by commands that need the storefront itself.
func (a *App) requireSession() error {
	if a.Session == nil {
		return &leroymerlin.Error{Kind: leroymerlin.KindAuth,
			Msg: fmt.Sprintf("profile %q has no session. Export leroymerlin.es cookies from your browser and run `leroymerlin auth login --cookies FILE`", a.Profile)}
	}
	return nil
}

// persistSession writes back cookies the site rotated, for file sessions only.
func (a *App) persistSession() {
	if !a.ready || a.Session == nil || a.Session.Source != config.SourceFile || !a.Session.Dirty() {
		return
	}
	if err := config.SaveSession(a.Paths, a.Profile, a.Session); err != nil {
		fmt.Fprintln(a.Stderr, "leroymerlin: warning: could not save the rotated session:", err)
	}
}

// persistGate records when this invocation last sent a request.
func (a *App) persistGate() {
	if !a.ready {
		return
	}
	if last := leroymerlin.LastRequest(); last.After(a.started) {
		_ = config.WriteAtomic(filepath.Join(a.Paths.StateDir, lastRequestFile), []byte(last.UTC().Format(time.RFC3339Nano)), 0o600)
	}
}

func underCommand(cmd *cobra.Command, name string) bool {
	for c := cmd; c != nil && c.HasParent(); c = c.Parent() {
		if c.Name() == name && !c.Parent().HasParent() {
			return true
		}
	}
	return false
}

// confirm asks a yes/no question. Without a terminal, --yes is required.
func (a *App) confirm(yes bool, prompt string) error {
	if yes {
		return nil
	}
	if !a.Interactive {
		return output.Usagef("%s Pass --yes to confirm without a prompt", prompt)
	}
	fmt.Fprintf(a.Stderr, "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(a.Stdin).ReadString('\n')
	if s := strings.ToLower(strings.TrimSpace(line)); s != "y" && s != "yes" {
		return output.Usagef("cancelled")
	}
	return nil
}
