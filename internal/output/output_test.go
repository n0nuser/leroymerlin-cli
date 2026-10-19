package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
)

// Remote text (store and product names) must not drive the terminal.
func TestTableStripsControlSequences(t *testing.T) {
	var b bytes.Buffer
	Table(&b, false, nil, [][]string{{"evil\x1b]52;c;cGF3bmVk\x07name\u009b31m", "ok\nline"}})
	out := b.String()
	if strings.ContainsAny(out, "\x1b\x07\u009b") {
		t.Fatalf("control bytes reached the terminal: %q", out)
	}
	if !strings.Contains(out, "ok line") {
		t.Fatalf("newline not flattened: %q", out)
	}
}

func TestJSONLSplitsArrays(t *testing.T) {
	var b bytes.Buffer
	Printer{W: &b, Format: JSONL}.Print([]map[string]int{{"a": 1}, {"a": 2}})
	if b.String() != "{\"a\":1}\n{\"a\":2}\n" {
		t.Fatalf("%q", b.String())
	}
}

type prettyThing struct{ N int }

func (p prettyThing) Pretty(w io.Writer, color bool) {
	fmt.Fprintf(w, "pretty:%d color:%v\n", p.N, color)
}

func TestParseFormat(t *testing.T) {
	for _, ok := range []string{"json", "jsonl", "pretty"} {
		if f, err := ParseFormat(ok); err != nil || string(f) != ok {
			t.Errorf("ParseFormat(%q) = %q, %v", ok, f, err)
		}
	}
	if _, err := ParseFormat("xml"); err == nil {
		t.Fatal("unknown format accepted")
	}
}

func TestPrinterRendersEveryFormat(t *testing.T) {
	var b bytes.Buffer
	if err := (Printer{W: &b, Format: JSON}).Print(map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "\"a\": 1") {
		t.Fatalf("json: %q", b.String())
	}

	b.Reset()
	if err := (Printer{W: &b, Format: JSONL}).Print(map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if b.String() != "{\"a\":1}\n" {
		t.Fatalf("jsonl scalar: %q", b.String())
	}

	// Pretty uses the value's own rendering when it has one...
	b.Reset()
	if err := (Printer{W: &b, Format: Pretty, Color: true}).Print(prettyThing{N: 7}); err != nil {
		t.Fatal(err)
	}
	if b.String() != "pretty:7 color:true\n" {
		t.Fatalf("pretty: %q", b.String())
	}
	// ...and falls back to indented json when it does not.
	b.Reset()
	if err := (Printer{W: &b, Format: Pretty}).Print(map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "\"a\": 1") {
		t.Fatalf("pretty fallback: %q", b.String())
	}
	// A value json cannot marshal is an error, not a panic.
	b.Reset()
	if err := (Printer{W: &b, Format: JSON}).Print(make(chan int)); err == nil {
		t.Fatal("unmarshalable value accepted")
	}
}

func TestCleanTruncateAndDim(t *testing.T) {
	if got := Clean("a\nb\tc\x00d"); got != "a b cd" {
		t.Fatalf("Clean = %q", got)
	}
	if got := Truncate("abcdef", 4); got != "abc…" {
		t.Fatalf("Truncate = %q", got)
	}
	if got := Truncate("abc", 4); got != "abc" {
		t.Fatalf("short Truncate = %q", got)
	}
	if Dim("x", false) != "x" || !strings.Contains(Dim("x", true), "\x1b[2m") {
		t.Fatal("Dim did not honour colour")
	}
}

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		err      error
		code     int
		category string
	}{
		{Usagef("bad flag"), 2, "usage"},
		{&leroymerlin.Error{Kind: leroymerlin.KindAuth, Msg: "no session"}, 3, "auth"},
		{&leroymerlin.Error{Kind: leroymerlin.KindBlocked, Status: 403, Msg: "challenge"}, 5, "blocked"},
		{&leroymerlin.Error{Kind: leroymerlin.KindNetwork, Status: 503, Retryable: true, Msg: "down"}, 6, "network"},
		{&leroymerlin.Error{Kind: leroymerlin.KindAPIChanged, Status: 418, Endpoint: "/search", Body: "x", Msg: "changed"}, 7, "api_changed"},
		{errors.New("plain"), 1, "generic"},
	} {
		env := Classify(tc.err, "1.2.3", "leroymerlin search")
		if env.Code != tc.code || env.Category != tc.category {
			t.Errorf("Classify(%v) = %d/%s, want %d/%s", tc.err, env.Code, env.Category, tc.code, tc.category)
		}
	}
	blocked := Classify(&leroymerlin.Error{Kind: leroymerlin.KindBlocked, Msg: "x"}, "1", "c")
	if len(blocked.SuggestedCommands) == 0 {
		t.Fatal("blocked error has no suggestions")
	}
	changed := Classify(&leroymerlin.Error{Kind: leroymerlin.KindAPIChanged, Endpoint: "/p", Msg: "x", Body: "secret=datadome=abcdefghijklmnopqrstuvwxyz"}, "1", "c")
	if changed.IssueURL == "" || !strings.Contains(changed.IssueURL, "template=api-change.yml") {
		t.Fatalf("issue url %q", changed.IssueURL)
	}
}

func TestIssueURLNeverCarriesArguments(t *testing.T) {
	u := IssueURL("v1", "leroymerlin search", &leroymerlin.Error{Endpoint: "/search", Status: 418, Body: "..."})
	if strings.Contains(u, "cemento") || !strings.Contains(u, "command=leroymerlin+search") {
		t.Fatalf("issue url %q", u)
	}
}

func TestPrintError(t *testing.T) {
	var b bytes.Buffer
	if code := PrintError(&b, Usagef("nope"), ErrText, "1", "leroymerlin"); code != 2 || !strings.Contains(b.String(), "nope") {
		t.Fatalf("text: %d %q", code, b.String())
	}
	b.Reset()
	if code := PrintError(&b, &leroymerlin.Error{Kind: leroymerlin.KindAPIChanged, Endpoint: "/x", Msg: "changed"}, ErrText, "1", "leroymerlin"); code != 7 || !strings.Contains(b.String(), "issues/new") {
		t.Fatalf("issue: %d %q", code, b.String())
	}
	b.Reset()
	if code := PrintError(&b, &leroymerlin.Error{Kind: leroymerlin.KindBlocked, Msg: "challenged"}, ErrJSON, "1", "leroymerlin"); code != 5 {
		t.Fatalf("json: %d", code)
	}
	var env Envelope
	if err := json.Unmarshal(b.Bytes(), &env); err != nil || env.Code != 5 || env.Category != "blocked" {
		t.Fatalf("envelope %q (%v)", b.String(), err)
	}
}

func TestColourAndTerminal(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	if !IsTerminal(devNull) {
		t.Fatal("/dev/null is a character device on this platform")
	}
	if IsTerminal(nil) {
		t.Fatal("nil file counted as a terminal")
	}
	t.Setenv("NO_COLOR", "1")
	if ColorEnabled(false, devNull) {
		t.Fatal("NO_COLOR ignored")
	}
	t.Setenv("NO_COLOR", "")
	if ColorEnabled(true, devNull) {
		t.Fatal("--no-color ignored")
	}
	t.Setenv("TERM", "dumb")
	if ColorEnabled(false, devNull) {
		t.Fatal("TERM=dumb ignored")
	}
}
