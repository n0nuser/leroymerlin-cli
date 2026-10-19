package leroymerlin

import (
	"net/url"
	"testing"
)

// All captures below are real responses from 2026-10-19, with cookies, signed
// ktx tokens and context ids stripped by hand.

func TestSearchFixture(t *testing.T) {
	res, err := ParseSearch(readFixture(t, "search_cemento_cola.html"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Query != "cemento cola" || res.Page != 1 || res.Pages != 10 || res.Total != 737 {
		t.Fatalf("header = %+v", res)
	}
	if res.Sort != "relevance" || len(res.Sorts) != 3 || len(res.Filters) != 8 {
		t.Fatalf("facets = sort %q sorts %d filters %d", res.Sort, len(res.Sorts), len(res.Filters))
	}
	if len(res.Products) < 18 {
		t.Fatalf("got %d products, the eager slice has 18", len(res.Products))
	}
	first := res.Products[0]
	if first.Ref != "81866477" || first.Name != "Cemento cola Pegoland blanco 25 kg" {
		t.Fatalf("first = %+v", first)
	}
	if first.Price == nil || first.Price.Amount != 3.94 || first.Price.Currency != "EUR" {
		t.Fatalf("first price = %+v", first.Price)
	}
	if first.URL != "https://www.leroymerlin.es/productos/cemento-cola-pegoland-blanco-25-kg-81866477.html" {
		t.Fatalf("first url = %q", first.URL)
	}
	if !first.Available || first.Seller != "Leroy Merlin" {
		t.Fatalf("first availability = %+v", first)
	}
	for i := 1; i < len(res.Products); i++ {
		if res.Products[i-1].Position > res.Products[i].Position {
			t.Fatalf("products are not in position order at %d", i)
		}
	}
	roundTrip(t, res)
}

func TestSearchFixtureFilteredAndSorted(t *testing.T) {
	res, err := ParseSearch(readFixture(t, "search_cemento_cola_filtered.html"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Sort != "price-asc" {
		t.Fatalf("sort = %q, want the applied price-asc", res.Sort)
	}
	if res.Total != 208 {
		t.Fatalf("total = %d, want the filtered 208", res.Total)
	}
	var applied *Filter
	for i := range res.Filters {
		if res.Filters[i].Applied {
			applied = &res.Filters[i]
		}
	}
	if applied == nil || applied.Code != "price" {
		t.Fatalf("applied filter = %+v", applied)
	}
	// The site pins a promoted product at position 11, so the page is not
	// strictly ascending; the first product still shows the applied order.
	if len(res.Products) == 0 || res.Products[0].Ref != "86672595" || res.Products[0].Price == nil || res.Products[0].Price.Amount != 5.04 {
		t.Fatalf("first product = %+v", res.Products[0])
	}
}

func TestSearchFixturePageTwo(t *testing.T) {
	res, err := ParseSearch(readFixture(t, "search_cemento_cola_page2.html"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Page != 2 || res.Pages != 10 || res.Total != 737 {
		t.Fatalf("pagination = %+v", res)
	}
	if len(res.Products) == 0 {
		t.Fatal("page 2 carried no products")
	}
}

func TestCategoryFixture(t *testing.T) {
	res, err := ParseSearch(readFixture(t, "category_cementos.html"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 288 || res.Pages != 6 || res.Page != 1 {
		t.Fatalf("category header = %+v", res)
	}
	if res.Query != "" {
		t.Fatalf("a category browse has no query, got %q", res.Query)
	}
	if len(res.Products) == 0 {
		t.Fatal("category page carried no products")
	}
	roundTrip(t, res.Products)
}

func TestSearchQueryPath(t *testing.T) {
	cases := []struct {
		q    SearchQuery
		want string
	}{
		{SearchQuery{Keywords: "cemento cola"}, "/search?q=cemento+cola"},
		{SearchQuery{Keywords: "cemento cola", Page: 2}, "/search?p=2&q=cemento+cola"},
		{SearchQuery{Keywords: "mortero", Sort: "price-asc"}, "/search?q=mortero&sort=price-asc"},
		{SearchQuery{Keywords: "mortero", Brands: []string{"SIKA"}}, `/search?filters=%7B%22attribute-06575%22%3A%22SIKA%22%7D&q=mortero`},
		{SearchQuery{Keywords: "mortero", MinPrice: "5", MaxPrice: "20"}, `/search?filters=%7B%22range-price-max%22%3A%2220%22%2C%22range-price-min%22%3A%225%22%7D&q=mortero`},
		{SearchQuery{Keywords: "mortero", SoldByLeroyMerlin: true}, `/search?filters=%7B%22vendor%22%3A%22internal%22%7D&q=mortero`},
		{SearchQuery{Category: "construccion/cementos-morteros-y-yesos/cementos"}, "/productos/construccion/cementos-morteros-y-yesos/cementos/"},
	}
	for _, tc := range cases {
		got := tc.q.Path()
		if got != tc.want {
			t.Errorf("Path() = %q, want %q", got, tc.want)
		}
		if u, err := url.Parse(got); err != nil || u.Path == "" {
			t.Errorf("Path() %q does not parse: %v", got, err)
		}
	}
}

func TestProductFixture(t *testing.T) {
	p, err := ParseProductPage(readFixture(t, "product_81866477.html"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Ref != "81866477" || p.Name != "Cemento cola Pegoland blanco 25 kg" {
		t.Fatalf("product = %+v", p)
	}
	if p.Price == nil || p.Price.Amount != 3.94 {
		t.Fatalf("price = %+v", p.Price)
	}
	if !p.InStock || !p.Available {
		t.Fatalf("availability = in_stock %v available %v", p.InStock, p.Available)
	}
	if p.GTIN != "8429340110040" {
		t.Fatalf("gtin = %q", p.GTIN)
	}
	if len(p.Deliveries) != 1 {
		t.Fatalf("deliveries = %+v", p.Deliveries)
	}
	if d := p.Deliveries[0]; d.Type != "homeDelivery" || d.Stock != 308 || d.Status != "ONSITE" || d.Time != "5 opening days" {
		t.Fatalf("delivery = %+v", d)
	}
	if p.Store != nil {
		t.Fatalf("this capture has no store selected, got %+v", p.Store)
	}
	roundTrip(t, *p)
}

func TestProductFixtureOutOfStock(t *testing.T) {
	p, err := ParseProductPage(readFixture(t, "product_81944344.html"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Ref != "81944344" || p.InStock || p.Available {
		t.Fatalf("product = %+v", p)
	}
	if len(p.Deliveries) != 0 {
		t.Fatalf("an unavailable product showed deliveries: %+v", p.Deliveries)
	}
}

// Regression: a product reference must be read from the URL, not guessed from
// the page's recommendation list.
func TestRefFromURL(t *testing.T) {
	for in, want := range map[string]string{
		"/productos/cemento-cola-pegoland-blanco-25-kg-81866477.html": "81866477",
		"https://www.leroymerlin.es/productos/x-13925520.html":        "13925520",
		"/productos/construccion/cementos/":                           "",
		"81866477":                                                    "",
	} {
		if got := RefFromURL(in); got != want {
			t.Errorf("RefFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// The captured home page has no store chosen ("¿Dónde estás?"), so stock is
// online only. A session with a store renders data-main-store-id instead.
func TestStoreContextWithoutStore(t *testing.T) {
	body := readFixture(t, "home_nostore.html")
	if s := ParseStoreContext(body); s != nil {
		t.Fatalf("a session with no store reported %+v", s)
	}
	syn := []byte(`<div class="context-header-container" data-main-store-id="33" data-main-store-name="Badalona"></div>`)
	if s := ParseStoreContext(syn); s == nil || s.ID != "33" || s.Name != "Badalona" {
		t.Fatalf("selected store = %+v", s)
	}
}

func TestHumanizeDeliveryTime(t *testing.T) {
	for in, want := range map[string]string{
		"5 OPENING_DAY":  "5 opening days",
		"1 OPENING_DAY":  "1 opening day",
		"2 CALENDAR_DAY": "2 calendar days",
	} {
		if got := humanizeDeliveryTime(in); got != want {
			t.Errorf("humanizeDeliveryTime(%q) = %q, want %q", in, got, want)
		}
	}
}
