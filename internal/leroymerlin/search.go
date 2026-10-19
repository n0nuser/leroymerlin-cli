package leroymerlin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Money is an amount in a currency, as the site states it.
type Money struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// SortOption is one of the orders the site offers for a result list.
type SortOption struct {
	Code    string `json:"code"`
	Label   string `json:"label"`
	Applied bool   `json:"applied"`
}

// FilterValue is one choice of a facet.
type FilterValue struct {
	Value   string `json:"value"`
	Total   int    `json:"total"`
	Applied bool   `json:"applied"`
}

// Filter is a facet as the result page renders it, applied or not.
type Filter struct {
	Type    string        `json:"type"`
	Code    string        `json:"code"`
	Label   string        `json:"label"`
	Applied bool          `json:"applied"`
	Unit    string        `json:"unit,omitempty"`
	Value   string        `json:"value,omitempty"`
	Values  []FilterValue `json:"values,omitempty"`
}

// Product is one catalogue entry as a result list shows it.
type Product struct {
	Ref         string   `json:"ref"`
	Name        string   `json:"name"`
	Brand       string   `json:"brand,omitempty"`
	URL         string   `json:"url"`
	Price       *Money   `json:"price,omitempty"`
	PriceNoTax  *Money   `json:"price_without_tax,omitempty"`
	Rating      *float64 `json:"rating,omitempty"`
	Seller      string   `json:"seller,omitempty"`
	SellerType  string   `json:"seller_type,omitempty"`
	Available   bool     `json:"available"`
	Sponsored   bool     `json:"sponsored,omitempty"`
	Position    int      `json:"position,omitempty"`
	TotalOffers int      `json:"total_offers,omitempty"`
}

// SearchQuery is what a search asks the site for. Category and Keywords are
// alternatives: a category browse is a search over one branch of the tree.
type SearchQuery struct {
	Keywords string
	Category string
	Brands   []string
	// Prices are decimal strings, kept as typed so 5, 5.5 and 5.50 reach the
	// site unchanged.
	MinPrice, MaxPrice string
	// SoldByLeroyMerlin applies the site's "Vendido por Leroy Merlin" toggle.
	SoldByLeroyMerlin bool
	Sort              string
	Page              int
	// InStock and Limit are local: the page is fetched whole, then filtered.
	// Keeping them here means `search` and a watched search apply them the
	// same way.
	InStock bool
	Limit   int
}

// SortCodes are the orders the storefront understands, from the result page.
var SortCodes = []string{"relevance", "price-asc", "price-desc"}

// Path is the storefront path this query reads, filters included.
func (q SearchQuery) Path() string {
	base := "/search"
	if q.Category != "" {
		base = "/productos/" + strings.Trim(q.Category, "/") + "/"
	}
	v := url.Values{}
	if q.Keywords != "" {
		v.Set("q", q.Keywords)
	}
	if q.Sort != "" && q.Sort != "relevance" {
		v.Set("sort", q.Sort)
	}
	if q.Page > 1 {
		v.Set("p", strconv.Itoa(q.Page))
	}
	filters := map[string]string{}
	if len(q.Brands) > 0 {
		filters["attribute-06575"] = strings.Join(q.Brands, "#|")
	}
	if q.SoldByLeroyMerlin {
		filters["vendor"] = "internal"
	}
	if q.MinPrice != "" {
		filters["range-price-min"] = q.MinPrice
	}
	if q.MaxPrice != "" {
		filters["range-price-max"] = q.MaxPrice
	}
	if len(filters) > 0 {
		raw, _ := json.Marshal(filters) // map keys, deterministic
		v.Set("filters", string(raw))
	}
	if enc := v.Encode(); enc != "" {
		base += "?" + enc
	}
	return base
}

