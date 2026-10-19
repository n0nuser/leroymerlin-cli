package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
	"github.com/n0nuser/leroymerlin-cli/internal/store"
)

func TestPrettyRendersEveryCommand(t *testing.T) {
	e := newEnv(t)
	t.Setenv("HOME", e.home) // mcp install must not touch the real home
	e.login()
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"auth", "status", "--format", "pretty"}, "profile"},
		{[]string{"store", "list", "--format", "pretty"}, "stores"},
		{[]string{"store", "list", "--query", "madrid", "--compact", "--format", "pretty"}, "SLUG"},
		{[]string{"category", "list", "--parent", "construccion", "--depth", "1", "--format", "pretty"}, "PATH"},
		{[]string{"category", "list", "--query", "cemento", "--format", "pretty"}, "PATH"},
		{[]string{"doctor", "--format", "pretty"}, "storefront"},
		{[]string{"search", "cemento cola", "--format", "pretty"}, "REF"},
		{[]string{"search", "--category", "construccion/cementos-morteros-y-yesos/cementos", "--format", "pretty"}, "REF"},
		{[]string{"product", "show", "81866477", "--format", "pretty"}, "ref"},
		{[]string{"product", "stock", "81866477", "--format", "pretty"}, "online"},
		{[]string{"watch", "add", "search", "cemento cola", "--name", "cola", "--format", "pretty"}, "watch cola"},
		{[]string{"watch", "list", "--format", "pretty"}, "WATCH"},
		{[]string{"watch", "check", "--all", "--format", "pretty"}, "no changes"},
		{[]string{"watch", "events", "--format", "pretty"}, "no changes"},
		{[]string{"profile", "list", "--format", "pretty"}, "default"},
		{[]string{"skills", "list", "--format", "pretty"}, "leroymerlin-usage"},
		{[]string{"config", "path", "--format", "pretty"}, "config"},
		{[]string{"sink", "list", "--format", "pretty"}, "[]"},
		{[]string{"mcp", "install", "claude-code", "--format", "pretty"}, "added"},
	}
	for _, tc := range cases {
		out, errOut, code := e.run(tc.args...)
		if code != 0 {
			t.Errorf("%v exit %d: %s", tc.args, code, errOut)
			continue
		}
		if !strings.Contains(out, tc.want) {
			t.Errorf("%v output lacks %q:\n%s", tc.args, tc.want, out)
		}
	}
}

