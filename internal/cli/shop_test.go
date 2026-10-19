package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
)

func (e *env) login() {
	e.t.Helper()
	if _, errOut, code := e.run("auth", "login", "--cookies", e.cookieFile(), "--no-check"); code != 0 {
		e.t.Fatalf("login exit %d: %s", code, errOut)
	}
}

func TestSearchCLI(t *testing.T) {
	e := newEnv(t)
	e.login()

	out, errOut, code := e.run("search", "cemento cola")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var res leroymerlin.SearchResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Total != 737 || res.Page != 1 || res.Pages != 10 || len(res.Products) == 0 {
		t.Fatalf("result = %+v", res)
	}
	if res.Products[0].Ref != "81866477" {
		t.Fatalf("first product = %+v", res.Products[0])
	}

	out, _, code = e.run("search", "cemento cola", "--page", "2")
	if code != 0 || !strings.Contains(out, `"page": 2`) {
		t.Fatalf("page 2: %d %s", code, out)
	}
	out, _, code = e.run("search", "cemento cola", "--sort", "price-asc", "--max-price", "20")
	if code != 0 || !strings.Contains(out, `"sort": "price-asc"`) || !strings.Contains(out, `"total": 208`) {
		t.Fatalf("filtered: %d %s", code, out)
	}
	out, _, code = e.run("search", "--category", "construccion/cementos-morteros-y-yesos/cementos")
	if code != 0 || !strings.Contains(out, `"total": 288`) {
		t.Fatalf("category: %d %s", code, out)
	}
	out, _, code = e.run("search", "cemento cola", "--limit", "3")
	var three struct{ Products []leroymerlin.Product }
	json.Unmarshal([]byte(out), &three)
	if code != 0 || len(three.Products) != 3 {
		t.Fatalf("limit: %d %d products", code, len(three.Products))
	}
	if _, _, code := e.run("search", "cemento", "--category", "construccion"); code != 2 {
		t.Fatalf("keywords plus category exit %d, want 2", code)
	}
	if _, _, code := e.run("search", "cemento", "--sort", "cheapest"); code != 2 {
		t.Fatalf("bad sort exit %d, want 2", code)
	}
}

func TestProductCLI(t *testing.T) {
	e := newEnv(t)
	e.login()

	out, errOut, code := e.run("product", "show", "81866477")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var p leroymerlin.ProductPage
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}
	if p.Ref != "81866477" || p.Price == nil || !p.InStock || !p.Available {
		t.Fatalf("product = %+v", p)
	}
	if p.Store != nil || len(p.Deliveries) != 1 {
		t.Fatalf("stock = store %+v deliveries %+v", p.Store, p.Deliveries)
	}
	// item is an alias for product.
	out, _, code = e.run("item", "stock", "81866477")
	if code != 0 || !strings.Contains(out, `"online": true`) || !strings.Contains(out, `"store": null`) {
		t.Fatalf("stock: %d %s", code, out)
	}
	out, _, code = e.run("product", "stock", "81944344")
	if code != 0 || !strings.Contains(out, `"online": false`) {
		t.Fatalf("out of stock: %d %s", code, out)
	}
	if _, _, code := e.run("product", "show", "12345678"); code != 4 {
		t.Fatalf("unknown reference exit %d, want 4", code)
	}
}

