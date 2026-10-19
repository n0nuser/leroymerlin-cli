package leroymerlin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fastClient shrinks the retry backoff and disables pacing, so the retry
// tests do not sleep for real.
func fastClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	old := retryBase
	retryBase = time.Millisecond
	t.Cleanup(func() { retryBase = old })
	return &Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

// statusServer replays one status per request, repeating the last one.
func statusServer(t *testing.T, statuses ...int) (*atomic.Int64, *httptest.Server) {
	t.Helper()
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(n.Add(1)) - 1
		if i >= len(statuses) {
			i = len(statuses) - 1
		}
		switch statuses[i] {
		case http.StatusOK:
			w.Write([]byte("ok"))
		case http.StatusForbidden:
			w.Header().Set("X-Datadome", "protected")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("captcha-delivery.com"))
		default:
			w.WriteHeader(statuses[i])
		}
	}))
	t.Cleanup(srv.Close)
	return &n, srv
}

func TestRetryAfterHeaderParsing(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	page := func(v string) *Page {
		h := http.Header{}
		if v != "" {
			h.Set("Retry-After", v)
		}
		return &Page{Header: h}
	}
	cases := []struct {
		header string
		want   time.Duration
		ok     bool
	}{
		{"120", 2 * time.Minute, true},
		{"0", 0, true},
		{"-5", 0, true},
		{now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second, true},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0, true},
		{"not a date", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		got, ok := retryAfter(page(tc.header), now)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("retryAfter(%q) = %s, %v; want %s, %v", tc.header, got, ok, tc.want, tc.ok)
		}
	}
	if _, ok := retryAfter(nil, now); ok {
		t.Error("retryAfter(nil) reported a delay")
	}
}

// Regression: a 5xx was returned to the caller even though it is marked
// retryable; two attempts must be spent before the third try wins.
func TestRetriesServerErrorsThenSucceeds(t *testing.T) {
	n, srv := statusServer(t, http.StatusInternalServerError, http.StatusBadGateway, http.StatusOK)
	p, err := fastClient(t, srv).GetHTML(t.Context(), "/")
	if err != nil {
		t.Fatal(err)
	}
	if string(p.Body) != "ok" {
		t.Fatalf("body = %q", p.Body)
	}
	if got := n.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestServerErrorsExhaustTheRetries(t *testing.T) {
	n, srv := statusServer(t, http.StatusServiceUnavailable)
	_, err := fastClient(t, srv).GetHTML(t.Context(), "/")
	var le *Error
	if !errors.As(err, &le) || le.Kind != KindNetwork || le.Kind.ExitCode() != 6 {
		t.Fatalf("error = %v", err)
	}
	if got := n.Load(); got != maxAttempts {
		t.Fatalf("attempts = %d, want %d", got, maxAttempts)
	}
}

func TestRateLimitWithShortRetryAfterIsHonoured(t *testing.T) {
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	p, err := fastClient(t, srv).GetHTML(t.Context(), "/")
	if err != nil {
		t.Fatal(err)
	}
	if string(p.Body) != "ok" || n.Load() != 2 {
		t.Fatalf("body %q attempts %d", p.Body, n.Load())
	}
}

func TestRateLimitWithLongRetryAfterIsSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := fastClient(t, srv).GetHTML(t.Context(), "/")
	var le *Error
	if !errors.As(err, &le) || le.Kind != KindBlocked || le.Kind.ExitCode() != 5 {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(le.Msg, "10m0s") {
		t.Fatalf("message does not state the wait: %s", le.Msg)
	}
}

func TestRateLimitWithoutRetryAfterIsNotRetried(t *testing.T) {
	n, srv := statusServer(t, http.StatusTooManyRequests)
	if _, err := fastClient(t, srv).GetHTML(t.Context(), "/"); err == nil {
		t.Fatal("rate limit was accepted")
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

// Regression: a bot-protection 403 must keep exiting 5 after one request, not
// be treated as a transient failure.
func TestChallengeIsNotRetried(t *testing.T) {
	n, srv := statusServer(t, http.StatusForbidden)
	_, err := fastClient(t, srv).GetHTML(t.Context(), "/")
	var le *Error
	if !errors.As(err, &le) || le.Kind != KindBlocked || le.Kind.ExitCode() != 5 {
		t.Fatalf("error = %v", err)
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

type timeoutTransport struct{ n atomic.Int64 }

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func (t *timeoutTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.n.Add(1)
	return nil, timeoutErr{}
}

func TestTimeoutErrorsAreRetried(t *testing.T) {
	old := retryBase
	retryBase = time.Millisecond
	t.Cleanup(func() { retryBase = old })
	tr := &timeoutTransport{}
	c := &Client{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{Transport: tr}}
	_, err := c.GetHTML(context.Background(), "/")
	var le *Error
	if !errors.As(err, &le) || le.Kind != KindNetwork || !le.Retryable {
		t.Fatalf("error = %v", err)
	}
	if got := tr.n.Load(); got != maxAttempts {
		t.Fatalf("attempts = %d, want %d", got, maxAttempts)
	}
}
