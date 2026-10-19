// Package leroymerlin is the client for leroymerlin.es: a Chrome-fingerprint
// transport, the user's imported browser session, one sequential rate gate,
// and the parsers for what the site returns. It never touches the disk; the
// CLI loads and saves sessions through the config package.
package leroymerlin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is the storefront. LEROYMERLIN_BASE_URL overrides it (tests).
const DefaultBaseURL = "https://www.leroymerlin.es"

// DefaultUserAgent is used when the session does not carry the user agent of
// the browser it came from. It has to look like the Chrome the transport
// impersonates, or the pair is inconsistent.
const DefaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"

// DefaultMinInterval spaces requests. The CLI is for one person browsing, not
// for crawling; this keeps it at roughly human speed.
const DefaultMinInterval = 1500 * time.Millisecond

// maxBody caps any response read into memory.
const maxBody = 32 << 20

// Retry policy for transient failures: two extra attempts, backing off from
// retryBase (500ms, then 1s) and never waiting longer than retryCap. Bot
// protection (403) is never retried.
const (
	maxAttempts = 3
	retryCap    = 5 * time.Second
	// maxRetryAfter is the longest Retry-After the client will wait out. A
	// longer one is surfaced with the wait time instead.
	maxRetryAfter = 5 * time.Minute
)

// retryBase is a variable so tests can shrink the backoff.
var retryBase = 500 * time.Millisecond

// retryBackoff is the wait before attempt n+1: 500ms, then 1s, capped.
func retryBackoff(attempt int) time.Duration {
	d := retryBase << attempt
	if d > retryCap || d <= 0 {
		return retryCap
	}
	return d
}

// Client talks to leroymerlin.es. It is safe for concurrent use, but requests
// are serialized through the rate gate on purpose.
type Client struct {
	BaseURL     string
	HTTP        *http.Client
	Session     *Session
	MinInterval time.Duration
	Debug       io.Writer // request log, cookies redacted; nil for none
}

// gate spaces requests across every Client in the process (the MCP server
// runs one per tool call). The CLI seeds it from disk so separate
// invocations are spaced too.
var gate struct {
	sync.Mutex
	last time.Time
}

// SeedLastRequest tells the gate when a previous process last sent a
// request. Earlier times than the one already known are ignored.
func SeedLastRequest(t time.Time) {
	gate.Lock()
	defer gate.Unlock()
	if t.After(gate.last) && !t.After(time.Now()) {
		gate.last = t
	}
}

// LastRequest is when the last request left this process, zero if none.
func LastRequest() time.Time {
	gate.Lock()
	defer gate.Unlock()
	return gate.last
}

// New builds a client with the Chrome transport.
func New() *Client {
	base := DefaultBaseURL
	if v := os.Getenv("LEROYMERLIN_BASE_URL"); v != "" {
		base = strings.TrimRight(v, "/")
	}
	return &Client{
		BaseURL:     base,
		HTTP:        &http.Client{Transport: ChromeTransport(nil), Timeout: 60 * time.Second, CheckRedirect: sameSite},
		MinInterval: DefaultMinInterval,
	}
}

// sameSite follows redirects only within leroymerlin.es, so the session is
// never sent elsewhere, and stops after five hops.
func sameSite(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("too many redirects")
	}
	if !ownDomain(req.URL.Hostname()) && req.URL.Host != via[0].URL.Host {
		return http.ErrUseLastResponse
	}
	return nil
}

// Page is a fetched document.
type Page struct {
	URL    string
	Status int
	Header http.Header
	Body   []byte
}

// GetHTML fetches a storefront page as a browser navigation would.
func (c *Client) GetHTML(ctx context.Context, path string) (*Page, error) {
	return c.get(ctx, path, map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		"Upgrade-Insecure-Requests": "1",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
	})
}

// GetXML fetches a sitemap or other XML document.
func (c *Client) GetXML(ctx context.Context, path string) (*Page, error) {
	return c.get(ctx, path, map[string]string{"Accept": "application/xml,text/xml;q=0.9,*/*;q=0.8"})
}

