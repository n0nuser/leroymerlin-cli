package leroymerlin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestKindCodesAndNames(t *testing.T) {
	cases := []struct {
		kind Kind
		code int
		name string
	}{
		{KindGeneric, 1, "generic"},
		{KindUsage, 2, "usage"},
		{KindAuth, 3, "auth"},
		{KindNotFound, 4, "not_found"},
		{KindBlocked, 5, "blocked"},
		{KindNetwork, 6, "network"},
		{KindAPIChanged, 7, "api_changed"},
	}
	for _, tc := range cases {
		if tc.kind.ExitCode() != tc.code || tc.kind.String() != tc.name {
			t.Errorf("%d = %d/%s, want %d/%s", tc.kind, tc.kind.ExitCode(), tc.kind.String(), tc.code, tc.name)
		}
	}
	if err := Usage("x %d", 1); err.Error() != "x 1" {
		t.Fatalf("Usage = %q", err.Error())
	}
}

func TestClassifyStatuses(t *testing.T) {
	challenge := readFixture(t, "blocked_datadome.html")
	cases := []struct {
		status int
		header http.Header
		body   []byte
		kind   Kind
	}{
		{200, nil, []byte("ok"), KindGeneric},
		{403, http.Header{"X-Datadome": {"protected"}}, challenge, KindBlocked},
		{403, nil, []byte("plain forbidden"), KindAPIChanged},
		{404, nil, nil, KindNotFound},
		{410, nil, nil, KindNotFound},
		{429, nil, nil, KindBlocked},
		{500, nil, nil, KindNetwork},
		{503, nil, nil, KindNetwork},
		{418, nil, []byte("teapot"), KindAPIChanged},
	}
	for _, tc := range cases {
		err := classify(&Page{Status: tc.status, Header: tc.header, Body: tc.body}, "/x")
		if tc.kind == KindGeneric {
			if err != nil {
				t.Errorf("%d classified as %v", tc.status, err)
			}
			continue
		}
		var le *Error
		if !errors.As(err, &le) || le.Kind != tc.kind {
			t.Errorf("%d = %v, want %v", tc.status, err, tc.kind)
		}
	}
}

func TestExcerptRedactsAndTruncates(t *testing.T) {
	long := strings.Repeat("a", 500)
	out := Excerpt([]byte(long))
	if len(out) > 300 {
		t.Fatalf("excerpt %d bytes", len(out))
	}
	out = Excerpt([]byte(`{"datadome":"0123456789abcdef0123"}`))
	if strings.Contains(out, "0123456789abcdef") {
		t.Fatalf("secret leaked: %s", out)
	}
}

