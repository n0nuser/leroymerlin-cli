// Package watch turns a saved query into a stream of change events: a search
// or a product is fetched again, compared with what the previous check saw,
// and only the differences are reported.
package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
	"github.com/n0nuser/leroymerlin-cli/internal/store"
)

// Fetcher is the part of the client a check needs.
type Fetcher interface {
	Search(ctx context.Context, q leroymerlin.SearchQuery) (*leroymerlin.SearchResult, error)
	ProductByURL(ctx context.Context, refOrURL string) (*leroymerlin.ProductPage, error)
}

// SearchTarget is the stored form of a watched search.
type SearchTarget struct {
	Query leroymerlin.SearchQuery `json:"query"`
}

// ProductTarget is the stored form of a watched product. The URL is saved with
// the reference so later checks need no search.
type ProductTarget struct {
	Ref string `json:"ref"`
	URL string `json:"url"`
}

// Result is what one check learned.
type Result struct {
	Upserts []store.Seen
	Removed []string
	Events  []store.Event
}

// productEvent is the payload an event carries.
type productEvent struct {
	Ref         string             `json:"ref"`
	Name        string             `json:"name,omitempty"`
	URL         string             `json:"url,omitempty"`
	Price       *leroymerlin.Money `json:"price,omitempty"`
	Available   bool               `json:"available"`
	InStock     bool               `json:"in_stock"`
	Store       string             `json:"store,omitempty"`
	Stock       int                `json:"stock,omitempty"`
	StockStatus string             `json:"stock_status,omitempty"`
}

// Check fetches a watch's target, diffs it against the last check and returns
// everything the store should commit. A product that was not in the previous
// state is reported as new; a baseline passes an empty state and ignores the
// events instead of reporting the whole catalogue.
func Check(ctx context.Context, f Fetcher, w store.Watch, seen map[string]store.Seen) (Result, error) {
	switch w.Kind {
	case store.KindSearch:
		var t SearchTarget
		if err := json.Unmarshal(w.Target, &t); err != nil {
			return Result{}, fmt.Errorf("watch %s has a broken search target: %w", w.Name, err)
		}
		return checkSearch(ctx, f, w, t, seen)
	case store.KindProduct:
		var t ProductTarget
		if err := json.Unmarshal(w.Target, &t); err != nil {
			return Result{}, fmt.Errorf("watch %s has a broken product target: %w", w.Name, err)
		}
		return checkProduct(ctx, f, w, t, seen)
	}
	return Result{}, fmt.Errorf("watch %s has unknown kind %q", w.Name, w.Kind)
}

func checkSearch(ctx context.Context, f Fetcher, w store.Watch, t SearchTarget, seen map[string]store.Seen) (Result, error) {
	pages := t.Query
	pages.Page = 1
	found := map[string]store.Seen{}
	order := []string{}
	for page := 1; page <= max(w.Pages, 1); page++ {
		pages.Page = page
		res, err := f.Search(ctx, pages)
		if err != nil {
			return Result{}, err
		}
		for _, p := range res.Products {
			snap, _ := json.Marshal(p)
			k := seenOf(p, snap)
			if _, dup := found[k.Ref]; !dup {
				order = append(order, k.Ref)
			}
			found[k.Ref] = k
		}
		if res.Page >= res.Pages || len(res.Products) == 0 {
			break
		}
	}
	return diff(w, seen, found, order), nil
}

func checkProduct(ctx context.Context, f Fetcher, w store.Watch, t ProductTarget, seen map[string]store.Seen) (Result, error) {
	p, err := f.ProductByURL(ctx, t.URL)
	if err != nil {
		return Result{}, err
	}
	snap, _ := json.Marshal(p)
	found := map[string]store.Seen{p.Ref: productSeen(p, snap)}
	return diff(w, seen, found, []string{p.Ref}), nil
}

func seenOf(p leroymerlin.Product, snap []byte) store.Seen {
	k := store.Seen{
		Ref: p.Ref, Name: p.Name, URL: p.URL,
		Available: p.Available, InStock: p.Available, Snapshot: snap,
	}
	if p.Price != nil {
		k.Price, k.Currency = p.Price.Amount, p.Price.Currency
	}
	return k
}

func productSeen(p *leroymerlin.ProductPage, snap []byte) store.Seen {
	k := store.Seen{
		Ref: p.Ref, Name: p.Name, URL: p.URL,
		Available: p.Available, InStock: p.InStock, Snapshot: snap,
	}
	if p.Price != nil {
		k.Price, k.Currency = p.Price.Amount, p.Price.Currency
	}
	if p.Store != nil {
		k.Store = p.Store.Name
	}
	for _, d := range p.Deliveries {
		if d.Type != "homeDelivery" {
			k.Stock, k.StockStatus = d.Stock, d.Status
		}
	}
	return k
}

// diff compares the previous state with the current one and writes the events
// for every real change.
func diff(w store.Watch, old, now map[string]store.Seen, order []string) Result {
	res := Result{}
	add := func(kind string, k store.Seen, change map[string]any) {
		ev := store.Event{Type: kind, Watch: w.Name, Profile: w.Profile, At: time.Now().UTC()}
		ev.Product, _ = json.Marshal(eventPayload(k))
		if change != nil {
			ev.Change, _ = json.Marshal(change)
		}
		res.Events = append(res.Events, ev)
	}
	for _, ref := range order {
		k := now[ref]
		res.Upserts = append(res.Upserts, k)
		before, existed := old[ref]
		switch {
		case !existed:
			add("product.new", k, nil)
		default:
			if k.Price != before.Price {
				add("product.price_changed", k, map[string]any{"from": before.Price, "to": k.Price})
			}
			switch {
			case (k.Stock != before.Stock || k.StockStatus != before.StockStatus) && (k.Store != "" || before.Store != ""):
				add("product.stock_changed", k, map[string]any{"from": before.Stock, "to": k.Stock, "from_status": before.StockStatus, "to_status": k.StockStatus})
			case k.Available && !before.Available:
				add("product.in_stock", k, nil)
			case !k.Available && before.Available:
				add("product.out_of_stock", k, nil)
			}
		}
	}
	for ref, k := range old {
		if _, still := now[ref]; !still {
			res.Removed = append(res.Removed, ref)
			ev := store.Event{Type: "product.removed", Watch: w.Name, Profile: w.Profile, At: time.Now().UTC()}
			ev.Product, _ = json.Marshal(eventPayload(k))
			res.Events = append(res.Events, ev)
		}
	}
	return res
}

func eventPayload(k store.Seen) productEvent {
	ev := productEvent{Ref: k.Ref, Name: k.Name, URL: k.URL, Available: k.Available, InStock: k.InStock, Store: k.Store, Stock: k.Stock, StockStatus: k.StockStatus}
	if k.Price > 0 {
		cur := k.Currency
		if cur == "" {
			cur = "EUR"
		}
		ev.Price = &leroymerlin.Money{Amount: k.Price, Currency: cur}
	}
	return ev
}
