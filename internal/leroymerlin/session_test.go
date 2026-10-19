package leroymerlin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func names(s Session) string {
	var n []string
	for _, c := range s.Cookies {
		n = append(n, c.Name)
	}
	return strings.Join(n, ",")
}

// All *.synthetic.* fixtures are hand-written: real exports carry live tokens.
func TestParseExportFormats(t *testing.T) {
	cases := []struct {
		file, format string
	}{
		{"cookies_editor.synthetic.json", FormatJSON},
		{"cookies_browser.synthetic.json", FormatJSON},
		{"cookies.synthetic.txt", FormatNetscape},
		{"session.synthetic.har", FormatHAR},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			res, err := ParseExport(readFixture(t, tc.file), FormatAuto)
			if err != nil {
				t.Fatal(err)
			}
			if res.Format != tc.format {
				t.Fatalf("detected %s, want %s", res.Format, tc.format)
			}
			if got := names(res.Session); got != "datadome,lm-store" {
				t.Fatalf("cookies %s, want datadome,lm-store (other domains dropped)", got)
			}
			if res.Session.Dirty() {
				t.Fatal("a fresh import is not dirty")
			}
			roundTrip(t, res.Session.Cookies)
		})
	}
}

// Regression: a {"userAgent","cookies"} export imported the cookies but
// dropped the user agent, so the client replayed Chrome 155 cookies as the
// default Chrome 133 and the pair did not match.
func TestParseExportBrowserWrapperKeepsUserAgent(t *testing.T) {
	res, err := ParseExport(readFixture(t, "cookies_browser.synthetic.json"), FormatAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Session.UserAgent, "Chrome/155") {
		t.Fatalf("user agent %q not taken from the export", res.Session.UserAgent)
	}
	if got := names(res.Session); got != "datadome,lm-store" {
		t.Fatalf("cookies %s, want datadome,lm-store (other domains dropped)", got)
	}
	if res.Skipped != 1 {
		t.Fatalf("skipped %d, want the one cookie for another site", res.Skipped)
	}
}

func TestParseExportHARKeepsRotatedCookieAndUA(t *testing.T) {
	res, err := ParseExport(readFixture(t, "session.synthetic.har"), FormatAuto)
	if err != nil {
		t.Fatal(err)
	}
	dd, _ := res.Session.Cookie("datadome")
	if dd.Value != "ROTATED_DD" {
		t.Fatalf("datadome = %q, want the value the last response set", dd.Value)
	}
	if !strings.Contains(res.Session.UserAgent, "Chrome/141") {
		t.Fatalf("user agent %q not taken from the HAR", res.Session.UserAgent)
	}
}

func TestParseExportNetscapeExpiryAndHTTPOnly(t *testing.T) {
	res, err := ParseExport(readFixture(t, "cookies.synthetic.txt"), FormatAuto)
	if err != nil {
		t.Fatal(err)
	}
	dd, _ := res.Session.Cookie("datadome")
	if dd.Expires.Unix() != 1823896471 || !dd.Secure {
		t.Fatalf("datadome = %+v", dd)
	}
	st, _ := res.Session.Cookie("lm-store")
	if !st.HTTPOnly || !st.Expires.IsZero() {
		t.Fatalf("lm-store = %+v", st)
	}
}

func TestParseExportHeader(t *testing.T) {
	for _, in := range []string{"Cookie: datadome=a; lm-store=b", "datadome=a; lm-store=b"} {
		res, err := ParseExport([]byte(in), FormatAuto)
		if err != nil {
			t.Fatal(err)
		}
		if res.Format != FormatHeader || names(res.Session) != "datadome,lm-store" {
			t.Fatalf("%q -> %s %s", in, res.Format, names(res.Session))
		}
	}
}

func TestParseExportRejects(t *testing.T) {
	for name, in := range map[string]string{
		"empty":          "",
		"other domains":  `[{"name":"x","value":"y","domain":".example.com"}]`,
		"har no cookies": `{"log":{"entries":[{"request":{"url":"https://www.leroymerlin.es/","headers":[]},"response":{}}]}}`,
		"broken json":    `[{"name":`,
	} {
		if _, err := ParseExport([]byte(in), FormatAuto); err == nil {
			t.Errorf("%s: accepted", name)
		} else if e, ok := err.(*Error); !ok || e.Kind != KindUsage {
			t.Errorf("%s: %v is not a usage error", name, err)
		}
	}
}

