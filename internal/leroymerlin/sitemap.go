package leroymerlin

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// The site publishes its store list and category tree as sitemaps for
// crawlers. They need no session and robots.txt allows them, so `store list`
// and `category list` work before `auth login`.
const (
	storeSitemap    = "/sitemap-store1.xml"
	categorySitemap = "/sitemap-category1.xml"
)

type urlset struct {
	URLs []struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
		Image   struct {
			Loc   string `xml:"loc"`
			Title string `xml:"title"`
		} `xml:"image"`
	} `xml:"url"`
}

// Store is one physical store. Slug is the stable key, taken from the store
// page URL (/tiendas/<slug>.html).
type Store struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Compact bool   `json:"compact"`
	URL     string `json:"url"`
}

// Category is one node of the product taxonomy. Path is the part of the URL
// after /productos/, without slashes at the ends, and is what `search
// --category` takes.
type Category struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Depth  int    `json:"depth"`
	Parent string `json:"parent,omitempty"`
	URL    string `json:"url"`
}

// Stores reads the store sitemap.
func (c *Client) Stores(ctx context.Context) ([]Store, error) {
	p, err := c.GetXML(ctx, storeSitemap)
	if err != nil {
		return nil, err
	}
	return ParseStores(p.Body)
}

// ParseStores turns the store sitemap into stores, sorted by name.
func ParseStores(raw []byte) ([]Store, error) {
	var set urlset
	if err := xml.Unmarshal(raw, &set); err != nil {
		return nil, apiChanged(storeSitemap, raw, "store sitemap is not XML: %v", err)
	}
	var out []Store
	for _, u := range set.URLs {
		pu, err := url.Parse(u.Loc)
		if err != nil {
			continue
		}
		slug, ok := strings.CutPrefix(pu.Path, "/tiendas/")
		if !ok || !strings.HasSuffix(slug, ".html") {
			continue
		}
		slug = strings.TrimSuffix(slug, ".html")
		name := strings.TrimSpace(u.Image.Title)
		if name == "" {
			name = slug
		}
		out = append(out, Store{Slug: slug, Name: name, Compact: strings.HasPrefix(slug, "compact-"), URL: u.Loc})
	}
	if len(out) == 0 {
		return nil, apiChanged(storeSitemap, raw, "the store sitemap lists no stores")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Categories reads the category sitemap.
func (c *Client) Categories(ctx context.Context) ([]Category, error) {
	p, err := c.GetXML(ctx, categorySitemap)
	if err != nil {
		return nil, err
	}
	return ParseCategories(p.Body)
}

// ParseCategories turns the category sitemap into a flat list sorted by path,
// so a parent always comes before its children.
func ParseCategories(raw []byte) ([]Category, error) {
	var set urlset
	if err := xml.Unmarshal(raw, &set); err != nil {
		return nil, apiChanged(categorySitemap, raw, "category sitemap is not XML: %v", err)
	}
	var out []Category
	for _, u := range set.URLs {
		pu, err := url.Parse(u.Loc)
		if err != nil {
			continue
		}
		rest, ok := strings.CutPrefix(pu.Path, "/productos/")
		path := strings.Trim(rest, "/")
		if !ok || path == "" {
			continue
		}
		segs := strings.Split(path, "/")
		cat := Category{Path: path, Name: strings.TrimSpace(u.Image.Title), Depth: len(segs), URL: u.Loc}
		if cat.Name == "" {
			cat.Name = segs[len(segs)-1]
		}
		if len(segs) > 1 {
			cat.Parent = strings.Join(segs[:len(segs)-1], "/")
		}
		out = append(out, cat)
	}
	if len(out) == 0 {
		return nil, apiChanged(categorySitemap, raw, "the category sitemap lists no categories")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func apiChanged(endpoint string, body []byte, format string, args ...any) error {
	return &Error{Kind: KindAPIChanged, Endpoint: endpoint, Body: Excerpt(body), Msg: fmt.Sprintf(format, args...)}
}
