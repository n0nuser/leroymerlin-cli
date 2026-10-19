package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/leroymerlin-cli/internal/config"
	"github.com/n0nuser/leroymerlin-cli/internal/harvest"
	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
	"github.com/n0nuser/leroymerlin-cli/internal/output"
)

// The harvest can be pointed at a browser without a flag.
const (
	chromeURLEnv  = "LEROYMERLIN_CHROME_URL"
	chromePathEnv = "LEROYMERLIN_CHROME"
)

type harvestFlags struct {
	browserURL string
	chrome     string
	userAgent  string
	timeout    time.Duration
}

func (a *App) harvestCmd() *cobra.Command {
	var f harvestFlags
	cmd := &cobra.Command{
		Use:   "harvest",
		Short: "Read the session out of a real Chrome window",
		Long: `Open leroymerlin.es in a real Chrome and read the session it earns, instead
of exporting cookies by hand.

The browser opens headed so you can clear any DataDome check it shows; the
window stays open while the command waits. If a Chrome debug endpoint is
already running (--browser-url, LEROYMERLIN_CHROME_URL, or the default
http://localhost:9222), the command attaches to it instead of launching.

A launched browser keeps its profile under the data directory, so the session
survives across runs. The harvested cookies and the browser's own user agent
are stored 0600 exactly as ` + "`auth login`" + ` stores an export. Choose your
store in that window before harvesting: stock follows it.

Examples:
  leroymerlin auth harvest
  leroymerlin auth harvest --chrome /usr/bin/chromium --timeout 2m
  leroymerlin auth harvest --browser-url http://localhost:9222`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if os.Getenv(config.CookieJarEnv) != "" {
				return output.Usagef("%s is set, so a stored session would be ignored. Unset it to harvest", config.CookieJarEnv)
			}
			ctx := cmd.Context()
			browserURL, chromePath := resolveChromeEnv(f.browserURL, f.chrome)
			opts := harvest.Options{
				BrowserURL:  browserURL,
				Chrome:      chromePath,
				UserDataDir: a.Paths.ChromeDir,
				UserAgent:   f.userAgent,
				Timeout:     f.timeout,
			}
			if opts.BrowserURL == "" && harvest.DebugURLReachable(ctx, harvest.DefaultDebugURL, nil) {
				opts.BrowserURL = harvest.DefaultDebugURL
				fmt.Fprintf(a.Stderr, "leroymerlin: attaching to the Chrome already listening on %s\n", opts.BrowserURL)
			}
			res, err := harvest.Run(ctx, opts)
			if err != nil {
				return err
			}
			raw, err := harvest.CookiesToExport(res.Cookies, res.UserAgent)
			if err != nil {
				return err
			}
			imported, err := leroymerlin.ParseExport(raw, leroymerlin.FormatJSON)
			if err != nil {
				return err
			}
			s := &config.StoredSession{Session: imported.Session, ImportedAt: time.Now().UTC().Truncate(time.Second), ImportFormat: "chrome"}
			if err := config.SaveSession(a.Paths, a.Profile, s); err != nil {
				return err
			}
			a.Session = s
			a.Client.Session = &s.Session
			return a.Printer.Print(harvestResult{Profile: a.Profile, Path: s.Path, Format: s.ImportFormat, Skipped: imported.Skipped, Session: s.Summarize()})
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.browserURL, "browser-url", "", "attach to this Chrome DevTools endpoint instead of launching (env "+chromeURLEnv+"; default: probe "+harvest.DefaultDebugURL+")")
	fl.StringVar(&f.chrome, "chrome", "", "Chrome or Chromium binary (env "+chromePathEnv+")")
	fl.StringVar(&f.userAgent, "user-agent", "", "override the user agent (default: the browser's own)")
	fl.DurationVar(&f.timeout, "timeout", harvest.DefaultTimeout, "how long to wait for the challenge to clear")
	return cmd
}

type harvestResult struct {
	Profile string              `json:"profile"`
	Path    string              `json:"path"`
	Format  string              `json:"import_format"`
	Skipped int                 `json:"skipped_other_domains"`
	Session leroymerlin.Summary `json:"session"`
}

func (r harvestResult) Pretty(w io.Writer, color bool) {
	fmt.Fprintf(w, "harvested %d leroymerlin.es cookies (%s) into profile %s\n", r.Session.Cookies, r.Format, r.Profile)
	if r.Skipped > 0 {
		fmt.Fprintln(w, output.Dim(fmt.Sprintf("skipped %d cookies for other sites", r.Skipped), color))
	}
	if !r.Session.HasDataDome {
		fmt.Fprintln(w, "warning: no datadome cookie; the site will likely challenge the cli")
	}
	if r.Session.UserAgent != "" {
		fmt.Fprintln(w, output.Dim("user agent "+r.Session.UserAgent, color))
	}
	fmt.Fprintln(w, output.Dim("stored at "+r.Path, color))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// resolveChromeEnv picks the DevTools endpoint and the browser binary: the
// flag wins, then the environment.
func resolveChromeEnv(browserURL, chrome string) (string, string) {
	return firstNonEmpty(browserURL, os.Getenv(chromeURLEnv)), firstNonEmpty(chrome, os.Getenv(chromePathEnv))
}
