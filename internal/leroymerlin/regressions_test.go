package leroymerlin

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Regression: a Set-Cookie on a same-site redirect was dropped, because the
// Cookie header was set once by hand and net/http resent it unchanged.
func TestRedirectKeepsRotatedCookie(t *testing.T) {
	var second string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.SetCookie(w, &http.Cookie{Name: "datadome", Value: "HOP", Path: "/"})
			http.Redirect(w, r, "/new", http.StatusFound)
			return
		}
		second = r.Header.Get("Cookie")
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	// Resolve www.leroymerlin.es to the test server, so host-only cookies
	// have the real domain.
	tr := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}}
	c := &Client{
		BaseURL: "http://www." + Domain,
		HTTP:    &http.Client{Transport: tr, CheckRedirect: sameSite},
		Session: &Session{Cookies: []Cookie{{Name: "datadome", Value: "OLD", Domain: "www." + Domain, Path: "/"}}},
	}
	if _, err := c.GetHTML(t.Context(), "/old"); err != nil {
		t.Fatal(err)
	}
	if second != "datadome=HOP" {
		t.Fatalf("second hop sent %q, want the rotated cookie", second)
	}
	if dd, _ := c.Session.Cookie("datadome"); dd.Value != "HOP" || !c.Session.Dirty() {
		t.Fatalf("session = %+v dirty=%v", dd, c.Session.Dirty())
	}
}

// Regression: with HTTPS_PROXY set, net/http tunnels through the proxy and
// does its own TLS, so the Chrome hello was silently skipped.
func TestChromeTransportIgnoresProxyEnv(t *testing.T) {
	if ChromeTransport(nil).Proxy != nil {
		t.Fatal("the storefront transport must not use a proxy")
	}
}

// Regression: a protected 200 page that carries the vendor header (or names
// its captcha host) was classified as a challenge.
func TestNormalPageWithVendorHeaderIsNotBlocked(t *testing.T) {
	h := http.Header{"X-Datadome": {"protected"}}
	if IsBlockPage(200, h, []byte(`<script src="https://js.captcha-delivery.com/x.js"></script>`)) {
		t.Fatal("a 200 page is never a challenge")
	}
}

// Regression: stores without an image title were listed under their slug,
// lowercase, and sorted after every real name.
func TestUntitledStoreGetsReadableName(t *testing.T) {
	raw, _ := os.ReadFile("testdata/sitemap_store1.xml")
	stores, err := ParseStores(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stores {
		if s.Slug == "santiago-jardin" && s.Name != "Santiago Jardin" {
			t.Fatalf("santiago-jardin named %q", s.Name)
		}
	}
}

// Regression: the redaction pattern missed JSON keys ("session_token":"…").
func TestExcerptRedactsJSONTokens(t *testing.T) {
	out := Excerpt([]byte(`{"session_token":"abcdefghijklmnopqrstuvwxyz","datadome":"0123456789abcdef"}`))
	if strings.Contains(out, "abcdefghijklmnop") || strings.Contains(out, "0123456789abcdef") {
		t.Fatalf("token leaked: %s", out)
	}
}

func TestClientHintsFollowTheSession(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Clone() }))
	defer srv.Close()
	res, err := ParseExport(readFixture(t, "session.synthetic.har"), FormatAuto)
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client(), Session: &res.Session}
	c.GetHTML(t.Context(), "/")
	if !strings.Contains(got.Get("Sec-Ch-Ua"), `v="141"`) || got.Get("Sec-Ch-Ua-Platform") != `"Linux"` {
		t.Fatalf("hints from the HAR not sent: %v", got)
	}
	c.Session = &Session{UserAgent: "Custom/1"}
	c.GetHTML(t.Context(), "/")
	if got.Get("Sec-Ch-Ua") != "" {
		t.Fatal("default hints sent beside a custom user agent")
	}
	c.Session = nil
	c.GetHTML(t.Context(), "/")
	if !strings.Contains(got.Get("Sec-Ch-Ua"), `v="133"`) {
		t.Fatal("default user agent without matching hints")
	}
}
