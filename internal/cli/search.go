package cli

import (
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
	"github.com/n0nuser/leroymerlin-cli/internal/output"
)

// searchFlags are shared by `search` and `watch add search`.
type searchFlags struct {
	category string
	brands   []string
	minPrice string
	maxPrice string
	inStock  bool
	soldByLM bool
	sort     string
	page     int
	limit    int
}

// bindSearchFlags defines the search flags and returns their own set, so a
// caller can ask which of exactly these flags was given.
func (a *App) bindSearchFlags(cmd *cobra.Command, f *searchFlags) *pflag.FlagSet {
	fl := pflag.NewFlagSet("search", pflag.ContinueOnError)
	fl.StringVar(&f.category, "category", "", "browse one category path instead of a text search (see `leroymerlin category list`)")
	fl.StringSliceVar(&f.brands, "brand", nil, "brand filter, repeatable (matches the site's Marca filter)")
	fl.StringVar(&f.minPrice, "min-price", "", "minimum price in euro")
	fl.StringVar(&f.maxPrice, "max-price", "", "maximum price in euro")
	fl.BoolVar(&f.inStock, "in-stock", false, "only products the site can add to the cart")
	fl.BoolVar(&f.soldByLM, "sold-by-leroy-merlin", false, "only products the site sells itself")
	fl.StringVar(&f.sort, "sort", "relevance", "order: relevance, price-asc, price-desc")
	fl.IntVar(&f.page, "page", 1, "result page")
	fl.IntVar(&f.limit, "limit", 0, "keep at most N products from the page (0: all)")
	cmd.Flags().AddFlagSet(fl)
	_ = cmd.RegisterFlagCompletionFunc("sort", cobra.FixedCompletions(leroymerlin.SortCodes, cobra.ShellCompDirectiveNoFileComp))
	return fl
}

// searchQuery validates the flags and builds the query.
func searchQuery(keywords []string, f searchFlags) (leroymerlin.SearchQuery, error) {
	q := leroymerlin.SearchQuery{
		Keywords: strings.Join(keywords, " "), Category: strings.Trim(f.category, "/"),
		Brands: f.brands, MinPrice: f.minPrice, MaxPrice: f.maxPrice,
		SoldByLeroyMerlin: f.soldByLM, Sort: f.sort, Page: f.page,
		InStock: f.inStock, Limit: f.limit,
	}
	switch {
	case q.Keywords == "" && q.Category == "":
		return q, output.Usagef("give keywords, --category PATH, or both")
	case q.Keywords != "" && q.Category != "":
		return q, output.Usagef("--category browses a branch of the tree; drop the keywords or the flag")
	}
	if q.Sort == "" {
		q.Sort = "relevance"
	}
	if !slices.Contains(leroymerlin.SortCodes, q.Sort) {
		return q, output.Usagef("--sort must be one of %s", strings.Join(leroymerlin.SortCodes, ", "))
	}
	if f.page < 1 {
		return q, output.Usagef("--page starts at 1")
	}
	if f.limit < 0 {
		return q, output.Usagef("--limit cannot be negative")
	}
	lo, err := price("--min-price", q.MinPrice)
	if err != nil {
		return q, err
	}
	hi, err := price("--max-price", q.MaxPrice)
	if err != nil {
		return q, err
	}
	if q.MinPrice != "" && q.MaxPrice != "" && lo > hi {
		return q, output.Usagef("--min-price is above --max-price")
	}
	return q, nil
}

// price validates a euro amount typed as a flag. Empty means "not given".
func price(flag, v string) (float64, error) {
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0, output.Usagef("%s %q is not a price", flag, v)
	}
	return n, nil
}

// searchResultView is what `search` prints. Products may be trimmed by --limit
// or --in-stock, so Shown can be smaller than the page's product count.
type searchResultView struct {
	leroymerlin.SearchResult
	Shown int `json:"shown"`
}

func (v searchResultView) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(v.Products))
	for _, p := range v.Products {
		price := "-"
		if p.Price != nil {
			price = fmt.Sprintf("%.2f %s", p.Price.Amount, p.Price.Currency)
		}
		stock := ""
		if !p.Available {
			stock = output.Dim("out", color)
		}
		rows = append(rows, []string{p.Ref, price, stock, output.Truncate(p.Brand, 12), output.Truncate(p.Name, 52), output.Dim(p.URL, color)})
	}
	head := fmt.Sprintf("%d products, page %d/%d, %s", v.Total, v.Page, v.Pages, v.Sort)
	if v.Query != "" {
		head = fmt.Sprintf("%q: %s", v.Query, head)
	}
	fmt.Fprintln(w, head)
	output.Table(w, color, []string{"REF", "PRICE", "", "BRAND", "NAME", "URL"}, rows)
	if v.Shown < v.Total && v.Pages > 1 {
		fmt.Fprintln(w, output.Dim("more pages: --page 2", color))
	}
}

func (a *App) searchCmd() *cobra.Command {
	var f searchFlags
	cmd := &cobra.Command{
		Use:   "search [keywords...]",
		Short: "Search the catalogue, or browse one category",
		Long: `Search the catalogue, or browse one category.

Filters are the ones the site offers: --brand (its Marca facet), --min-price
and --max-price, --sold-by-leroy-merlin, and --sort. --in-stock keeps only
what the site says it can add to the cart. --category browses a branch of the
tree from the public sitemap, instead of a text search.

Results are server-rendered; the site loads the tail of a page lazily, so a
page reports the total the site gives and the products it embedded.

Examples:
  leroymerlin search "cemento cola" --format pretty
  leroymerlin search "mortero" --brand SIKA --max-price 30 --sort price-asc
  leroymerlin search --category construccion/cementos-morteros-y-yesos/cementos --in-stock`,
		RunE: func(cmd *cobra.Command, args []string) error {
			q, err := searchQuery(args, f)
			if err != nil {
				return err
			}
			res, err := a.Client.Search(cmd.Context(), q)
			if err != nil {
				return err
			}
			res.Category = q.Category
			return a.Printer.Print(searchResultView{SearchResult: *res, Shown: len(res.Products)})
		},
	}
	a.bindSearchFlags(cmd, &f)
	return cmd
}