func TestSessionJarEdgeCases(t *testing.T) {
	s := &Session{Cookies: []Cookie{
		{Name: "expired", Value: "x", Domain: ".leroymerlin.es", Expires: time.Now().Add(-time.Hour)},
		{Name: "typed", Value: "y", Domain: "leroymerlin.es", Path: "/deep"},
		{Name: "keep", Value: "z", Domain: ".leroymerlin.es", Path: "/"},
	}}
	if c, ok := s.Cookie("expired"); ok {
		t.Fatalf("expired cookie returned %+v", c)
	}
	u := mustURL(t, "https://www.leroymerlin.es/deep/page")
	names := []string{}
	for _, c := range s.Jar().Cookies(u) {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "typed,keep" {
		t.Fatalf("jar = %v", names)
	}
	// A response can remove a cookie and add a host-only one.
	s.Jar().SetCookies(u, []*http.Cookie{
		{Name: "keep", MaxAge: -1, Domain: ".leroymerlin.es", Path: "/"},
		{Name: "hostonly", Value: "h"},
		{Name: "foreign", Value: "f", Domain: "example.com"},
	})
	if _, ok := s.Cookie("keep"); ok {
		t.Fatal("a removed cookie stayed")
	}
	if !s.Dirty() {
		t.Fatal("removal did not mark the session dirty")
	}
	var hostOnly, foreign bool
	for _, c := range s.Cookies {
		if c.Name == "hostonly" && c.Domain == "www.leroymerlin.es" {
			hostOnly = true
		}
		if c.Name == "foreign" {
			foreign = true
		}
	}
	if !hostOnly || foreign {
		t.Fatalf("cookies = %+v", s.Cookies)
	}
}

func TestSummarizeAndCookieString(t *testing.T) {
	var nilSession *Session
	if nilSession.Dirty() {
		t.Fatal("nil session is dirty")
	}
	if _, ok := nilSession.Cookie("x"); ok {
		t.Fatal("nil session returned a cookie")
	}
	s := &Session{Cookies: []Cookie{{Name: "datadome", Value: "secret", Domain: ".leroymerlin.es", Expires: time.Now().Add(time.Hour)}}}
	sum := s.Summarize()
	if sum.Cookies != 1 || !sum.HasDataDome || sum.Expires == nil {
		t.Fatalf("summary = %+v", sum)
	}
	if strings.Contains(s.Cookies[0].String(), "secret") {
		t.Fatalf("cookie string leaks: %s", s.Cookies[0].String())
	}
	plain := (&Session{Cookies: []Cookie{{Name: "a", Value: "b"}}}).Summarize()
	if plain.HasDataDome || plain.Expires != nil {
		t.Fatalf("plain summary = %+v", plain)
	}
}

func TestParseExportDetectAndErrors(t *testing.T) {
	if _, err := ParseExport([]byte("   "), FormatAuto); err == nil {
		t.Fatal("empty export accepted")
	}
	if _, err := ParseExport([]byte("datadome=x"), "bogus"); err == nil {
		t.Fatal("unknown format accepted")
	}
	// A BOM is stripped.
	if _, err := ParseExport([]byte("\xef\xbb\xbfdatadome=x"), FormatHeader); err != nil {
		t.Fatal(err)
	}
	// Netscape line with the wrong field count.
	if _, err := ParseExport([]byte("a\tb\tc\n"), FormatNetscape); err == nil {
		t.Fatal("short cookies.txt line accepted")
	}
	// A header with no cookies.
	if _, err := ParseExport([]byte("not-a-cookie"), FormatHeader); err == nil {
		t.Fatal("junk header accepted")
	}
	// HAR that does not parse.
	if _, err := ParseExport([]byte(`{"log":`), FormatHAR); err == nil {
		t.Fatal("broken HAR accepted")
	}
	if _, err := ParseExport([]byte(`{"log":{"entries":[]}}`), FormatHAR); err == nil {
		t.Fatal("empty HAR accepted")
	}
	if detect([]byte(`{"log":{"entries":[]}}`)) != FormatHAR {
		t.Fatal("HAR not detected")
	}
	if detect([]byte(`[{"name":"x"}]`)) != FormatJSON {
		t.Fatal("array not detected")
	}
	if detect([]byte("# Netscape\n.leroymerlin.es\tTRUE\t/\tTRUE\t0\ta\tb")) != FormatNetscape {
		t.Fatal("netscape not detected")
	}
	if detect([]byte("datadome=x")) != FormatHeader {
		t.Fatal("header not detected")
	}
}

func TestParseStoresErrorsAndBranches(t *testing.T) {
	if _, err := ParseStores([]byte("not xml")); err == nil {
		t.Fatal("bad store xml accepted")
	}
	if _, err := ParseStores([]byte(`<urlset></urlset>`)); err == nil {
		t.Fatal("empty store sitemap accepted")
	}
	raw := []byte(`<urlset>
	  <url><loc>https://www.leroymerlin.es/tiendas/compact-sin-titulo.html</loc></url>
	  <url><loc>https://www.leroymerlin.es/otra.html</loc></url>
	</urlset>`)
	stores, err := ParseStores(raw)
	if err != nil || len(stores) != 1 || stores[0].Name != "Compact Sin Titulo" || !stores[0].Compact {
		t.Fatalf("stores = %+v %v", stores, err)
	}
}

func TestParseCategoriesErrorsAndBranches(t *testing.T) {
	if _, err := ParseCategories([]byte("not xml")); err == nil {
		t.Fatal("bad category xml accepted")
	}
	if _, err := ParseCategories([]byte(`<urlset></urlset>`)); err == nil {
		t.Fatal("empty category sitemap accepted")
	}
	raw := []byte(`<urlset>
	  <url><loc>https://www.leroymerlin.es/productos/construccion/cementos/</loc><image:image><image:title>Cementos</image:title></image:image></url>
	  <url><loc>https://www.leroymerlin.es/productos/construccion/</loc></url>
	  <url><loc>https://www.leroymerlin.es/otra/</loc></url>
	</urlset>`)
	cats, err := ParseCategories(raw)
	if err != nil || len(cats) != 2 {
		t.Fatalf("categories = %+v %v", cats, err)
	}
	byPath := map[string]Category{}
	for _, c := range cats {
		byPath[c.Path] = c
	}
	if byPath["construccion"].Name != "construccion" || byPath["construccion"].Parent != "" {
		t.Fatalf("top = %+v", byPath["construccion"])
	}
	if byPath["construccion/cementos"].Parent != "construccion" || byPath["construccion/cementos"].Depth != 2 {
		t.Fatalf("child = %+v", byPath["construccion/cementos"])
	}
}

func TestProductPageErrorBranches(t *testing.T) {
	if _, err := ParseProductPage([]byte(`<script type="application/ld+json" id="jsonld_PRODUCT">{`)); err == nil {
		t.Fatal("broken jsonld accepted")
	}
	if _, err := ParseProductPage([]byte("<html>nothing</html>")); err == nil {
		t.Fatal("a page with no product parsed")
	}
	// A jsonld offer list and a session-cookie store context.
	body := []byte(`<script type="application/ld+json" id="jsonld_PRODUCT">{"name":"X","sku":"1","offers":[{"price":"2.5","priceCurrency":"EUR","availability":"http://schema.org/OutOfStock"}]}</script>`)
	p, err := ParseProductPage(body)
	if err != nil {
		t.Fatal(err)
	}
	if p.Price == nil || p.Price.Amount != 2.5 || p.InStock {
		t.Fatalf("product = %+v", p)
	}
	if humanizeDeliveryTime("no-number") != "no-number" {
		t.Fatalf("humanize = %q", humanizeDeliveryTime("no-number"))
	}
}

func TestProductLookupViaFakeSite(t *testing.T) {
	search := readFixture(t, "search_cemento_cola.html")
	product := readFixture(t, "product_81866477.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/productos/") {
			w.Write(product)
			return
		}
		w.Write(search)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	// A bare reference: the search page answers, then the product page.
	p, err := c.Product(context.Background(), "81866477")
	if err != nil || p.Ref != "81866477" {
		t.Fatalf("Product = %+v %v", p, err)
	}
	// A URL goes straight to the product path.
	if p, err := c.Product(context.Background(), "/productos/x-81866477.html"); err != nil || p.Ref != "81866477" {
		t.Fatalf("URL = %+v %v", p, err)
	}
	// An unknown reference is a not-found error.
	if _, err := c.Product(context.Background(), "00000000"); err == nil {
		t.Fatal("unknown reference accepted")
	}
	// ProductByURL strips a full URL back to its path and query.
	if _, err := c.ProductByURL(context.Background(), srv.URL+"/productos/x-81866477.html?q=1"); err != nil {
		t.Fatal(err)
	}
}

func TestSearchErrorsAndCards(t *testing.T) {
	if _, err := ParseSearch([]byte("<html>plain</html>")); err == nil {
		t.Fatal("a page that is not a result list parsed")
	}
	broken := []byte(`<ol id="guidance-product-list"></ol><script type="application/json" id="guidance-ssr-data">{</script>`)
	if _, err := ParseSearch(broken); err == nil {
		t.Fatal("broken guidance data accepted")
	}
	// Cards with a nil offer and duplicate refs keep one entry each.
	page := []byte(`<ol id="guidance-product-list"></ol>
	<script type="application/json" class="dataTms">[{"name":"cdl_products_list","value":[{"sku":"1","name":"A","url":"/a"},{"sku":"1","name":"A","url":"/a"}]}]</script>
	<script type="application/json" class="dataTms">[{"name":"other","value":[]}]</script>`)
	res, err := ParseSearch(page)
	if err != nil || len(res.Products) != 1 || res.Products[0].Price != nil {
		t.Fatalf("cards = %+v %v", res, err)
	}
}

func TestSearchQueryPathVariants(t *testing.T) {
	got := SearchQuery{Keywords: "x", Brands: []string{"A", "B"}, SoldByLeroyMerlin: true, MinPrice: "1", MaxPrice: "2", Page: 3, Sort: "price-desc", Category: ""}.Path()
	for _, want := range []string{"q=x", "p=3", "sort=price-desc", "attribute-06575", "vendor", "range-price-min", "range-price-max"} {
		if !strings.Contains(got, want) {
			t.Errorf("path %q lacks %q", got, want)
		}
	}
	if p := (SearchQuery{Keywords: "x", Sort: "relevance", Page: 1}).Path(); p != "/search?q=x" {
		t.Fatalf("default path = %q", p)
	}
	if p := (SearchQuery{Page: 1}).Path(); p != "/search" {
		t.Fatalf("empty path = %q", p)
	}
	if absoluteURL("") != "" || absoluteURL("https://x/y") != "https://x/y" || absoluteURL("/p") != DefaultBaseURL+"/p" {
		t.Fatal("absoluteURL variants")
	}
}

func TestDataTmsHelpers(t *testing.T) {
	body := []byte(`<script type="application/json" class="dataTms">broken</script>
	<script type="application/json" class="dataTms">[{"name":"a","value":"x"}]</script>
	<div data-store-id="7"></div>`)
	if vals := dataTmsValue(body, "a"); len(vals) != 1 || string(vals[0]) != `"x"` {
		t.Fatalf("dataTms = %v", vals)
	}
	if got := htmlAttr(body, "data-store-id"); got != "7" {
		t.Fatalf("htmlAttr = %q", got)
	}
	if got := htmlAttr(body, "missing"); got != "" {
		t.Fatalf("missing attr = %q", got)
	}
	if _, ok := scriptJSON(body, `id="nope"`); ok {
		t.Fatal("found a missing script")
	}
	if got := htmlClassText(body, "nope"); got != "" {
		t.Fatalf("class text = %q", got)
	}
}

func TestSameSiteRedirects(t *testing.T) {
	if err := sameSite(&http.Request{URL: mustURL(t, "https://www.leroymerlin.es/x")}, []*http.Request{{URL: mustURL(t, "https://www.leroymerlin.es/")}}); err != nil {
		t.Fatalf("same-site redirect blocked: %v", err)
	}
	if err := sameSite(&http.Request{URL: mustURL(t, "https://evil.example/x")}, []*http.Request{{URL: mustURL(t, "https://www.leroymerlin.es/")}}); err != http.ErrUseLastResponse {
		t.Fatalf("foreign redirect = %v", err)
	}
	via := make([]*http.Request, 5)
	for i := range via {
		via[i] = &http.Request{URL: mustURL(t, "https://www.leroymerlin.es/")}
	}
	if err := sameSite(&http.Request{URL: mustURL(t, "https://www.leroymerlin.es/x")}, via); err == nil {
		t.Fatal("sixth redirect allowed")
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestClientHintsAndSeed(t *testing.T) {
	c := &Client{}
	if hints := c.clientHints(DefaultUserAgent); hints["Sec-Ch-Ua-Platform"] != `"Linux"` {
		t.Fatalf("default hints = %v", hints)
	}
	if hints := c.clientHints("Custom/1"); hints != nil {
		t.Fatalf("custom hints = %v", hints)
	}
	c.Session = &Session{ClientHints: map[string]string{"Sec-Ch-Ua": `"X"`}}
	if hints := c.clientHints("Custom/1"); hints["Sec-Ch-Ua"] != `"X"` {
		t.Fatalf("session hints = %v", hints)
	}
	SeedLastRequest(time.Now().Add(-time.Hour))
	SeedLastRequest(time.Now().Add(time.Hour)) // future stamps are ignored
	if LastRequest().After(time.Now()) {
		t.Fatal("future stamp accepted")
	}
}

func TestParseProductPageStoreVariants(t *testing.T) {
	for _, body := range []string{
		`<span class="l-context-header__item-store-name-text">Salamanca</span>`,
		`<div data-store-id="037"></div>`,
	} {
		s := ParseStoreContext([]byte(body))
		if s == nil || s.Name == "" && s.ID == "" {
			t.Fatalf("store context not read from %s", body)
		}
	}
	if ParseStoreContext([]byte(`<span class="l-context-header__item-store-name-text">¿Dónde estás?</span>`)) != nil {
		t.Fatal("the no-store placeholder counted as a store")
	}
	if ParseStoreContext([]byte("nothing")) != nil {
		t.Fatal("empty page reported a store")
	}
}

func TestTransportDialErrorIsNetwork(t *testing.T) {
	old := retryBase
	retryBase = time.Millisecond
	t.Cleanup(func() { retryBase = old })
	c := &Client{BaseURL: "https://127.0.0.1:1", HTTP: &http.Client{Transport: ChromeTransport(nil)}, MinInterval: 0}
	_, err := c.GetHTML(context.Background(), "/")
	var le *Error
	if !errors.As(err, &le) || le.Kind != KindNetwork {
		t.Fatalf("dial error = %v", err)
	}
}

func TestParseExportSnakeCaseUserAgent(t *testing.T) {
	res, err := ParseExport([]byte(`{"user_agent":"UA/1","cookies":[{"name":"datadome","value":"x","domain":".leroymerlin.es","path":"/"}]}`), FormatJSON)
	if err != nil || res.Session.UserAgent != "UA/1" {
		t.Fatalf("snake case user agent = %+v %v", res.Session, err)
	}
}

func TestSearchCardOrderTieBreak(t *testing.T) {
	page := []byte(`<ol id="guidance-product-list"></ol>
	<script type="application/json" class="dataTms">[{"name":"cdl_products_list","value":[
	  {"sku":"2","name":"B","position":1,"offer":{"unitprice_ati":2}},
	  {"sku":"1","name":"A","position":1,"offer":{"unitprice_ati":1}}
	]}]</script>`)
	res, err := ParseSearch(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Products) != 2 || res.Products[0].Ref != "1" {
		t.Fatalf("order = %+v", res.Products)
	}
}

func TestProductStoreNameOnly(t *testing.T) {
	body := []byte(`<script type="application/ld+json" id="jsonld_PRODUCT">{"name":"X","sku":"1"}</script><div data-main-store-name="Salamanca"></div>`)
	p, err := ParseProductPage(body)
	if err != nil {
		t.Fatal(err)
	}
	if p.Store == nil || p.Store.Name != "Salamanca" || p.Store.ID != "" {
		t.Fatalf("store = %+v", p.Store)
	}
}
