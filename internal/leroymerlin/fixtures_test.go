package leroymerlin

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// roundTrip re-marshals v and decodes it again into a fresh value of the same
// type, and fails if anything moved. It pins the JSON contract of the type.
func roundTrip[T any](t *testing.T, v T) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var back T
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v, back) {
		t.Fatalf("round trip changed the value:\n got %+v\nwant %+v", back, v)
	}
}

// sitemap_store1.xml is the live store sitemap captured 2026-10-19 (no session needed).
func TestStoreSitemapFixture(t *testing.T) {
	stores, err := ParseStores(readFixture(t, "sitemap_store1.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stores) < 100 {
		t.Fatalf("got %d stores, the capture has over 100", len(stores))
	}
	bySlug := map[string]Store{}
	for _, s := range stores {
		if s.Slug == "" || s.Name == "" || s.URL == "" {
			t.Fatalf("incomplete store %+v", s)
		}
		bySlug[s.Slug] = s
	}
	if s := bySlug["badalona"]; s.Name != "Badalona" || s.Compact || s.URL != "https://www.leroymerlin.es/tiendas/badalona.html" {
		t.Fatalf("badalona = %+v", s)
	}
	if s := bySlug["compact-tudela"]; s.Name != "Compact Tudela" || !s.Compact {
		t.Fatalf("compact-tudela = %+v", s)
	}
	roundTrip(t, stores)
}

// sitemap_category1.xml is a trimmed slice of the live category sitemap
// (2026-10-19): the top level plus two branches, entries unmodified.
func TestCategorySitemapFixture(t *testing.T) {
	cats, err := ParseCategories(readFixture(t, "sitemap_category1.xml"))
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]Category{}
	for i, c := range cats {
		if i > 0 && cats[i-1].Path > c.Path {
			t.Fatal("categories are not sorted by path")
		}
		byPath[c.Path] = c
	}
	top, ok := byPath["construccion"]
	if !ok || top.Depth != 1 || top.Parent != "" || top.Name == "" {
		t.Fatalf("construccion = %+v (present %v)", top, ok)
	}
	sub, ok := byPath["construccion/aislamiento"]
	if !ok || sub.Depth != 2 || sub.Parent != "construccion" {
		t.Fatalf("construccion/aislamiento = %+v (present %v)", sub, ok)
	}
	roundTrip(t, cats)
}

// blocked_datadome.html is the challenge page the site served to plain curl on
// 2026-10-19, with its per-request tokens replaced by REDACTED.
func TestBlockPageFixture(t *testing.T) {
	body := readFixture(t, "blocked_datadome.html")
	h := http.Header{"Server": {"DataDome"}, "X-Datadome": {"protected"}}
	if !IsBlockPage(403, h, body) {
		t.Fatal("captured challenge page not recognised")
	}
	// The body alone is enough when a proxy drops the vendor headers.
	if !IsBlockPage(403, http.Header{}, body) {
		t.Fatal("challenge body without vendor headers not recognised")
	}
	if IsBlockPage(403, http.Header{}, []byte("<html>forbidden</html>")) {
		t.Fatal("a plain origin 403 is not a challenge")
	}
	if IsBlockPage(200, http.Header{}, []byte("<html>ok</html>")) {
		t.Fatal("a normal page is not a challenge")
	}
	err := classify(&Page{Status: 403, Header: h, Body: body}, "/search")
	if e, ok := err.(*Error); !ok || e.Kind != KindBlocked || e.Kind.ExitCode() != 5 {
		t.Fatalf("classify = %v, want blocked (exit 5)", err)
	}
}