// get runs attempt with the documented retry policy. It retries 5xx and
// timeout errors (KindNetwork + Retryable) up to maxAttempts, and an HTTP 429
// once when its Retry-After is short enough. A 403 challenge is returned as
// it is: retrying it would only dig the session deeper.
func (c *Client) get(ctx context.Context, path string, headers map[string]string) (*Page, error) {
	rateRetried := false
	for attempt := 0; ; attempt++ {
		p, err := c.attempt(ctx, path, headers)
		if err == nil {
			return p, nil
		}
		var le *Error
		if !errors.As(err, &le) {
			return nil, err
		}
		if le.Status == http.StatusTooManyRequests && !rateRetried {
			if wait, ok := retryAfter(p, time.Now()); ok {
				if wait > maxRetryAfter {
					return nil, &Error{Kind: KindBlocked, Status: le.Status, Endpoint: le.Endpoint, Retryable: true,
						Msg: fmt.Sprintf("leroymerlin.es is rate limiting; retry after %s", wait.Round(time.Second))}
				}
				rateRetried = true
				c.logf("GET %s -> 429, waiting %s (Retry-After)", path, wait.Round(time.Second))
				if err := sleep(ctx, wait); err != nil {
					return nil, err
				}
				continue
			}
		}
		if le.Kind == KindNetwork && le.Retryable && attempt+1 < maxAttempts {
			wait := retryBackoff(attempt)
			c.logf("GET %s -> %v, retrying in %s", path, err, wait)
			if err := sleep(ctx, wait); err != nil {
				return nil, err
			}
			continue
		}
		return nil, err
	}
}

