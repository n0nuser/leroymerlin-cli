package harvest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"

	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
)

// The cookies the browser holds become the export the importer already reads:
// only leroymerlin.es cookies survive, the user agent travels, an expiry in
// seconds is parsed and a session cookie (-1) stays session-scoped.
func TestCookiesToExportRoundTrip(t *testing.T) {
	expiry := float64(time.Date(2027, 10, 19, 10, 0, 0, 0, time.UTC).Unix())
	raw, err := CookiesToExport([]*network.Cookie{
		{Name: "datadome", Value: "DD", Domain: ".leroymerlin.es", Path: "/", Expires: expiry, Secure: true, HTTPOnly: true},
		{Name: "store_id", Value: "37", Domain: "www.leroymerlin.es", Path: "/", Expires: -1},
		{Name: "tracker", Value: "x", Domain: ".example.com", Path: "/"},
		{Name: "empty", Value: "y", Domain: ""},
		nil,
	}, "UA/155")
	if err != nil {
		t.Fatal(err)
	}
	res, err := leroymerlin.ParseExport(raw, leroymerlin.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	if res.Session.UserAgent != "UA/155" {
		t.Fatalf("user agent %q", res.Session.UserAgent)
	}
	if got := len(res.Session.Cookies); got != 2 {
		t.Fatalf("kept %d cookies: %+v", got, res.Session.Cookies)
	}
	if res.Skipped != 2 { // the example.com cookie and the one with no domain
		t.Fatalf("skipped %d, want the example.com cookie and the domain-less one", res.Skipped)
	}
	dd, ok := res.Session.Cookie("datadome")
	if !ok || dd.Value != "DD" || dd.Expires.Unix() != int64(expiry) || !dd.HTTPOnly {
		t.Fatalf("datadome: %+v", dd)
	}
	st, _ := res.Session.Cookie("store_id")
	if !st.Expires.IsZero() {
		t.Fatalf("a session cookie got an expiry: %+v", st)
	}
}

func TestCookiesToExportForeignOnlyIsRefused(t *testing.T) {
	raw, err := CookiesToExport([]*network.Cookie{{Name: "x", Value: "y", Domain: ".example.com"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leroymerlin.ParseExport(raw, leroymerlin.FormatJSON); err == nil {
		t.Fatal("an export of only foreign cookies was accepted")
	}
}

func TestHostMatches(t *testing.T) {
	for d, want := range map[string]bool{
		"leroymerlin.es": true, ".leroymerlin.es": true, "www.leroymerlin.es": true,
		"leroymerlin.es.evil.example": false, "evilleroymerlin.es": false, "leroymerlin.fr": false,
	} {
		if hostMatches(d) != want {
			t.Errorf("hostMatches(%q) = %v", d, !want)
		}
	}
}

func TestHasCookie(t *testing.T) {
	cookies := []*network.Cookie{
		{Name: "datadome", Domain: "example.com"},
		{Name: "datadome", Domain: "www.leroymerlin.es"},
	}
	if !hasCookie(cookies, "datadome") {
		t.Fatal("the leroymerlin datadome cookie was not seen")
	}
	if hasCookie([]*network.Cookie{{Name: "datadome", Domain: ".example.com"}}, "datadome") {
		t.Fatal("a foreign datadome cookie counted")
	}
}

func TestFindChromePrefersTheOverride(t *testing.T) {
	look := func(name string) (string, error) {
		if name == "chromium" || name == "custom-chrome" {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	if p, err := FindChrome("custom-chrome", look); err != nil || p != "/usr/bin/custom-chrome" {
		t.Fatalf("override: %q %v", p, err)
	}
	if p, err := FindChrome("", look); err != nil || p != "/usr/bin/chromium" {
		t.Fatalf("search: %q %v", p, err)
	}
}

func TestFindChromeAcceptsAPath(t *testing.T) {
	path := t.TempDir() + "/chrome"
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if p, err := FindChrome(path, func(string) (string, error) { return "", errors.New("nope") }); err != nil || p != path {
		t.Fatalf("path override: %q %v", p, err)
	}
}

func TestFindChromeRejectsAMissingOverride(t *testing.T) {
	if _, err := FindChrome("/nope/chrome", func(string) (string, error) { return "", errors.New("nope") }); err == nil {
		t.Fatal("a missing override was accepted")
	}
}

func TestDebugURLReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(`{"Browser":"Chrome/155"}`))
	}))
	defer srv.Close()
	if !DebugURLReachable(context.Background(), srv.URL, srv.Client()) {
		t.Fatal("a live devtools endpoint was not seen")
	}
	if DebugURLReachable(context.Background(), "http://127.0.0.1:1", &http.Client{Timeout: 300 * time.Millisecond}) {
		t.Fatal("a dead endpoint was seen as live")
	}
}

// The clear-page check must look for the challenge, not accept its title.
func TestChallengeCheckMentionsTheCaptcha(t *testing.T) {
	if !strings.Contains(clearJS, "captcha-delivery") || !strings.Contains(clearJS, "leroymerlin.es") {
		t.Fatalf("clearJS = %s", clearJS)
	}
	if !strings.Contains(clearJS, "enable js") {
		t.Fatal("clearJS does not reject the JavaScript wall")
	}
}