func TestStoreCurrentWithoutSelection(t *testing.T) {
	e := newEnv(t)
	e.login()
	_, errOut, code := e.run("store", "current")
	if code != 4 || !strings.Contains(errOut, "no store selected") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestWatchCLI(t *testing.T) {
	e := newEnv(t)
	e.login()

	out, errOut, code := e.run("watch", "add", "search", "cemento cola", "--name", "cola", "--max-price", "20")
	if code != 0 {
		t.Fatalf("add: %d %s", code, errOut)
	}
	if !strings.Contains(out, `"name": "cola"`) || !strings.Contains(out, `"seen": 18`) {
		t.Fatalf("baseline = %s", out)
	}
	out, _, _ = e.run("watch", "list")
	var ws []map[string]any
	json.Unmarshal([]byte(out), &ws)
	if len(ws) != 1 || ws[0]["name"] != "cola" {
		t.Fatalf("list = %s", out)
	}
	out, errOut, code = e.run("watch", "check", "--all")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("check: %d %s %s", code, out, errOut)
	}
	if out, _, code := e.run("watch", "add", "product", "81866477", "--name", "pegoland"); code != 0 {
		t.Fatalf("add product: %d %s", code, out)
	}
	out, _, _ = e.run("watch", "events")
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("events = %s", out)
	}
	if out, _, code := e.run("watch", "remove", "cola", "--yes"); code != 0 || !strings.Contains(out, `"removed": "cola"`) {
		t.Fatalf("remove: %d %s", code, out)
	}
	if _, _, code := e.run("watch", "check", "--all", "--format", "jsonl"); code != 0 {
		t.Fatalf("jsonl check exit %d", code)
	}
}

func TestWatchNeedsAName(t *testing.T) {
	e := newEnv(t)
	e.login()
	if _, _, code := e.run("watch", "add", "search", "cemento"); code != 2 {
		t.Fatalf("missing --name exit %d, want 2", code)
	}
	if _, _, code := e.run("watch", "add", "search", "cemento", "--name", "a/b"); code != 2 {
		t.Fatalf("bad name exit %d, want 2", code)
	}
	if _, _, code := e.run("watch", "check"); code != 2 {
		t.Fatalf("check without --all exit %d, want 2", code)
	}
}

func TestSinkListAndTest(t *testing.T) {
	e := newEnv(t)
	cfg := filepath.Join(e.home, "config", "leroymerlin-cli", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `[sinks.ok]
type = "exec"
command = ["true"]

[sinks.broken]
type = "webhook"
`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := e.run("sink", "list")
	if code != 0 {
		t.Fatalf("list: %d %s", code, errOut)
	}
	if !strings.Contains(out, `"name": "ok"`) || !strings.Contains(out, `"invalid"`) {
		t.Fatalf("list = %s", out)
	}
	if out, _, code := e.run("sink", "test", "ok"); code != 0 || !strings.Contains(out, `"delivered": true`) {
		t.Fatalf("test: %d %s", code, out)
	}
	if _, _, code := e.run("sink", "test", "broken"); code != 2 {
		t.Fatalf("invalid sink exit %d, want 2", code)
	}
	if _, _, code := e.run("sink", "test", "nope"); code != 2 {
		t.Fatalf("unknown sink exit %d, want 2", code)
	}
}

func TestStoreCurrentWithStore(t *testing.T) {
	e := newEnv(t)
	e.withStore = true
	e.login()
	out, errOut, code := e.run("store", "current")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var s struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}
	if s.ID != "037" || s.Name != "Salamanca" {
		t.Fatalf("store = %s", out)
	}
}

func TestProductStockPickup(t *testing.T) {
	e := newEnv(t)
	e.withStore = true
	e.login()
	out, errOut, code := e.run("product", "stock", "81866477")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var v struct {
		Store      *leroymerlin.StoreRef
		Deliveries []leroymerlin.Delivery
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	if v.Store == nil || v.Store.ID != "037" || v.Store.Name != "Salamanca" {
		t.Fatalf("store = %+v", v.Store)
	}
	var pickup *leroymerlin.Delivery
	for i := range v.Deliveries {
		if v.Deliveries[i].Type == "storeDelivery" {
			pickup = &v.Deliveries[i]
		}
	}
	if pickup == nil || pickup.Stock != 308 || pickup.Status != "ONORDER" {
		t.Fatalf("deliveries = %+v", v.Deliveries)
	}
	pretty, _, code := e.run("product", "stock", "81866477", "--format", "pretty")
	if code != 0 || !strings.Contains(pretty, "store pickup, 2 opening days, on order, 308 available, free") {
		t.Fatalf("pretty stock = %s", pretty)
	}
}