// SearchResult is one page of results, with the facets and orders the site
// offered beside them.
type SearchResult struct {
	Query    string       `json:"query,omitempty"`
	Category string       `json:"category,omitempty"`
	Page     int          `json:"page"`
	Pages    int          `json:"pages"`
	Total    int          `json:"total"`
	Sort     string       `json:"sort"`
	Sorts    []SortOption `json:"sorts,omitempty"`
	Filters  []Filter     `json:"filters,omitempty"`
	Products []Product    `json:"products"`
}

// guidanceData is the server-rendered JSON island the result page carries.
type guidanceData struct {
	SSRData struct {
		SSRDataType string `json:"ssrDataType"`
		Header      struct {
			Type  string `json:"type"`
			Query struct {
				Original string `json:"original"`
			} `json:"query"`
		} `json:"header"`
		Body struct {
			Pagination struct {
				CurrentPage   int `json:"currentPage"`
				NumberOfPages int `json:"numberOfPages"`
			} `json:"pagination"`
			Products struct {
				Number int `json:"number"`
			} `json:"products"`
			Sorts   []SortOption `json:"sorts"`
			Filters struct {
				StockAndDelivery []Filter `json:"stockAndDeliveryFilters"`
				Product          []Filter `json:"productFilters"`
			} `json:"filters"`
		} `json:"body"`
	} `json:"ssrData"`
}

