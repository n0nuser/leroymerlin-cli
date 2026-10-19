package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
	"github.com/n0nuser/leroymerlin-cli/internal/output"
)

// productView is a product page plus its human rendering.
type productView struct {
	leroymerlin.ProductPage
}

func (p productView) Pretty(w io.Writer, color bool) {
	rows := [][]string{
		{"ref", p.Ref},
		{"name", p.Name},
	}
	add := func(k, v string) {
		if v != "" {
			rows = append(rows, []string{k, v})
		}
	}
	add("brand", p.Brand)
	if p.Price != nil {
		add("price", fmt.Sprintf("%.2f %s", p.Price.Amount, p.Price.Currency))
	}
	add("seller", p.Seller)
	add("available", fmt.Sprint(p.Available))
	if p.Store != nil {
		add("store", fmt.Sprintf("%s (%s)", p.Store.Name, p.Store.ID))
	} else {
		add("store", output.Dim("none selected in this session", color))
	}
	for _, d := range p.Deliveries {
		line := d.Type
		if d.Time != "" {
			line += ", " + d.Time
		}
		if d.Stock > 0 {
			line += fmt.Sprintf(", %d in stock", d.Stock)
		}
		add("delivery", line)
	}
	add("url", p.URL)
	output.Table(w, color, nil, rows)
}

// stockView is the answer to "will my store have it": the selected store when
// the session has one, and every way the site offers the product.
type stockView struct {
	Ref        string                 `json:"ref"`
	Name       string                 `json:"name,omitempty"`
	Store      *leroymerlin.StoreRef  `json:"store"`
	Online     bool                   `json:"online"`
	Deliveries []leroymerlin.Delivery `json:"deliveries,omitempty"`
}

func (v stockView) Pretty(w io.Writer, color bool) {
	rows := [][]string{
		{"ref", v.Ref},
		{"name", v.Name},
	}
	switch {
	case v.Store == nil:
		rows = append(rows, []string{"store", output.Dim("none selected in this session: choose a store in your browser and import the cookies again", color)})
	default:
		rows = append(rows, []string{"store", fmt.Sprintf("%s (%s)", v.Store.Name, v.Store.ID)})
	}
	rows = append(rows, []string{"online", fmt.Sprint(v.Online)})
	for _, d := range v.Deliveries {
		line := d.Type
		if d.Time != "" {
			line += ", " + d.Time
		}
		if d.Stock > 0 {
			line += fmt.Sprintf(", %d available", d.Stock)
		}
		if d.Price != nil {
			line += fmt.Sprintf(", %.2f %s", d.Price.Amount, d.Price.Currency)
		}
		rows = append(rows, []string{"delivery", line})
	}
	output.Table(w, color, nil, rows)
}

func (a *App) productCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "product",
		Aliases: []string{"item"},
		Short:   "Read one product and its stock",
		Long: `Read one product and its stock.

REF is a product reference (the 8-digit number at the end of a product URL) or
a full product URL. A bare reference needs one search first, to learn the URL
the site uses.

Stock follows the store selected in the browser session ("Elegir mi tienda").
Without one, the site reports online availability and home delivery only.`,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "show REF",
			Short: "Read one product",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return a.showProduct(cmd, args[0])
			},
		},
		&cobra.Command{
			Use:   "stock REF",
			Short: "Whether the product is available, and where",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				p, err := a.loadProduct(cmd, args[0])
				if err != nil {
					return err
				}
				return a.Printer.Print(stockView{Ref: p.Ref, Name: p.Name, Store: p.Store, Online: p.Available, Deliveries: p.Deliveries})
			},
		},
	)
	return cmd
}

func (a *App) showProduct(cmd *cobra.Command, ref string) error {
	p, err := a.loadProduct(cmd, ref)
	if err != nil {
		return err
	}
	return a.Printer.Print(productView{*p})
}

func (a *App) loadProduct(cmd *cobra.Command, refOrURL string) (*leroymerlin.ProductPage, error) {
	if err := a.requireSession(); err != nil {
		return nil, err
	}
	return a.Client.Product(cmd.Context(), refOrURL)
}
