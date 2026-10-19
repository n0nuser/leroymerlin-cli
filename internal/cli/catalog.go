package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
	"github.com/n0nuser/leroymerlin-cli/internal/output"
)

type storeList []leroymerlin.Store

func (l storeList) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(l))
	for _, s := range l {
		kind := "store"
		if s.Compact {
			kind = "compact"
		}
		rows = append(rows, []string{s.Slug, s.Name, kind})
	}
	output.Table(w, color, []string{"SLUG", "NAME", "TYPE"}, rows)
	fmt.Fprintln(w, output.Dim(fmt.Sprintf("%d stores", len(l)), color))
}

func (a *App) storeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Leroy Merlin stores in Spain",
		Long: `Leroy Merlin stores in Spain.

The list comes from the site's public store sitemap and needs no session.
Stock is always for one store: the one selected in the browser session the
profile was imported from. Use one profile per store to compare stores.`,
	}
	var query string
	var compact bool
	list := &cobra.Command{
		Use:   "list",
		Short: "List stores (from the public sitemap, no session needed)",
		Long: `List every store from the public store sitemap, sorted by name.

Examples:
  leroymerlin store list --format pretty
  leroymerlin store list --query madrid
  leroymerlin store list --format jsonl | jq -r .slug`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			stores, err := a.Client.Stores(cmd.Context())
			if err != nil {
				return err
			}
			q := fold(query)
			out := storeList{}
			for _, s := range stores {
				if compact && !s.Compact {
					continue
				}
				if q != "" && !strings.Contains(fold(s.Name+" "+s.Slug), q) {
					continue
				}
				out = append(out, s)
			}
			return a.Printer.Print(out)
		},
	}
	list.Flags().StringVarP(&query, "query", "q", "", "only stores whose name or slug contains this (accents ignored)")
	list.Flags().BoolVar(&compact, "compact", false, "only the smaller \"Compact\" format stores")
	current := &cobra.Command{
		Use:   "current",
		Short: "The store selected in the session (stock follows it)",
		Long: `Show the store the imported browser session has selected ("Elegir mi tienda").

Stock is always for that store. Without a selection the site reports online
availability only; choose a store in the browser the session came from and
import the cookies again.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireSession(); err != nil {
				return err
			}
			s, err := a.Client.CurrentStore(cmd.Context())
			if err != nil {
				return err
			}
			if s == nil {
				return &leroymerlin.Error{Kind: leroymerlin.KindNotFound,
					Msg: "this session has no store selected. Open leroymerlin.es in the browser it came from, choose a store (\"Elegir mi tienda\"), and import the cookies again"}
			}
			return a.Printer.Print(storeRefView{*s})
		},
	}
	cmd.AddCommand(list, current)
	return cmd
}

type categoryList []leroymerlin.Category

func (l categoryList) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(l))
	for _, c := range l {
		rows = append(rows, []string{strings.Repeat("  ", c.Depth-1) + c.Name, c.Path})
	}
	output.Table(w, color, []string{"NAME", "PATH"}, rows)
}

func (a *App) categoryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "category", Short: "The product category tree"}
	var parent, query string
	var depth int
	list := &cobra.Command{
		Use:   "list",
		Short: "List categories (from the public sitemap, no session needed)",
		Long: `List product categories from the public category sitemap, parents first.
PATH is the part of the category URL after /productos/.

Examples:
  leroymerlin category list --depth 1 --format pretty
  leroymerlin category list --parent construccion --depth 2
  leroymerlin category list --query cemento`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cats, err := a.Client.Categories(cmd.Context())
			if err != nil {
				return err
			}
			parent = strings.Trim(parent, "/")
			q := fold(query)
			base := 0
			if parent != "" {
				base = strings.Count(parent, "/") + 1
			}
			out := categoryList{}
			for _, c := range cats {
				if parent != "" && !strings.HasPrefix(c.Path, parent+"/") {
					continue
				}
				if depth > 0 && c.Depth > base+depth {
					continue
				}
				if q != "" && !strings.Contains(fold(c.Name+" "+c.Path), q) {
					continue
				}
				out = append(out, c)
			}
			if parent != "" && len(out) == 0 && q == "" {
				return &leroymerlin.Error{Kind: leroymerlin.KindNotFound, Msg: fmt.Sprintf("no category under %q. See `leroymerlin category list --depth 1`", parent)}
			}
			return a.Printer.Print(out)
		},
	}
	list.Flags().StringVar(&parent, "parent", "", "only categories below this path")
	list.Flags().IntVar(&depth, "depth", 0, "levels below --parent (or the top) to include; 0 for all")
	list.Flags().StringVarP(&query, "query", "q", "", "only categories whose name or path contains this (accents ignored)")
	cmd.AddCommand(list)
	return cmd
}

// storeRefView pretty-prints the store selected in the session.
type storeRefView struct {
	leroymerlin.StoreRef
}

func (s storeRefView) Pretty(w io.Writer, color bool) {
	output.Table(w, color, nil, [][]string{{"id", s.ID}, {"name", s.Name}})
}

// fold lowercases and strips Spanish accents so "almeria" finds "Almería".
func fold(s string) string {
	return strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n", "à", "a", "è", "e", "ò", "o", "ç", "c", "ï", "i").Replace(strings.ToLower(s))
}
