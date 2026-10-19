package leroymerlin

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// StoreRef identifies a physical store.
type StoreRef struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// Delivery is one way the site says a product can be obtained: home delivery,
// and (when a store is selected) pickup at that store.
type Delivery struct {
	Type   string `json:"type"`
	Stock  int    `json:"stock,omitempty"`
	Status string `json:"status,omitempty"`
	Time   string `json:"time,omitempty"`
	Price  *Money `json:"price,omitempty"`
}

// ProductPage is one product, as its page states it.
type ProductPage struct {
	Ref         string     `json:"ref"`
	Name        string     `json:"name"`
	Brand       string     `json:"brand,omitempty"`
	URL         string     `json:"url"`
	Description string     `json:"description,omitempty"`
	GTIN        string     `json:"gtin,omitempty"`
	Image       string     `json:"image,omitempty"`
	Price       *Money     `json:"price,omitempty"`
	PriceNoTax  *Money     `json:"price_without_tax,omitempty"`
	InStock     bool       `json:"in_stock"`
	Available   bool       `json:"available"`
	Rating      *float64   `json:"rating,omitempty"`
	Reviews     int        `json:"reviews,omitempty"`
	Seller      string     `json:"seller,omitempty"`
	SellerType  string     `json:"seller_type,omitempty"`
	Store       *StoreRef  `json:"store"`
	Deliveries  []Delivery `json:"deliveries,omitempty"`
}

// jsonLDProduct is the schema.org block the product page carries.
type jsonLDProduct struct {
	Name        string          `json:"name"`
	SKU         string          `json:"sku"`
	GTIN        string          `json:"gtin"`
	Description string          `json:"description"`
	Image       json.RawMessage `json:"image"`
	Offers      json.RawMessage `json:"offers"`
	Rating      struct {
		ReviewCount string `json:"reviewCount"`
		RatingValue string `json:"ratingValue"`
	} `json:"aggregateRating"`
}

type jsonLDOffer struct {
	Price        string `json:"price"`
	Currency     string `json:"priceCurrency"`
	Availability string `json:"availability"`
	URL          string `json:"url"`
}

// detailProduct is one entry of the product page's cdl_products island.
type detailProduct struct {
	Brand string `json:"brand"`
	SKU   string `json:"sku"`
	Name  string `json:"name"`
	Offer *struct {
		AddToCart  bool    `json:"add_to_cart_availability"`
		PriceATI   float64 `json:"unitprice_ati"`
		PriceTF    float64 `json:"unitprice_tf"`
		Seller     string  `json:"seller_name"`
		SellerTyp  string  `json:"seller_type"`
		Deliveries []struct {
			Price  float64 `json:"price"`
			Stock  int     `json:"stock"`
			Status string  `json:"stockStatus"`
			Time   string  `json:"time"`
			Type   string  `json:"type"`
		} `json:"available_deliveries"`
	} `json:"offer"`
}

var refFromPathRe = regexp.MustCompile(`-(\d{6,9})\.html$`)

// RefFromURL returns the product reference at the end of a product URL, or ""
// when the path carries none.
func RefFromURL(p string) string {
	m := refFromPathRe.FindStringSubmatch(p)
	if m == nil {
		return ""
	}
	return m[1]
}