// A lookalike domain must not receive or supply cookies.
func TestOwnDomain(t *testing.T) {
	for d, want := range map[string]bool{
		"leroymerlin.es": true, ".leroymerlin.es": true, "www.leroymerlin.es": true,
		"leroymerlin.es.evil.example": false, "evilleroymerlin.es": false, "leroymerlin.fr": false,
	} {
		if ownDomain(d) != want {
			t.Errorf("ownDomain(%q) = %v", d, !want)
		}
	}
}

func TestSessionHeaderMatching(t *testing.T) {
	s := &Session{Cookies: []Cookie{
		{Name: "a", Value: "1", Domain: ".leroymerlin.es", Path: "/"},
		{Name: "b", Value: "2", Domain: "www.leroymerlin.es", Path: "/productos"},
		{Name: "c", Value: "3", Domain: ".leroymerlin.es", Expires: time.Now().Add(-time.Hour)},
		{Name: "d", Value: "4", Domain: ".leroymerlin.es", Secure: true},
	}}
	u, _ := url.Parse("https://www.leroymerlin.es/productos/x.html")
	if got := s.header(u); got != "a=1; b=2; d=4" {
		t.Fatalf("header = %q", got)
	}
	u, _ = url.Parse("http://www.leroymerlin.es/")
	if got := s.header(u); got != "a=1" {
		t.Fatalf("plain-http header = %q, want secure cookies withheld", got)
	}
}

func TestClientSendsSessionAndAbsorbsRotation(t *testing.T) {
	var gotCookie, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie, gotUA = r.Header.Get("Cookie"), r.Header.Get("User-Agent")
		http.SetCookie(w, &http.Cookie{Name: "datadome", Value: "NEW", Domain: ".leroymerlin.es", Path: "/", MaxAge: 3600})
		w.Write([]byte("<html>ok</html>"))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	// The test server is 127.0.0.1, so give the session a cookie for it too.
	c.Session = &Session{UserAgent: "UA/1", Cookies: []Cookie{{Name: "datadome", Value: "OLD", Domain: "127.0.0.1", Path: "/"}}}
	if _, err := c.GetHTML(t.Context(), "/"); err != nil {
		t.Fatal(err)
	}
	if gotCookie != "datadome=OLD" || gotUA != "UA/1" {
		t.Fatalf("sent Cookie %q UA %q", gotCookie, gotUA)
	}
	if !c.Session.Dirty() {
		t.Fatal("a Set-Cookie for leroymerlin.es must mark the session dirty")
	}
	var rotated bool
	for _, ck := range c.Session.Cookies {
		rotated = rotated || (ck.Domain == ".leroymerlin.es" && ck.Value == "NEW")
	}
	if !rotated {
		t.Fatalf("rotated cookie not stored: %v", c.Session.Cookies)
	}
}

func TestClientIgnoresForeignSetCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "x", Value: "y", Domain: "example.com"})
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client(), Session: &Session{}}
	if _, err := c.GetHTML(t.Context(), "/"); err != nil {
		t.Fatal(err)
	}
	if len(c.Session.Cookies) != 0 || c.Session.Dirty() {
		t.Fatalf("foreign cookie absorbed: %+v", c.Session.Cookies)
	}
}

func TestClientMapsStatuses(t *testing.T) {
	block := readFixture(t, "blocked_datadome.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/blocked":
			w.Header().Set("X-Datadome", "protected")
			w.WriteHeader(403)
			w.Write(block)
		case "/missing":
			w.WriteHeader(404)
		case "/teapot":
			w.WriteHeader(418)
			w.Write([]byte(`{"session_token":"abcdefghijklmnopqrstuvwxyz"}`))
		}
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	for path, want := range map[string]Kind{"/blocked": KindBlocked, "/missing": KindNotFound, "/teapot": KindAPIChanged} {
		_, err := c.GetHTML(t.Context(), path)
		e, ok := err.(*Error)
		if !ok || e.Kind != want {
			t.Fatalf("%s: %v, want %v", path, err, want)
		}
		if strings.Contains(e.Body, "abcdefghijklmnop") {
			t.Fatalf("%s: excerpt leaks a token: %s", path, e.Body)
		}
	}
}

func TestRateGateSpacesRequests(t *testing.T) {
	c := &Client{MinInterval: 40 * time.Millisecond}
	start := time.Now()
	for range 3 {
		if err := c.wait(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d < 80*time.Millisecond {
		t.Fatalf("3 requests took %s, want at least 2 intervals", d)
	}
}
