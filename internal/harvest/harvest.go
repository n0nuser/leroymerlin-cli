// Package harvest reads a live leroymerlin.es browser session straight out of
// a real Chrome, so the user never has to export cookies by hand. It launches
// a headed browser (a human can clear a DataDome challenge in it) or attaches
// to one already running, then reads the cookies and the browser's own user
// agent over CDP. It is pure data on the way out: the caller maps it through
// ParseExport and the config package.
//
// No test in this package launches a browser; the browser step is integration
// only.
package harvest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

// Domain is the site whose cookies are kept.
const Domain = "leroymerlin.es"

// Home is the page the browser opens; a challenge for it is the one to clear.
const Home = "https://www.leroymerlin.es/"

// DefaultDebugURL is probed when no endpoint is given: a Chrome started with
// --remote-debugging-port=9222. IPv4 on purpose: Chrome binds 127.0.0.1 and
// "localhost" can resolve to ::1 on Linux.
const DefaultDebugURL = "http://127.0.0.1:9222"

// DefaultTimeout is how long a person gets to clear a challenge.
const DefaultTimeout = 5 * time.Minute

// PollInterval is how often the page is checked.
const PollInterval = time.Second

// Options say where the browser is and how long to wait.
type Options struct {
	// BrowserURL attaches to an already-running Chrome (http://host:port or a
	// ws:// devtools URL). Empty launches one.
	BrowserURL string
	// Chrome is the browser binary. Empty searches PATH and the usual places.
	Chrome string
	// UserDataDir is the profile a fresh launched browser keeps, so the
	// DataDome session survives across runs.
	UserDataDir string
	// ProfileDir is the real Chrome profile to copy. Empty auto-detects one.
	ProfileDir string
	// TempDir is the parent for the copied profile. Empty uses the user cache.
	TempDir string
	// UserAgent overrides the browser's own when reading it.
	UserAgent string
	// Timeout bounds the wait for the challenge to clear.
	Timeout time.Duration
}

// Result is what the browser gave: its user agent and every cookie, and where
// the browser came from.
type Result struct {
	UserAgent string
	Cookies   []*network.Cookie
	// Source is "attach", "profile" or "fresh".
	Source string
	// Profile is the real Chrome profile root when Source is "profile".
	Profile string
}

// Run opens the site, waits for the challenge to clear, then reads the
// cookies and the user agent. It never returns on a timeout without saying so.
func Run(ctx context.Context, o Options) (Result, error) {
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.BrowserURL != "" {
		ctx, cancel := remote.NewAllocator(ctx, NormalizeDebugURL(o.BrowserURL))
		defer cancel()
		return read(ctx, o, "attach", "")
	}
	root, err := DetectProfile(o.ProfileDir)
	if err == nil {
		return runCopied(ctx, o, root)
	}
	if o.ProfileDir != "" {
		// An explicit profile that does not work is an error, not a silent
		// switch to a fresh browser with no session.
		return Result{}, err
	}
	// No real profile: a fresh browser may need a person to clear a check.
	return runFresh(ctx, o)
}

// read drives one browser context: open the home page, wait for the
// challenge, then read the cookies and user agent.
func read(ctx context.Context, o Options, source, profile string) (Result, error) {
	browserCtx, cancelBrowser := chromedp.NewContext(ctx)
	defer cancelBrowser()

	if _, err := chromedp.Run(browserCtx, chromedp.Navigate(Home)); err != nil {
		return Result{}, fmt.Errorf("opening %s: %w", Home, err)
	}
	if err := waitClear(ctx, browserCtx, o.Timeout, source == "profile"); err != nil {
		return Result{}, err
	}
	ua, err := chromedp.Run(browserCtx, chromedp.Evaluate[string](userAgentJS))
	if err != nil {
		return Result{}, fmt.Errorf("reading the user agent: %w", err)
	}
	ua = strings.TrimSpace(ua)
	if o.UserAgent != "" {
		ua = strings.TrimSpace(o.UserAgent)
	}
	cookies, err := chromedp.Run(browserCtx, cookiesAction)
	if err != nil {
		return Result{}, fmt.Errorf("reading the cookies: %w", err)
	}
	return Result{UserAgent: ua, Cookies: cookies, Source: source, Profile: profile}, nil
}

