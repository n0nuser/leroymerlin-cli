package watch

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
	"github.com/n0nuser/leroymerlin-cli/internal/store"
)

// fakeFetcher returns the next queued search result per call.
type fakeFetcher struct {
	searches []*leroymerlin.SearchResult
	products []*leroymerlin.ProductPage
	searchN  int
	productN int
	fail     bool
	last     leroymerlin.SearchQuery
}

func (f *fakeFetcher) Search(ctx context.Context, q leroymerlin.SearchQuery) (*leroymerlin.SearchResult, error) {
	f.last = q
	if f.fail {
		return nil, &leroymerlin.Error{Kind: leroymerlin.KindNetwork, Msg: "boom"}
	}
	if f.searchN >= len(f.searches) {
		return f.searches[len(f.searches)-1], nil
	}
	r := f.searches[f.searchN]
	f.searchN++
	return r, nil
}

func (f *fakeFetcher) ProductByURL(ctx context.Context, refOrURL string) (*leroymerlin.ProductPage, error) {
	if f.fail {
		return nil, &leroymerlin.Error{Kind: leroymerlin.KindNetwork, Msg: "boom"}
	}
	p := f.products[min(f.productN, len(f.products)-1)]
	f.productN++
	return p, nil
}

func product(ref string, price float64, available bool) leroymerlin.Product {
	return leroymerlin.Product{Ref: ref, Name: "P " + ref, URL: "https://example/" + ref, Price: &leroymerlin.Money{Amount: price, Currency: "EUR"}, Available: available}
}

func searchPage(products ...leroymerlin.Product) *leroymerlin.SearchResult {
	return &leroymerlin.SearchResult{Page: 1, Pages: 1, Products: products}
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestSearchCheckReportsChanges(t *testing.T) {
	ctx := t.Context()
	st := newStore(t)
	target, _ := json.Marshal(SearchTarget{Query: leroymerlin.SearchQuery{Keywords: "cemento"}})
	w, err := st.AddWatch(ctx, store.Watch{Name: "cola", Profile: "default", Kind: store.KindSearch, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeFetcher{searches: []*leroymerlin.SearchResult{
		searchPage(product("1", 10, true), product("2", 20, false)),
		searchPage(product("1", 12, true), product("2", 20, true), product("3", 5, true)),
		searchPage(product("1", 12, true), product("3", 5, true)),
	}}
	// Baseline: silent, but the state is committed.
	base, err := Check(ctx, f, w, map[string]store.Seen{})
	if err != nil {
		t.Fatal(err)
	}
	if len(base.Upserts) != 2 {
		t.Fatalf("baseline = %+v", base)
	}
	seen := map[string]store.Seen{}
	for _, k := range base.Upserts {
		seen[k.Ref] = k
	}
	// The CLI baselines silently: it commits the state but not the "new"
	// events the empty state produced.
	if err := st.CommitCheck(ctx, w.ID, base.Upserts, base.Removed, nil); err != nil {
		t.Fatal(err)
	}
	// Second check: a price change, a product back in stock and a new one.
	res, err := Check(ctx, f, w, seen)
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]int{}
	for _, ev := range res.Events {
		types[ev.Type]++
	}
	for _, want := range []string{"product.price_changed", "product.in_stock", "product.new"} {
		if types[want] == 0 {
			t.Fatalf("missing %s in %+v", want, types)
		}
	}
	if types["product.new"] != 1 || types["product.price_changed"] != 1 {
		t.Fatalf("event counts = %+v", types)
	}
	var change map[string]float64
	for _, ev := range res.Events {
		if ev.Type == "product.price_changed" {
			_ = json.Unmarshal(ev.Change, &change)
		}
	}
	if change["from"] != 10 || change["to"] != 12 {
		t.Fatalf("price change = %+v", change)
	}
	for _, k := range res.Upserts {
		seen[k.Ref] = k
	}
	// Third check: product 2 left the result.
	res, err = Check(ctx, f, w, seen)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "2" {
		t.Fatalf("removed = %v", res.Removed)
	}
	if len(res.Events) != 1 || res.Events[0].Type != "product.removed" {
		t.Fatalf("events = %+v", res.Events)
	}
}

func TestProductCheckTracksPriceAndStock(t *testing.T) {
	ctx := t.Context()
	target, _ := json.Marshal(ProductTarget{Ref: "9", URL: "/productos/x-9.html"})
	f := &fakeFetcher{products: []*leroymerlin.ProductPage{
		{Ref: "9", Name: "Cemento", URL: "https://example/9", Price: &leroymerlin.Money{Amount: 4, Currency: "EUR"}, InStock: true, Available: true,
			Store: &leroymerlin.StoreRef{ID: "33", Name: "Badalona"}, Deliveries: []leroymerlin.Delivery{{Type: "storePickup", Stock: 4, Status: "ONSITE"}}},
		{Ref: "9", Name: "Cemento", URL: "https://example/9", Price: &leroymerlin.Money{Amount: 5, Currency: "EUR"}, InStock: true, Available: true,
			Store: &leroymerlin.StoreRef{ID: "33", Name: "Badalona"}, Deliveries: []leroymerlin.Delivery{{Type: "storePickup", Stock: 4, Status: "ONORDER"}}},
	}}
	w, err := newStore(t).AddWatch(ctx, store.Watch{Name: "p", Profile: "default", Kind: store.KindProduct, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	base, _ := Check(ctx, f, w, map[string]store.Seen{})
	if len(base.Upserts) != 1 || base.Upserts[0].Store != "Badalona" || base.Upserts[0].Stock != 4 {
		t.Fatalf("baseline = %+v", base.Upserts)
	}
	seen := map[string]store.Seen{"9": base.Upserts[0]}
	res, err := Check(ctx, f, w, seen)
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]bool{}
	for _, ev := range res.Events {
		types[ev.Type] = true
	}
	if !types["product.price_changed"] || !types["product.stock_changed"] {
		t.Fatalf("events = %+v", types)
	}
	var change map[string]string
	for _, ev := range res.Events {
		if ev.Type == "product.stock_changed" {
			_ = json.Unmarshal(ev.Change, &change)
		}
	}
	if change["to_status"] != "ONORDER" {
		t.Fatalf("stock change = %+v", change)
	}
}

// A watched search keeps the local filters, so `watch add search --in-stock
// --limit 5` means the same as a search with those flags.
func TestSearchCheckCarriesLocalFilters(t *testing.T) {
	ctx := t.Context()
	target, _ := json.Marshal(SearchTarget{Query: leroymerlin.SearchQuery{Keywords: "cemento", InStock: true, Limit: 5}})
	w := store.Watch{Name: "x", Kind: store.KindSearch, Target: target, Pages: 1}
	f := &fakeFetcher{searches: []*leroymerlin.SearchResult{searchPage(product("1", 1, true))}}
	if _, err := Check(ctx, f, w, nil); err != nil {
		t.Fatal(err)
	}
	if !f.last.InStock || f.last.Limit != 5 {
		t.Fatalf("check sent %+v, want the local filters", f.last)
	}
}

func TestCheckPropagatesFetchErrors(t *testing.T) {
	ctx := t.Context()
	w := store.Watch{Name: "x", Kind: store.KindSearch, Target: json.RawMessage(`{"query":{"keywords":"a"}}`)}
	if _, err := Check(ctx, &fakeFetcher{fail: true}, w, nil); err == nil {
		t.Fatal("a fetch error was swallowed")
	}
}
