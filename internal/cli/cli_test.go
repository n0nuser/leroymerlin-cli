package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSite serves the captured fixtures. "/" answers according to *home:
// "ok", or "block" for the captured challenge page.
func fakeSite(t *testing.T, home *string, withStore *bool) *httptest.Server {
	t.Helper()
	fixture := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join("..", "leroymerlin", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap-store1.xml":
			w.Write(fixture("sitemap_store1.xml"))
		case "/sitemap-category1.xml":
			w.Write(fixture("sitemap_category1.xml"))
		case "/":
			if *home == "block" {
				w.Header().Set("Server", "DataDome")
				w.WriteHeader(403)
				w.Write(fixture("blocked_datadome.html"))
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "datadome", Value: "ROTATED", Domain: ".leroymerlin.es", Path: "/", MaxAge: 3600})
			if *withStore {
				w.Write(fixture("home_salamanca.html"))
				return
			}
			w.Write(fixture("home_nostore.html"))
		case "/search":
			switch {
			case r.URL.Query().Get("p") == "2":
				w.Write(fixture("search_cemento_cola_page2.html"))
			case r.URL.Query().Get("filters") != "":
				w.Write(fixture("search_cemento_cola_filtered.html"))
			default:
				w.Write(fixture("search_cemento_cola.html"))
			}
		case "/productos/construccion/cementos-morteros-y-yesos/cementos/":
			w.Write(fixture("category_cementos.html"))
		case "/productos/cemento-cola-pegoland-blanco-25-kg-81866477.html":
			if *withStore {
				w.Write(fixture("product_81866477_store.html"))
				return
			}
			w.Write(fixture("product_81866477.html"))
		case "/productos/mortero-cola-axton-flexible-gel-blanco-25-kg-81944344.html":
			w.Write(fixture("product_81944344.html"))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type env struct {
	t         *testing.T
	home      string
	page      string
	withStore bool
}

func newEnv(t *testing.T) *env {
	e := &env{t: t, home: t.TempDir(), page: "ok"}
	srv := fakeSite(t, &e.page, &e.withStore)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(e.home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(e.home, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(e.home, "state"))
	t.Setenv("LEROYMERLIN_BASE_URL", srv.URL)
	t.Setenv("LEROYMERLIN_COOKIEJAR", "")
	t.Setenv("LEROYMERLIN_PROFILE", "")
	t.Setenv("LEROYMERLIN_CONFIG", "")
	return e
}

func (e *env) run(args ...string) (stdout, stderr string, code int) {
	var o, er bytes.Buffer
	code = Execute("0.0.0-test", args, strings.NewReader(""), &o, &er)
	return o.String(), er.String(), code
}

// cookieFile writes a synthetic export whose datadome cookie also matches the
// 127.0.0.1 test server.
func (e *env) cookieFile() string {
	p := filepath.Join(e.home, "export.json")
	os.WriteFile(p, []byte(`[{"name":"datadome","value":"SECRET_VALUE","domain":".leroymerlin.es","path":"/"},
		{"name":"datadome","value":"SECRET_VALUE","domain":"127.0.0.1","path":"/"}]`), 0o600)
	return p
}

func TestStoreListFromSitemap(t *testing.T) {
	e := newEnv(t)
	out, errOut, code := e.run("store", "list", "--query", "madrid")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var stores []map[string]any
	if err := json.Unmarshal([]byte(out), &stores); err != nil {
		t.Fatal(err)
	}
	if len(stores) == 0 {
		t.Fatal("no Madrid stores")
	}
	for _, s := range stores {
		if !strings.Contains(strings.ToLower(s["name"].(string)+s["slug"].(string)), "madrid") {
			t.Fatalf("filter leaked %v", s)
		}
	}
	out, _, _ = e.run("store", "list", "--format", "jsonl", "--compact")
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.Contains(line, `"compact":true`) {
			t.Fatalf("jsonl line %s", line)
		}
	}
}

func TestCategoryListFilters(t *testing.T) {
	e := newEnv(t)
	out, errOut, code := e.run("category", "list", "--parent", "construccion", "--depth", "1")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var cats []struct {
		Path  string
		Depth int
	}
	json.Unmarshal([]byte(out), &cats)
	if len(cats) == 0 {
		t.Fatal("no children")
	}
	for _, c := range cats {
		if c.Depth != 2 || !strings.HasPrefix(c.Path, "construccion/") {
			t.Fatalf("unexpected %+v", c)
		}
	}
	if _, _, code := e.run("category", "list", "--parent", "nope"); code != 4 {
		t.Fatalf("unknown parent exit %d, want 4", code)
	}
}

func TestLoginStatusLogout(t *testing.T) {
	e := newEnv(t)
	out, errOut, code := e.run("auth", "login", "--cookies", e.cookieFile())
	if code != 0 {
		t.Fatalf("login exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, `"session_valid": true`) {
		t.Fatalf("login did not validate: %s", out)
	}
	path := filepath.Join(e.home, "config", "leroymerlin-cli", "sessions", "default.json")
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("session file %v %v", fi, err)
	}
	// The login check got a rotated cookie; it must be on disk.
	raw, _ := os.ReadFile(path)
	if !bytes.Contains(raw, []byte("ROTATED")) {
		t.Fatal("rotated cookie not persisted")
	}
	out, _, _ = e.run("auth", "status")
	if strings.Contains(out, "SECRET_VALUE") || strings.Contains(out, "ROTATED") {
		t.Fatalf("status leaks a cookie value: %s", out)
	}
	if !strings.Contains(out, `"has_datadome": true`) {
		t.Fatalf("status: %s", out)
	}
	if out, _, code = e.run("auth", "logout"); code != 0 || !strings.Contains(out, `"removed": true`) {
		t.Fatalf("logout %d %s", code, out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("session still on disk after logout")
	}
}

func TestTopLevelLoginAlias(t *testing.T) {
	e := newEnv(t)
	if _, errOut, code := e.run("login", "--cookies", e.cookieFile(), "--no-check"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestLoginNeedsInputWithoutTTY(t *testing.T) {
	e := newEnv(t)
	if _, _, code := e.run("auth", "login"); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestDoctorReportsChallenge(t *testing.T) {
	e := newEnv(t)
	e.run("auth", "login", "--cookies", e.cookieFile(), "--no-check")
	e.page = "block"
	out, errOut, code := e.run("doctor")
	if code != 5 {
		t.Fatalf("doctor exit %d, want 5: %s", code, errOut)
	}
	if !strings.Contains(out, `"challenged": true`) || !strings.Contains(out, `"session_valid": false`) {
		t.Fatalf("doctor output: %s", out)
	}
	e.page = "ok"
	if _, errOut, code := e.run("doctor"); code != 0 {
		t.Fatalf("healthy doctor exit %d: %s", code, errOut)
	}
}

func TestDoctorWithoutSessionIsAuth(t *testing.T) {
	e := newEnv(t)
	if _, _, code := e.run("doctor"); code != 3 {
		t.Fatalf("exit %d, want 3", code)
	}
}

// A session from LEROYMERLIN_COOKIEJAR is never written, even when the site
// rotates its cookie, and login refuses to shadow it.
func TestEnvSessionStaysOffDisk(t *testing.T) {
	e := newEnv(t)
	t.Setenv("LEROYMERLIN_COOKIEJAR", e.cookieFile())
	if _, errOut, code := e.run("auth", "status", "--check"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(e.home, "config", "leroymerlin-cli", "sessions")); !os.IsNotExist(err) {
		t.Fatal("env session reached the disk")
	}
	if _, _, code := e.run("auth", "login", "--cookies", e.cookieFile()); code != 2 {
		t.Fatalf("login with env set: exit %d, want 2", code)
	}
}

func TestErrorEnvelopeAndUsage(t *testing.T) {
	e := newEnv(t)
	_, errOut, code := e.run("store", "list", "--format", "xml")
	if code != 2 || !strings.Contains(errOut, "unknown format") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	_, errOut, code = e.run("category", "list", "--parent", "nope", "--error-format", "json")
	var env struct {
		Code     int    `json:"code"`
		Category string `json:"category"`
	}
	if err := json.Unmarshal([]byte(errOut), &env); err != nil || env.Code != 4 || env.Category != "not_found" || code != 4 {
		t.Fatalf("envelope %q exit %d", errOut, code)
	}
	if _, _, code := e.run("--profile", "../escape", "store", "list"); code != 2 {
		t.Fatalf("bad profile exit %d", code)
	}
}

func TestConfigSetValidates(t *testing.T) {
	e := newEnv(t)
	if _, _, code := e.run("config", "set", "defualt_profile", "x"); code != 2 {
		t.Fatalf("typo key exit %d, want 2", code)
	}
	if out, _, code := e.run("config", "set", "default_profile", "madrid"); code != 0 || !strings.Contains(out, "madrid") {
		t.Fatalf("set: %d %s", code, out)
	}
	out, _, _ := e.run("config", "get", "default_profile")
	if strings.TrimSpace(out) != `"madrid"` {
		t.Fatalf("get: %s", out)
	}
}

func TestMCPToolsListAndCall(t *testing.T) {
	newEnv(t)
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"store_list","arguments":{"query":"badalona"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"store_list","arguments":{"bogus":1}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"doctor","arguments":{}}}`,
	}, "\n") + "\n"
	var out, errOut bytes.Buffer
	if code := Execute("0.0.0-test", []string{"mcp"}, strings.NewReader(in), &out, &errOut); code != 0 {
		t.Fatalf("mcp exit %d: %s", code, errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 5 { // the notification gets no reply
		t.Fatalf("%d replies:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"protocolVersion":"2025-03-26"`) {
		t.Fatalf("initialize: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"store_list"`) || !strings.Contains(lines[1], `"inputSchema"`) {
		t.Fatalf("tools/list: %s", lines[1])
	}
	if !strings.Contains(lines[2], `badalona`) || !strings.Contains(lines[2], `"isError":false`) {
		t.Fatalf("tools/call: %s", lines[2])
	}
	if !strings.Contains(lines[3], `-32602`) {
		t.Fatalf("bad argument: %s", lines[3])
	}
	// doctor fails without a session: a tool error carrying the envelope.
	if !strings.Contains(lines[4], `"isError":true`) || !strings.Contains(lines[4], `\"code\":3`) {
		t.Fatalf("doctor tool: %s", lines[4])
	}
}

func TestMain(m *testing.M) {
	minInterval = 0 // fake servers need no pacing
	os.Exit(m.Run())
}

// Separate invocations share the rate gate through a stamp in the state dir.
func TestRequestStampPersists(t *testing.T) {
	e := newEnv(t)
	if _, errOut, code := e.run("store", "list"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	raw, err := os.ReadFile(filepath.Join(e.home, "state", "leroymerlin-cli", "last-request"))
	if err != nil || len(raw) == 0 {
		t.Fatalf("no request stamp: %v", err)
	}
}