// ParseProductPage reads a product page.
func ParseProductPage(body []byte) (*ProductPage, error) {
	p := &ProductPage{Deliveries: []Delivery{}}
	if raw, ok := scriptJSON(body, `id="jsonld_PRODUCT"`); ok {
		var ld jsonLDProduct
		if err := json.Unmarshal(raw, &ld); err != nil {
			return nil, &Error{Kind: KindAPIChanged, Endpoint: "jsonld_PRODUCT",
				Msg: "the product's structured data no longer parses. The site changed its shape"}
		}
		p.Name, p.Ref, p.GTIN, p.Description = ld.Name, ld.SKU, ld.GTIN, ld.Description
		p.Image = firstJSONString(ld.Image)
		if offer := parseOffer(ld.Offers); offer != nil {
			if v, err := strconv.ParseFloat(offer.Price, 64); err == nil {
				cur := offer.Currency
				if cur == "" {
					cur = "EUR"
				}
				p.Price = &Money{Amount: v, Currency: cur}
			}
			p.InStock = strings.HasSuffix(offer.Availability, "InStock")
			p.URL = offer.URL
		}
		if v, err := strconv.ParseFloat(ld.Rating.RatingValue, 64); err == nil {
			p.Rating = &v
		}
		if n, err := strconv.Atoi(ld.Rating.ReviewCount); err == nil {
			p.Reviews = n
		}
	}
	for _, raw := range dataTmsValue(body, "cdl_products") {
		var items []detailProduct
		if err := json.Unmarshal(raw, &items); err != nil {
			continue
		}
		for _, it := range items {
			if it.Offer == nil || (p.Ref != "" && it.SKU != "" && it.SKU != p.Ref) {
				continue
			}
			p.Available = it.Offer.AddToCart
			if it.Brand != "" {
				p.Brand = it.Brand
			}
			if p.Ref == "" {
				p.Ref = it.SKU
			}
			if p.Price == nil && it.Offer.PriceATI > 0 {
				p.Price = &Money{Amount: it.Offer.PriceATI, Currency: "EUR"}
			}
			if p.PriceNoTax == nil && it.Offer.PriceTF > 0 {
				p.PriceNoTax = &Money{Amount: it.Offer.PriceTF, Currency: "EUR"}
			}
			if it.Offer.Seller != "" {
				p.Seller, p.SellerType = it.Offer.Seller, it.Offer.SellerTyp
			}
			for _, d := range it.Offer.Deliveries {
				dl := Delivery{Type: d.Type, Stock: d.Stock, Status: d.Status, Time: d.Time}
				if d.Price > 0 {
					dl.Price = &Money{Amount: d.Price, Currency: "EUR"}
				}
				p.Deliveries = append(p.Deliveries, dl)
			}
		}
	}
	if p.Ref == "" && p.Name == "" {
		return nil, &Error{Kind: KindAPIChanged, Endpoint: "product",
			Msg: "the product page carried neither structured data nor a product island. The site changed its shape"}
	}
	p.Store = parseStore(body)
	for i := range p.Deliveries {
		if p.Deliveries[i].Time != "" {
			p.Deliveries[i].Time = humanizeDeliveryTime(p.Deliveries[i].Time)
		}
	}
	return p, nil
}

// parseStore reads the store the session has selected, if the page states one.
// Without a selection the site renders an empty data-main-store-id and the
// tracking layer says "no cookie here".
func parseStore(body []byte) *StoreRef {
	id := htmlAttr(body, "data-main-store-id")
	name := htmlAttr(body, "data-main-store-name")
	if id == "" && name == "" {
		for _, raw := range dataTmsValue(body, "cdl_user_context_main_store") {
			var v string
			if json.Unmarshal(raw, &v) == nil && v != "" && v != "no cookie here" {
				name = v
			}
		}
	}
	if id == "" && name == "" {
		if t := htmlClassText(body, "l-context-header__item-store-name-text"); t != "" && t != "¿Dónde estás?" {
			name = t
		}
	}
	if id == "" && name == "" {
		return nil
	}
	return &StoreRef{ID: id, Name: name}
}

// ParseStoreContext returns the store the session has selected, from a home
// or product page. nil means the session has no store yet.
func ParseStoreContext(body []byte) *StoreRef { return parseStore(body) }

// CurrentStore fetches the home page and reports the store selected in the
// session, if there is one.
func (c *Client) CurrentStore(ctx context.Context) (*StoreRef, error) {
	p, err := c.GetHTML(ctx, "/")
	if err != nil {
		return nil, err
	}
	return ParseStoreContext(p.Body), nil
}

// parseOffer accepts schema.org offers as an object or a list.
func parseOffer(raw json.RawMessage) *jsonLDOffer {
	if len(raw) == 0 {
		return nil
	}
	var one jsonLDOffer
	if err := json.Unmarshal(raw, &one); err == nil && one.Price != "" {
		return &one
	}
	var list []jsonLDOffer
	if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
		return &list[0]
	}
	return nil
}

func firstJSONString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		return list[0]
	}
	return ""
}

// humanizeDeliveryTime turns the site's "5 OPENING_DAY" into "5 opening days".
func humanizeDeliveryTime(s string) string {
	parts := strings.SplitN(s, " ", 2)
	if len(parts) != 2 {
		return strings.ReplaceAll(s, "_", " ")
	}
	unit := strings.ToLower(strings.ReplaceAll(parts[1], "_", " "))
	if n, err := strconv.Atoi(parts[0]); err == nil && n > 1 && !strings.HasSuffix(unit, "s") {
		unit += "s"
	}
	return parts[0] + " " + unit
}