// runCopied copies the real profile's cookie store into a temporary 0700
// directory, launches Chrome on it with a free DevTools port, and always
// removes the copy and the browser on the way out.
func runCopied(ctx context.Context, o Options, root string) (res Result, err error) {
	temp, cleanup, err := PrepareProfile(root, o.TempDir)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		if cerr := cleanup(); cerr != nil && err == nil {
			err = fmt.Errorf("removing the temporary Chrome profile: %w", cerr)
		}
	}()
	path, err := FindChrome(o.Chrome, execLookPath)
	if err != nil {
		return Result{}, err
	}
	port, err := FreePort()
	if err != nil {
		return Result{}, fmt.Errorf("finding a free DevTools port: %w", err)
	}
	// A fixed debugging port means chromedp talks over a websocket, so the
	// allocator needs the remote dialer.
	opts := append(launchOptions(path, temp),
		remote.WebSocket,
		chromedp.Flag("remote-debugging-port", strconv.Itoa(port)),
		chromedp.Flag("remote-debugging-address", "127.0.0.1"),
	)
	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	defer cancel() // kills the browser this allocator started
	return read(allocCtx, o, "profile", root)
}

// runFresh launches a browser with its own persistent profile. It is the
// fallback when no real profile exists, and may need a human to clear a check.
func runFresh(ctx context.Context, o Options) (Result, error) {
	path, err := FindChrome(o.Chrome, execLookPath)
	if err != nil {
		return Result{}, err
	}
	allocCtx, cancel := chromedp.NewExecAllocator(ctx, launchOptions(path, o.UserDataDir)...)
	defer cancel()
	return read(allocCtx, o, "fresh", "")
}

// launchOptions is the headed browser every launched harvest uses. The copied
// path adds its websocket dialer and fixed port flags on top.
func launchOptions(path, profileDir string) []chromedp.ExecAllocatorOption {
	return append(slices.Clone(chromedp.DefaultExecAllocatorOptions[:]),
		// Headed on purpose: DataDome shows a captcha a person has to clear.
		// VisibleWindow drops --headless and the "controlled by software"
		// infobar; it needs a display.
		chromedp.VisibleWindow,
		chromedp.ExecPath(path),
		chromedp.UserDataDir(profileDir),
	)
}

