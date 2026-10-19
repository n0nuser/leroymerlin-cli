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

// Client talks to leroymerlin.es. It is safe for concurrent use, but requests
// are serialized through the rate gate on purpose.
type Client struct {
	BaseURL     string
	HTTP        *http.Client
	Session     *Session
	MinInterval time.Duration
	Debug       io.Writer // request log, cookies redacted; nil for none

	mu   sync.Mutex
	last time.Time
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

func (c *Client) get(ctx context.Context, path string, headers map[string]string) (*Page, error) {
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
	if h := c.Session.header(u); h != "" {
		req.Header.Set("Cookie", h)
	}
	start := time.Now()
	resp, err := c.HTTP.Do(req)
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
	c.Session.absorb(resp)
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, &Error{Kind: KindNetwork, Msg: fmt.Sprintf("reading %s: %v", u.Path, err), Endpoint: u.Path, Retryable: true}
	}
	c.logf("GET %s -> %d (%d bytes, %s)", u.Path, resp.StatusCode, len(body), time.Since(start).Round(time.Millisecond))
	p := &Page{URL: resp.Request.URL.String(), Status: resp.StatusCode, Header: resp.Header, Body: body}
	if err := classify(p, u.Path); err != nil {
		return nil, err
	}
	return p, nil
}

// wait enforces MinInterval (plus up to 30% jitter) between requests.
func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.MinInterval > 0 && !c.last.IsZero() {
		d := c.MinInterval + time.Duration(rand.Int64N(int64(c.MinInterval)*3/10+1))
		if sleep := time.Until(c.last.Add(d)); sleep > 0 {
			t := time.NewTimer(sleep)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
	}
	c.last = time.Now()
	return nil
}

// IsBlockPage reports whether a response is the bot-protection challenge
// rather than the page asked for. The status alone is not enough: a real 403
// from the origin is a different problem from a challenge.
func IsBlockPage(status int, h http.Header, body []byte) bool {
	if strings.EqualFold(h.Get("Server"), "DataDome") || h.Get("X-Datadome") != "" || h.Get("X-Datadome-Cid") != "" {
		return status == http.StatusForbidden || status == http.StatusTooManyRequests || bytes.Contains(body, []byte("captcha-delivery.com"))
	}
	return (status == http.StatusForbidden || status == http.StatusTooManyRequests) && bytes.Contains(body, []byte("captcha-delivery.com"))
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
