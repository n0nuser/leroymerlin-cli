package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/n0nuser/leroymerlin-cli/internal/output"
	"github.com/n0nuser/leroymerlin-cli/internal/store"
	"github.com/n0nuser/leroymerlin-cli/internal/watch"
)

type watchView struct {
	store.Watch
	Seen int `json:"seen,omitempty"`
}

func (v watchView) Pretty(w io.Writer, color bool) {
	fmt.Fprintf(w, "watch %s (%s) saved\n", v.Name, describeTarget(v.Watch))
}

func describeTarget(w store.Watch) string {
	switch w.Kind {
	case store.KindSearch:
		var t watch.SearchTarget
		if json.Unmarshal(w.Target, &t) == nil {
			if t.Query.Keywords != "" {
				return fmt.Sprintf("search %q", t.Query.Keywords)
			}
			if t.Query.Category != "" {
				return "category " + t.Query.Category
			}
		}
	case store.KindProduct:
		var t watch.ProductTarget
		if json.Unmarshal(w.Target, &t) == nil {
			return "product " + t.Ref
		}
	}
	return ""
}

type watchList []store.Watch

func (l watchList) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(l))
	for _, wt := range l {
		last := "never"
		if !wt.LastCheckAt.IsZero() {
			last = wt.LastCheckAt.Format(time.RFC3339)
		}
		rows = append(rows, []string{wt.Name, wt.Kind, output.Truncate(describeTarget(wt), 40), wt.Interval.String(), strings.Join(wt.Sinks, ","), last, output.Dim(wt.Profile, color)})
	}
	output.Table(w, color, []string{"WATCH", "KIND", "TARGET", "EVERY", "SINKS", "CHECKED", "PROFILE"}, rows)
}

// eventList prints events as one JSON document, or one per line with jsonl.
type eventList []store.Event

func (l eventList) Pretty(w io.Writer, color bool) {
	if len(l) == 0 {
		fmt.Fprintln(w, output.Dim("no changes", color))
		return
	}
	rows := make([][]string, 0, len(l))
	for _, ev := range l {
		var p store.ProductPayload
		_ = json.Unmarshal(ev.Product, &p)
		price := "-"
		if p.Price != nil {
			price = fmt.Sprintf("%.2f %s", p.Price.Amount, p.Price.Currency)
		}
		change := ""
		if len(ev.Change) > 0 {
			change = string(ev.Change)
		}
		rows = append(rows, []string{ev.At.Format("15:04:05"), ev.Type, ev.Watch, p.Ref, price, output.Truncate(p.Name, 40), output.Truncate(change, 24)})
	}
	output.Table(w, color, []string{"AT", "TYPE", "WATCH", "REF", "PRICE", "NAME", "CHANGE"}, rows)
}