// A watched product that goes out of stock produces an event, and the pretty
// renderer draws it.
func TestWatchCheckRendersAnEvent(t *testing.T) {
	e := newEnv(t)
	e.login()
	if _, errOut, code := e.run("watch", "add", "product", "81866477", "--name", "pegoland"); code != 0 {
		t.Fatalf("add exit %d: %s", code, errOut)
	}
	e.altProduct = true
	out, errOut, code := e.run("watch", "check", "--all", "--format", "pretty")
	if code != 0 {
		t.Fatalf("check exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "product.out_of_stock") || !strings.Contains(out, "81866477") {
		t.Fatalf("event not rendered:\n%s", out)
	}
	out, _, code = e.run("watch", "events", "--format", "jsonl")
	if code != 0 || !strings.Contains(out, "product.out_of_stock") {
		t.Fatalf("events: %d %s", code, out)
	}
}

func TestPrettyDirectViews(t *testing.T) {
	var b bytes.Buffer
	storeRefView{}.Pretty(&b, false)
	storeList{{Slug: "madrid", Name: "Madrid", Compact: true}}.Pretty(&b, false)
	categoryList{{Path: "construccion", Name: "Construcción", Depth: 1}}.Pretty(&b, false)

	price := &leroymerlin.Money{Amount: 3.94, Currency: "EUR"}
	searchResultView{
		SearchResult: leroymerlin.SearchResult{Total: 737, Page: 1, Pages: 10, Sort: "relevance",
			Products: []leroymerlin.Product{{Ref: "1", Name: "Uno", Brand: "AXTON", Price: price, Available: true, URL: "https://x/1"}}},
		Shown: 1,
	}.Pretty(&b, true)

	productView{leroymerlin.ProductPage{
		Ref: "1", Name: "Uno", Brand: "AXTON", Price: price, Available: true, Seller: "Leroy Merlin", URL: "https://x/1",
		Deliveries: []leroymerlin.Delivery{
			{Type: "storeDelivery", Stock: 3, Status: "ONSITE", Time: "1 day", Price: &leroymerlin.Money{}},
			{Type: "homeDelivery", Stock: 1, Status: "ONORDER", Time: "5 days", Price: price},
			{Type: "weird", Status: "OTHER"},
		},
	}}.Pretty(&b, false)
	productView{leroymerlin.ProductPage{Ref: "2", Store: &leroymerlin.StoreRef{ID: "037", Name: "Salamanca"}}}.Pretty(&b, false)

	stockView{Ref: "1", Name: "Uno", Store: &leroymerlin.StoreRef{ID: "037", Name: "Salamanca"}, Online: true,
		Deliveries: []leroymerlin.Delivery{{Type: "storeDelivery", Stock: 0, Status: "OUTOFSTOCK", Price: price}}}.Pretty(&b, false)
	stockView{Ref: "2"}.Pretty(&b, false)

	watchView{Watch: store.Watch{Name: "cola", Kind: store.KindSearch, Target: []byte(`{"query":{"keywords":"cemento"}}`)}}.Pretty(&b, false)
	watchView{Watch: store.Watch{Name: "cat", Kind: store.KindSearch, Target: []byte(`{"query":{"category":"construccion"}}`)}}.Pretty(&b, false)
	watchView{Watch: store.Watch{Name: "prod", Kind: store.KindProduct, Target: []byte(`{"ref":"1"}`)}}.Pretty(&b, false)
	watchView{Watch: store.Watch{Name: "broken", Kind: "weird", Target: []byte(`{`)}}.Pretty(&b, false)
	watchList{
		{Name: "a", Kind: store.KindSearch, Target: []byte(`{"query":{"keywords":"x"}}`), Sinks: []string{"shop"}, Interval: time.Minute},
		{Name: "b", Kind: store.KindProduct, Target: []byte(`{"ref":"1"}`), LastCheckAt: time.Now()},
	}.Pretty(&b, false)

	eventList{}.Pretty(&b, false)
	eventList{{Type: "product.price_changed", Watch: "cola", At: time.Now(),
		Product: []byte(`{"ref":"1","name":"Uno","price":{"amount":3.94,"currency":"EUR"}}`),
		Change:  []byte(`{"from":5,"to":3.94}`)}}.Pretty(&b, false)
	eventList{{Type: "product.removed", Watch: "cola", At: time.Now()}}.Pretty(&b, false)

	harvestResult{Profile: "default", Path: "/tmp/s.json", Format: "chrome", Skipped: 2, Source: "profile", ChromeProfile: "/home/u/.config/google-chrome",
		Session: leroymerlin.Summary{Cookies: 13, HasDataDome: true, UserAgent: "UA"}}.Pretty(&b, false)
	harvestResult{Source: "fresh", Session: leroymerlin.Summary{Cookies: 1}}.Pretty(&b, false)
	harvestResult{Source: "attach", Session: leroymerlin.Summary{Cookies: 1, HasDataDome: true}}.Pretty(&b, false)

	doctorReport{OK: true, Profile: "default", Checks: []doctorCheck{{Name: "config", OK: true, Detail: "/tmp/c"}, {Name: "session", OK: false, Detail: "none"}}}.Pretty(&b, false)

	loginResult{Profile: "default", Path: "/tmp/s.json", Format: "json", Skipped: 1,
		Session: leroymerlin.Summary{Cookies: 2, HasDataDome: true}, Checked: true, Valid: boolPtr(true)}.Pretty(&b, false)
	falseVal := false
	loginResult{Checked: true, Valid: &falseVal, CheckError: "refused", Session: leroymerlin.Summary{}}.Pretty(&b, false)
	loginResult{Checked: false, Session: leroymerlin.Summary{HasDataDome: false}}.Pretty(&b, false)

	statusResult{Profile: "default"}.Pretty(&b, false)
	statusResult{Profile: "default", Source: "file", Path: "/tmp/s.json", Format: "json", ImportedAt: timePtr(time.Now()),
		Session: &leroymerlin.Summary{Cookies: 2, Names: []string{"datadome"}, HasDataDome: true, UserAgent: "UA"},
		Checked: true, Valid: boolPtr(false), CheckError: "nope"}.Pretty(&b, false)

	profileList{{Name: "default", Default: true}, {Name: "madrid"}}.Pretty(&b, false)

	installView{Harness: "codex", Path: "/tmp/config.toml", Action: "added", Command: "/bin/lm", Args: []string{"mcp"}}.Pretty(&b, false)

	if b.Len() == 0 {
		t.Fatal("pretty renderers wrote nothing")
	}
}

func boolPtr(b bool) *bool           { return &b }
func timePtr(t time.Time) *time.Time { return &t }

func TestCommandErrorPaths(t *testing.T) {
	e := newEnv(t)
	// No session.
	for _, args := range [][]string{
		{"product", "show", "81866477"},
		{"watch", "add", "search", "cemento", "--name", "x"},
		{"watch", "check", "--all"},
		{"store", "current"},
	} {
		if _, _, code := e.run(args...); code != 3 {
			t.Errorf("%v exit %d, want 3 (no session)", args, code)
		}
	}
	e.login()
	// Not found.
	if _, _, code := e.run("product", "show", "00000000"); code != 4 {
		t.Errorf("unknown product exit %d", code)
	}
	if _, _, code := e.run("category", "list", "--parent", "nope"); code != 4 {
		t.Errorf("unknown category exit %d", code)
	}
	if _, _, code := e.run("watch", "check", "nope"); code != 2 {
		t.Errorf("unknown watch exit %d", code)
	}
	if _, _, code := e.run("watch", "remove", "nope", "--yes"); code != 2 {
		t.Errorf("removing a missing watch exit %d", code)
	}
	// Blocked storefront.
	e.page = "block"
	if _, _, code := e.run("doctor"); code != 5 {
		t.Errorf("challenged doctor exit %d", code)
	}
	e.page = "ok"
	if _, _, code := e.run("doctor"); code != 0 {
		t.Errorf("healthy doctor exit %d", code)
	}
	// Config errors.
	if _, _, code := e.run("config", "get", "nope"); code != 2 {
		t.Errorf("config get unknown exit %d", code)
	}
	if _, _, code := e.run("config", "set", "nope", "x"); code != 2 {
		t.Errorf("config set unknown exit %d", code)
	}
	if out, _, code := e.run("config", "list"); code != 0 || !strings.Contains(out, "watch") {
		t.Errorf("config list: %d %s", code, out)
	}
	// Profiles.
	if out, _, code := e.run("profile", "use", "work"); code != 0 || !strings.Contains(out, "work") {
		t.Errorf("profile use: %d %s", code, out)
	}
	if _, _, code := e.run("profile", "use", "../escape"); code != 2 {
		t.Errorf("bad profile name exit %d", code)
	}
	if out, _, code := e.run("profile", "remove", "work", "--yes"); code != 0 || !strings.Contains(out, "work") {
		t.Errorf("profile remove: %d %s", code, out)
	}
	// Skills and mcp.
	if _, _, code := e.run("skills", "get", "nope"); code != 2 {
		t.Errorf("unknown skill exit %d", code)
	}
	if out, _, code := e.run("skills", "get", "leroymerlin-usage"); code != 0 || !strings.Contains(out, "leroymerlin") {
		t.Errorf("skill get: %d", code)
	}
	if _, _, code := e.run("mcp", "install", "nope"); code != 2 {
		t.Errorf("unknown harness exit %d", code)
	}
	if _, _, code := e.run("mcp", "--help"); code != 0 {
		t.Errorf("mcp help exit %d", code)
	}
	// Update on a development build.
	if _, _, code := e.run("update", "--check"); code != 2 {
		t.Errorf("development update exit %d", code)
	}
	// Harvest without a usable browser fails before launching.
	if _, _, code := e.run("auth", "harvest", "--profile", "/nope/profile", "--chrome", "/nope/chrome"); code == 0 {
		t.Error("harvest with a bad browser succeeded")
	}
	// Login from a netscape export and from a HAR.
	netscape := filepath.Join(e.home, "cookies.txt")
	os.WriteFile(netscape, []byte("# Netscape HTTP Cookie File\n.leroymerlin.es\tTRUE\t/\tTRUE\t0\tdatadome\tDD\n"), 0o600)
	if _, errOut, code := e.run("auth", "login", "--cookies", netscape); code != 0 {
		t.Errorf("netscape login exit %d: %s", code, errOut)
	}
	har := filepath.Join("..", "leroymerlin", "testdata", "session.synthetic.har")
	if _, errOut, code := e.run("auth", "login", "--har", har); code != 0 {
		t.Errorf("har login exit %d: %s", code, errOut)
	}
	if _, _, code := e.run("auth", "login", "--cookies", filepath.Join(e.home, "missing.json")); code == 0 {
		t.Error("login with a missing file succeeded")
	}
}

func TestCommandTreeAndArgsHelpers(t *testing.T) {
	tree := CommandTree("1.0.0")
	if tree == nil || tree.Name() != "leroymerlin" {
		t.Fatal("CommandTree did not build the root")
	}
	cmd := tree
	if err := exactArgs(0)(cmd, nil); err != nil {
		t.Fatalf("exactArgs(0): %v", err)
	}
	if err := exactArgs(2)(cmd, []string{"one"}); err == nil {
		t.Fatal("exactArgs(2) accepted one argument")
	}
	if err := maxArgs(0)(cmd, []string{"one"}); err == nil {
		t.Fatal("maxArgs(0) accepted an argument")
	}
	if err := noArgs(cmd, nil); err != nil {
		t.Fatalf("noArgs: %v", err)
	}
	if err := anyArgs(cmd, []string{"anything"}); err != nil {
		t.Fatalf("anyArgs: %v", err)
	}
}

func TestMoreCommandPaths(t *testing.T) {
	e := newEnv(t)
	t.Setenv("HOME", e.home)
	e.login()
	if out, _, code := e.run("auth", "status", "--check", "--format", "pretty"); code != 0 || !strings.Contains(out, "storefront") {
		t.Errorf("status --check: %d %s", code, out)
	}
	if _, _, code := e.run("config", "set", "watch.interval", "1m"); code != 0 {
		t.Fatal("config set failed")
	}
	if out, _, code := e.run("config", "get", "watch.interval"); code != 0 || !strings.Contains(out, "1m") {
		t.Errorf("config get: %d %s", code, out)
	}
	if _, _, code := e.run("store", "list", "--format", "jsonl"); code != 0 {
		t.Error("store list jsonl")
	}
	if _, _, code := e.run("category", "list", "--format", "jsonl"); code != 0 {
		t.Error("category list jsonl")
	}
	if _, _, code := e.run("skills", "list", "--format", "jsonl"); code != 0 {
		t.Error("skills jsonl")
	}
	if _, _, code := e.run("search", "cemento", "--in-stock", "--limit", "3", "--format", "pretty"); code != 0 {
		t.Error("filtered search")
	}
	if _, _, code := e.run("search", "cemento", "--page", "0"); code != 2 {
		t.Error("page zero accepted")
	}
	if _, _, code := e.run("watch", "add", "search", "cemento", "--name", "f", "--in-stock", "--limit", "2", "--pages", "2"); code != 0 {
		t.Error("watch add with filters failed")
	}
	if _, _, code := e.run("watch", "add", "search", "cemento", "--name", "i", "--interval", "1s"); code != 2 {
		t.Error("tiny interval accepted")
	}
	if _, _, code := e.run("watch", "add", "search", "cemento", "--name", "n", "--notify", "nope"); code != 2 {
		t.Error("unknown sink accepted")
	}
	if _, _, code := e.run("watch", "add", "product", "81866477", "--name", "bad/name"); code != 2 {
		t.Error("bad watch name accepted")
	}
	if _, _, code := e.run("watch", "events", "--since", "notaduration"); code != 2 {
		t.Error("bad --since accepted")
	}
	for _, harness := range []string{"cursor", "codex"} {
		if _, _, code := e.run("mcp", "install", harness); code != 0 {
			t.Errorf("mcp install %s failed", harness)
		}
	}
	if _, _, code := e.run("update", "--yes"); code != 2 {
		t.Error("development update with --yes accepted")
	}
	if _, _, code := e.run("completion", "bash"); code != 0 {
		t.Error("completion failed")
	}
	if _, errOut, code := e.run("doctor", "--error-format", "json"); code != 0 {
		t.Errorf("doctor json: %d %s", code, errOut)
	}
	if _, _, code := e.run("--debug", "search", "cemento"); code != 0 {
		t.Error("debug search failed")
	}
	e.withStore = true
	if out, _, code := e.run("store", "current", "--format", "pretty"); code != 0 || !strings.Contains(out, "Salamanca") {
		t.Errorf("store current pretty: %d %s", code, out)
	}
	if out, _, code := e.run("product", "stock", "81866477", "--format", "pretty"); code != 0 || !strings.Contains(out, "store pickup") {
		t.Errorf("with-store stock: %d %s", code, out)
	}
	if _, _, code := e.run("auth", "logout"); code != 0 {
		t.Error("logout failed")
	}
	if _, _, code := e.run("doctor", "--error-format", "json"); code != 3 {
		t.Error("doctor without a session should be exit 3")
	}
}