func waitClear(ctx context.Context, browserCtx context.Context, timeout time.Duration, copied bool) error {
	deadline := time.Now().Add(timeout)
	for {
		clear, err := chromedp.Run(browserCtx, chromedp.Evaluate[bool](clearJS))
		if err == nil && clear {
			if cookies, err := chromedp.Run(browserCtx, cookiesAction); err == nil && hasCookie(cookies, "datadome") {
				return nil
			}
		}
		if time.Now().After(deadline) {
			if copied {
				return fmt.Errorf("the challenge was not cleared within %s. If the window shows the site but not your session, Chrome could not decrypt the copied cookies (the OS keychain must be available). Otherwise solve the check in the window and run `leroymerlin auth harvest` again", timeout)
			}
			return fmt.Errorf("the challenge was not cleared within %s. Solve it in the browser window and run `leroymerlin auth harvest` again", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(PollInterval):
		}
	}
}

// cookiesAction reads every cookie the browser holds. It is a chromedp action
// so it can run on the browser context like any other step.
func cookiesAction(ctx context.Context, t *chromedp.Target) ([]*network.Cookie, error) {
	res, err := cdp.Call(ctx, t, network.GetCookies, network.GetCookiesParams{})
	if err != nil {
		return nil, err
	}
	return res.Cookies, nil
}

// clearJS is true once the storefront, not the challenge, is on screen. The
// challenge page's title is exactly "leroymerlin.es" and it loads DataDome's
// captcha script when it wants a check.
const clearJS = `(() => {
  const t = (document.title || "").trim();
  if (t === "" || t.toLowerCase() === "leroymerlin.es") return false;
  const b = document.body ? document.body.innerText.slice(0, 300) : "";
  if (/enable js|disable any ad blocker/i.test(b)) return false;
  if (document.querySelector('script[src*="captcha-delivery"],iframe[src*="captcha-delivery"]')) return false;
  return true;
})()`

const userAgentJS = `navigator.userAgent`

func hasCookie(cookies []*network.Cookie, name string) bool {
	for _, c := range cookies {
		if c == nil {
			continue
		}
		if c.Name == name && hostMatches(c.Domain) {
			return true
		}
	}
	return false
}

// hostMatches reports whether a cookie domain belongs to the site, so a
// lookalike (leroymerlin.es.evil.example) does not count and does not travel.
func hostMatches(domain string) bool {
	d := strings.ToLower(strings.TrimPrefix(domain, "."))
	return d == Domain || strings.HasSuffix(d, "."+Domain)
}

// CookiesToExport renders the browser cookies as an export the importer
// already reads: a {"userAgent","cookies"} wrapper, the same shape a script
// that reads document.cookie produces. ParseExport then keeps only
// leroymerlin.es cookies and drops the rest.
func CookiesToExport(cookies []*network.Cookie, userAgent string) ([]byte, error) {
	type exported struct {
		Name           string  `json:"name"`
		Value          string  `json:"value"`
		Domain         string  `json:"domain"`
		Path           string  `json:"path,omitempty"`
		ExpirationDate float64 `json:"expirationDate,omitempty"`
		Secure         bool    `json:"secure,omitempty"`
		HTTPOnly       bool    `json:"httpOnly,omitempty"`
	}
	out := struct {
		UserAgent string     `json:"userAgent,omitempty"`
		Cookies   []exported `json:"cookies"`
	}{UserAgent: strings.TrimSpace(userAgent)}
	for _, c := range cookies {
		if c == nil || c.Name == "" {
			continue
		}
		e := exported{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path, Secure: c.Secure, HTTPOnly: c.HTTPOnly}
		if c.Expires > 0 { // CDP uses -1 for a session cookie
			e.ExpirationDate = c.Expires
		}
		out.Cookies = append(out.Cookies, e)
	}
	return json.Marshal(out)
}

// NormalizeDebugURL points a local DevTools endpoint at 127.0.0.1, because
// "localhost" can resolve to ::1 while Chrome listens on IPv4.
func NormalizeDebugURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		u.Host = net.JoinHostPort("127.0.0.1", u.Port())
	}
	return u.String()
}

// execLookPath is exec.LookPath, as a variable so tests can stand in for it.
var execLookPath = exec.LookPath

// browserNames are tried in PATH, and wellKnown as absolute paths, when no
// binary is set. The override (--chrome or LEROYMERLIN_CHROME) wins over both.
var browserNames = []string{
	"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
	"chrome", "brave-browser", "microsoft-edge", "microsoft-edge-stable",
}

func wellKnownPaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	case "windows":
		var out []string
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if base == "" {
				continue
			}
			out = append(out,
				filepath.Join(base, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`),
			)
		}
		return out
	}
	return nil
}

// FindChrome resolves the browser binary: the override (a name or a path), a
// common name in PATH, or one of the usual install locations.
func FindChrome(override string, lookPath func(string) (string, error)) (string, error) {
	if override != "" {
		if p, err := lookPath(override); err == nil {
			return p, nil
		}
		if isFile(override) {
			return override, nil
		}
		return "", fmt.Errorf("%q is not a browser I can run. Set --chrome or LEROYMERLIN_CHROME to Chrome or Chromium", override)
	}
	for _, name := range browserNames {
		if p, err := lookPath(name); err == nil {
			return p, nil
		}
	}
	for _, p := range wellKnownPaths() {
		if isFile(p) {
			return p, nil
		}
	}
	return "", errors.New("no Chrome or Chromium found. Install one, or set --chrome/LEROYMERLIN_CHROME to its path")
}

// DebugURLReachable reports whether a Chrome debug endpoint answers, so the
// command can attach to a browser a person already has open.
func DebugURLReachable(ctx context.Context, url string, hc *http.Client) bool {
	if hc == nil {
		hc = &http.Client{Timeout: 700 * time.Millisecond}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(NormalizeDebugURL(url), "/")+"/json/version", nil)
	if err != nil {
		return false
	}
	resp, err := hc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