// retryAfter reads a Retry-After header as a delay from now, accepting seconds
// and an HTTP-date. It reports false when the header is missing or unusable.
func retryAfter(p *Page, now time.Time) (time.Duration, bool) {
	if p == nil {
		return 0, false
	}
	v := strings.TrimSpace(p.Header.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			secs = 0
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}

// sleep waits for d or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// attempt is one request. It still enforces the process-wide pace, so retries
// never crowd the site.
func (c *Client) attempt(ctx context.Context, path string, headers map[string]string) (*Page, error) {
	u, err := url.Parse(c.BaseURL + path)
	if err != nil {
		return nil, Usage("bad path %q: %v", path, err)
	}
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	ua := DefaultUserAgent
	if c.Session != nil && c.Session.UserAgent != "" {
		ua = c.Session.UserAgent
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for k, v := range c.clientHints(ua) {
		req.Header.Set(k, v)
	}
	// The session is the cookie jar, so redirects within the site send and
	// keep rotated cookies hop by hop.
	hc := *c.HTTP
	if c.Session != nil {
		hc.Jar = c.Session.Jar()
	}
	start := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		c.logf("GET %s -> %v", u.Path, err)
		var ne net.Error
		retry := errors.As(err, &ne) && ne.Timeout()
		return nil, &Error{Kind: KindNetwork, Msg: fmt.Sprintf("cannot reach %s: %v", u.Host, err), Endpoint: u.Path, Retryable: retry}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, &Error{Kind: KindNetwork, Msg: fmt.Sprintf("reading %s: %v", u.Path, err), Endpoint: u.Path, Retryable: true}
	}
	c.logf("GET %s -> %d (%d bytes, %s)", u.Path, resp.StatusCode, len(body), time.Since(start).Round(time.Millisecond))
	p := &Page{URL: resp.Request.URL.String(), Status: resp.StatusCode, Header: resp.Header, Body: body}
	if err := classify(p, u.Path); err != nil {
		return p, err
	}
	return p, nil
}

// wait enforces MinInterval (plus up to 30% jitter) since the last request
// of any client.
// ponytail: two processes started at the same instant can still race the
// on-disk stamp; a lock file would close it if that ever matters.
func (c *Client) wait(ctx context.Context) error {
	gate.Lock()
	defer gate.Unlock()
	if c.MinInterval > 0 && !gate.last.IsZero() {
		d := c.MinInterval + time.Duration(rand.Int64N(int64(c.MinInterval)*3/10+1))
		if sleep := time.Until(gate.last.Add(d)); sleep > 0 {
			t := time.NewTimer(sleep)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
	}
	gate.last = time.Now()
	return nil
}

// clientHints are the Sec-CH-UA headers Chrome sends with every request. A
// session imported from a HAR carries the browser's own; the default user
// agent gets matching defaults; a custom user agent without hints gets none
// rather than a mismatched set.
func (c *Client) clientHints(ua string) map[string]string {
	if c.Session != nil && len(c.Session.ClientHints) > 0 {
		return c.Session.ClientHints
	}
	if ua == DefaultUserAgent {
		return map[string]string{
			"Sec-Ch-Ua":          `"Not(A:Brand";v="99", "Google Chrome";v="133", "Chromium";v="133"`,
			"Sec-Ch-Ua-Mobile":   "?0",
			"Sec-Ch-Ua-Platform": `"Linux"`,
		}
	}
	return nil
}

// IsBlockPage reports whether a response is the bot-protection challenge
// rather than the page asked for. The status alone is not enough: a real 403
// from the origin is a different problem from a challenge.
//
// Only a 403 or 429 can be a challenge: protected 200 pages may carry the
// vendor's headers and reference its hosts too.
func IsBlockPage(status int, h http.Header, body []byte) bool {
	if status != http.StatusForbidden && status != http.StatusTooManyRequests {
		return false
	}
	vendor := strings.EqualFold(h.Get("Server"), "DataDome") || h.Get("X-Datadome") != "" || h.Get("X-Datadome-Cid") != ""
	return vendor || bytes.Contains(body, []byte("captcha-delivery.com"))
}

// classify maps a response status to an error, or nil for a usable page.
func classify(p *Page, endpoint string) error {
	switch {
	case IsBlockPage(p.Status, p.Header, p.Body):
		return &Error{Kind: KindBlocked, Status: p.Status, Endpoint: endpoint, Retryable: true,
			Msg: "leroymerlin.es bot protection challenged the request. Open leroymerlin.es in the browser your session came from, pass any check it shows, then re-import the session"}
	case p.Status == http.StatusTooManyRequests:
		return &Error{Kind: KindBlocked, Status: p.Status, Endpoint: endpoint, Retryable: true, Msg: "leroymerlin.es is rate limiting. Wait a few minutes before retrying"}
	case p.Status == http.StatusNotFound || p.Status == http.StatusGone:
		return &Error{Kind: KindNotFound, Status: p.Status, Endpoint: endpoint, Msg: fmt.Sprintf("%s was not found on leroymerlin.es", endpoint)}
	case p.Status >= 500:
		return &Error{Kind: KindNetwork, Status: p.Status, Endpoint: endpoint, Retryable: true, Msg: fmt.Sprintf("leroymerlin.es answered HTTP %d. Try again later", p.Status)}
	case p.Status != http.StatusOK:
		return &Error{Kind: KindAPIChanged, Status: p.Status, Endpoint: endpoint, Body: Excerpt(p.Body),
			Msg: fmt.Sprintf("%s answered HTTP %d, which the cli does not expect", endpoint, p.Status)}
	}
	return nil
}

var secretish = regexp.MustCompile(`(?i)(datadome|token|session|auth)[^=:"]*"?\s*[=:]\s*"?[A-Za-z0-9._~+/-]{12,}`)

// Excerpt is the first 300 bytes of a body, with anything token-like redacted,
// for issue reports.
func Excerpt(body []byte) string {
	s := string(body[:min(len(body), 300)])
	return secretish.ReplaceAllString(s, "$1=<redacted>")
}

func (c *Client) logf(format string, args ...any) {
	if c.Debug != nil {
		fmt.Fprintf(c.Debug, "leroymerlin: debug: "+format+"\n", args...)
	}
}
