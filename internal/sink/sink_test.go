package sink

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n0nuser/leroymerlin-cli/internal/config"
	"github.com/n0nuser/leroymerlin-cli/internal/store"
)

func event(kind string) store.Event {
	return store.Event{
		Type: kind, Watch: "cola", Profile: "default",
		Product: json.RawMessage(`{"ref":"81866477","name":"Cemento cola 25 kg","url":"https://x/81866477","price":{"amount":3.94,"currency":"EUR"}}`),
		Change:  json.RawMessage(`{"from":5,"to":3.94}`),
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.SinkConfig
		ok   bool
	}{
		{"ntfy", config.SinkConfig{Type: "ntfy", URL: "https://ntfy.sh", Topic: "t"}, true},
		{"ntfy no topic", config.SinkConfig{Type: "ntfy", URL: "https://ntfy.sh"}, false},
		{"ntfy no url", config.SinkConfig{Type: "ntfy", Topic: "t"}, false},
		{"webhook", config.SinkConfig{Type: "webhook", URL: "https://x/hook"}, true},
		{"webhook no url", config.SinkConfig{Type: "webhook"}, false},
		{"exec", config.SinkConfig{Type: "exec", Command: []string{"true"}}, true},
		{"exec no command", config.SinkConfig{Type: "exec"}, false},
		{"unknown", config.SinkConfig{Type: "carrier-pigeon"}, false},
		{"empty", config.SinkConfig{}, false},
	}
	for _, tc := range cases {
		err := Validate(tc.name, tc.cfg)
		if tc.ok && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
}

func TestDeliverUnknownType(t *testing.T) {
	if err := Deliver(context.Background(), "x", config.SinkConfig{Type: "nope"}, event("product.new")); err == nil {
		t.Fatal("unknown type accepted")
	}
}

func TestNtfyDeliversTitleBodyAndToken(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	os.WriteFile(tokenFile, []byte("  secret-token \n"), 0o600)
	var got *http.Request
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
	}))
	defer srv.Close()
	cfg := config.SinkConfig{Type: "ntfy", URL: srv.URL + "/", Topic: "leroymerlin", TokenFile: tokenFile}
	if err := Deliver(context.Background(), "shop", cfg, event("product.price_changed")); err != nil {
		t.Fatal(err)
	}
	if got.Method != http.MethodPost || got.URL.Path != "/leroymerlin" {
		t.Fatalf("request %s %s", got.Method, got.URL.Path)
	}
	if got.Header.Get("Authorization") != "Bearer secret-token" {
		t.Fatalf("authorization %q", got.Header.Get("Authorization"))
	}
	if !strings.Contains(got.Header.Get("Title"), "Precio") {
		t.Fatalf("title %q", got.Header.Get("Title"))
	}
	if body != "5 -> 3.94" {
		t.Fatalf("body %q", body)
	}
}

func TestNtfyServerErrorIsReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg := config.SinkConfig{Type: "ntfy", URL: srv.URL, Topic: "t"}
	if err := Deliver(context.Background(), "shop", cfg, event("product.new")); err == nil {
		t.Fatal("an HTTP 500 was accepted")
	}
}

func TestWebhookPostsTheEventJSON(t *testing.T) {
	var got store.Event
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("body: %v", err)
		}
	}))
	defer srv.Close()
	if err := Deliver(context.Background(), "hook", config.SinkConfig{Type: "webhook", URL: srv.URL}, event("product.in_stock")); err != nil {
		t.Fatal(err)
	}
	if contentType != "application/json" {
		t.Fatalf("content-type %q", contentType)
	}
	if got.Type != "product.in_stock" || got.Watch != "cola" {
		t.Fatalf("event %+v", got)
	}
}

func TestWebhookServerErrorIsReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	if err := Deliver(context.Background(), "hook", config.SinkConfig{Type: "webhook", URL: srv.URL}, event("product.new")); err == nil {
		t.Fatal("an HTTP 502 was accepted")
	}
}

func TestExecSinkReceivesTheEventOnStdin(t *testing.T) {
	out := filepath.Join(t.TempDir(), "event.json")
	script := filepath.Join(t.TempDir(), "sink.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat > \""+out+"\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.SinkConfig{Type: "exec", Command: []string{script}}
	if err := Deliver(context.Background(), "script", cfg, event("product.new")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"type":"product.new"`) {
		t.Fatalf("stdin payload %s", raw)
	}
}

func TestExecSinkReturnsStderrOnFailure(t *testing.T) {
	fail := filepath.Join(t.TempDir(), "fail.sh")
	os.WriteFile(fail, []byte("#!/bin/sh\necho boom >&2\nexit 3\n"), 0o700)
	err := Deliver(context.Background(), "script", config.SinkConfig{Type: "exec", Command: []string{fail}}, event("product.new"))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v", err)
	}
}

func TestSummary(t *testing.T) {
	cases := map[string]string{
		"product.new":           "Nuevo",
		"product.price_changed": "Precio",
		"product.in_stock":      "Disponible",
		"product.out_of_stock":  "Agotado",
		"product.stock_changed": "Stock",
		"product.removed":       "Fuera del resultado",
		"something.else":        "something.else",
	}
	for kind, want := range cases {
		title, body, link := Summary(event(kind))
		if !strings.Contains(title, want) {
			t.Errorf("%s: title %q", kind, title)
		}
		if kind != "product.removed" && body == "" && kind != "something.else" {
			t.Errorf("%s: empty body", kind)
		}
		if kind == "product.new" && link != "https://x/81866477" {
			t.Errorf("%s: link %q", kind, link)
		}
	}
	// An event with no product payload and no price must not panic.
	title, _, _ := Summary(store.Event{Type: "product.new"})
	if title != "Nuevo: " {
		t.Fatalf("empty product title %q", title)
	}
}

func TestToken(t *testing.T) {
	if tok, err := token(""); err != nil || tok != "" {
		t.Fatalf("empty token = %q %v", tok, err)
	}
	if _, err := token(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing token file accepted")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.WriteFile(filepath.Join(home, "tok"), []byte("abc\n"), 0o600)
	if tok, err := token("~/tok"); err != nil || tok != "abc" {
		t.Fatalf("home token = %q %v", tok, err)
	}
}