// cardProduct is one entry of a cdl_products_list tracking island.
type cardProduct struct {
	Brand     string   `json:"brand"`
	SKU       string   `json:"sku"`
	ID        string   `json:"identifier"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Position  int      `json:"position"`
	Rating    *float64 `json:"rating"`
	Sponsored bool     `json:"product_is_sponsored"`
	Offer     *struct {
		AddToCart bool    `json:"add_to_cart_availability"`
		PriceATI  float64 `json:"unitprice_ati"`
		PriceTF   float64 `json:"unitprice_tf"`
		Seller    string  `json:"seller_name"`
		SellerTyp string  `json:"seller_type"`
		Offers    int     `json:"total_offer_count"`
	} `json:"offer"`
}

// ParseSearch reads a search or category result page.
func ParseSearch(body []byte) (*SearchResult, error) {
	if !bytes.Contains(body, []byte(`id="guidance-ssr-data"`)) && !bytes.Contains(body, []byte(`id="guidance-product-list"`)) {
		return nil, &Error{Kind: KindAPIChanged, Endpoint: "search",
			Msg: "the page is not a result list. The site changed its shape"}
	}
	res := &SearchResult{Products: []Product{}}
	if raw, ok := scriptJSON(body, `id="guidance-ssr-data"`); ok {
		var g guidanceData
		if err := json.Unmarshal(raw, &g); err != nil {
			return nil, &Error{Kind: KindAPIChanged, Endpoint: "guidance-ssr-data",
				Msg: "the result page's guidance data no longer parses. The site changed its shape"}
		}
		b := g.SSRData.Body
		res.Query = g.SSRData.Header.Query.Original
		res.Page = b.Pagination.CurrentPage
		res.Pages = b.Pagination.NumberOfPages
		res.Total = b.Products.Number
		res.Sorts = b.Sorts
		res.Filters = append(res.Filters, b.Filters.StockAndDelivery...)
		res.Filters = append(res.Filters, b.Filters.Product...)
		for _, s := range b.Sorts {
			if s.Applied {
				res.Sort = s.Code
			}
		}
	}
	if res.Page == 0 {
		res.Page = 1
	}
	if res.Sort == "" {
		res.Sort = "relevance"
	}
	res.Products = parseCards(body)
	if len(res.Products) == 0 && res.Total == 0 {
		return nil, &Error{Kind: KindAPIChanged, Endpoint: "search",
			Msg: "the result page carried no product list. The site changed its shape"}
	}
	return res, nil
}

// parseCards reads the product list island the page embeds. The page carries
// several cdl_products_list islands: the result list (the big one) and small
// auxiliary blocks (sponsored or recently viewed) that are identical on every
// page. Only the largest island is the result list; ordering follows the
// position the site assigned inside it.
func parseCards(body []byte) []Product {
	var best []cardProduct
	for _, raw := range dataTmsValue(body, "cdl_products_list") {
		var items []cardProduct
		if err := json.Unmarshal(raw, &items); err != nil {
			continue
		}
		if len(items) > len(best) {
			best = items
		}
	}
	byRef := map[string]Product{}
	for _, it := range best {
		if it.SKU == "" {
			continue
		}
		p := Product{
			Ref: it.SKU, Name: it.Name, Brand: it.Brand, URL: absoluteURL(it.URL),
			Rating: it.Rating, Sponsored: it.Sponsored, Position: it.Position,
		}
		if it.Offer != nil {
			p.Available = it.Offer.AddToCart
			p.Seller = it.Offer.Seller
			p.SellerType = it.Offer.SellerTyp
			p.TotalOffers = it.Offer.Offers
			if it.Offer.PriceATI > 0 {
				p.Price = &Money{Amount: it.Offer.PriceATI, Currency: "EUR"}
			}
			if it.Offer.PriceTF > 0 {
				p.PriceNoTax = &Money{Amount: it.Offer.PriceTF, Currency: "EUR"}
			}
		}
		byRef[p.Ref] = p
	}
	out := make([]Product, 0, len(byRef))
	for _, p := range byRef {
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}

// absoluteURL turns a site-relative path into a full URL.
func absoluteURL(p string) string {
	if p == "" || strings.HasPrefix(p, "http") {
		return p
	}
	return DefaultBaseURL + p
}

// Search fetches and parses one result page, then applies the local filters
// (in-stock, limit) the query carries.
func (c *Client) Search(ctx context.Context, q SearchQuery) (*SearchResult, error) {
	p, err := c.GetHTML(ctx, q.Path())
	if err != nil {
		return nil, err
	}
	res, err := ParseSearch(p.Body)
	if err != nil {
		return nil, err
	}
	if q.InStock {
		kept := res.Products[:0]
		for _, p := range res.Products {
			if p.Available {
				kept = append(kept, p)
			}
		}
		res.Products = kept
	}
	if q.Limit > 0 && len(res.Products) > q.Limit {
		res.Products = res.Products[:q.Limit]
	}
	return res, nil
}

// Product reads one product from a reference or URL. A bare reference needs
// one search first: the site redirects a reference query to the product page
// when it recognises it, so that page is parsed directly instead of fetching
// it twice.
func (c *Client) Product(ctx context.Context, refOrURL string) (*ProductPage, error) {
	if strings.Contains(refOrURL, "/") || strings.HasSuffix(refOrURL, ".html") {
		return c.ProductByURL(ctx, refOrURL)
	}
	p, err := c.GetHTML(ctx, "/search?q="+url.QueryEscape(refOrURL))
	if err != nil {
		return nil, err
	}
	if strings.Contains(p.URL, "/productos/") && RefFromURL(p.URL) == refOrURL {
		return ParseProductPage(p.Body)
	}
	res, err := ParseSearch(p.Body)
	if err != nil {
		return nil, err
	}
	for _, it := range res.Products {
		if it.Ref == refOrURL {
			return c.ProductByURL(ctx, it.URL)
		}
	}
	return nil, &Error{Kind: KindNotFound, Msg: fmt.Sprintf("no product with reference %s in the catalogue", refOrURL)}
}

// ProductByURL fetches and parses one product page.
func (c *Client) ProductByURL(ctx context.Context, refOrURL string) (*ProductPage, error) {
	path := refOrURL
	if u, err := url.Parse(refOrURL); err == nil && u.Host != "" {
		path = u.RequestURI()
	}
	p, err := c.GetHTML(ctx, path)
	if err != nil {
		return nil, err
	}
	return ParseProductPage(p.Body)
}
