package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestWatchLifecycle(t *testing.T) {
	ctx := t.Context()
	st := open(t)
	target := json.RawMessage(`{"query":{"keywords":"cemento"}}`)
	w, err := st.AddWatch(ctx, Watch{Name: "cola", Profile: "default", Kind: KindSearch, Target: target, Interval: 2 * time.Minute, Pages: 2})
	if err != nil {
		t.Fatal(err)
	}
	if w.ID == 0 || w.Interval != 2*time.Minute {
		t.Fatalf("watch = %+v", w)
	}
	if _, err := st.AddWatch(ctx, Watch{Name: "cola", Profile: "default", Kind: KindSearch, Target: target}); err != ErrExists {
		t.Fatalf("duplicate name = %v, want ErrExists", err)
	}
	ws, err := st.Watches(ctx, "default")
	if err != nil || len(ws) != 1 || ws[0].Name != "cola" {
		t.Fatalf("Watches = %+v, %v", ws, err)
	}
	if ws, _ := st.Watches(ctx, "other"); len(ws) != 0 {
		t.Fatalf("another profile saw %d watches", len(ws))
	}
	got, err := st.WatchByName(ctx, "cola")
	if err != nil || got.ID != w.ID || string(got.Target) != string(target) {
		t.Fatalf("WatchByName = %+v, %v", got, err)
	}
	if _, err := st.WatchByName(ctx, "nope"); err == nil {
		t.Fatal("a missing watch did not error")
	}
	if ok, _ := st.RemoveWatch(ctx, "cola"); !ok {
		t.Fatal("remove reported nothing")
	}
	if ok, _ := st.RemoveWatch(ctx, "cola"); ok {
		t.Fatal("removing twice reported a removal")
	}
}

func TestSeenAndEvents(t *testing.T) {
	ctx := t.Context()
	st := open(t)
	w, err := st.AddWatch(ctx, Watch{Name: "pegoland", Profile: "default", Kind: KindProduct, Target: json.RawMessage(`{"ref":"81866477"}`)})
	if err != nil {
		t.Fatal(err)
	}
	seen, err := st.Seen(ctx, w.ID)
	if err != nil || len(seen) != 0 {
		t.Fatalf("initial Seen = %+v, %v", seen, err)
	}
	upsert := Seen{Ref: "81866477", Name: "Cemento", URL: "https://example/1.html", Price: 3.94, Currency: "EUR", Available: true, InStock: true, Snapshot: json.RawMessage(`{"ref":"81866477"}`)}
	ev := Event{Type: "product.price_changed", Watch: "pegoland", Profile: "default", At: time.Now().UTC(),
		Product: json.RawMessage(`{"ref":"81866477"}`), Change: json.RawMessage(`{"from":5,"to":3.94}`)}
	if err := st.CommitCheck(ctx, w.ID, []Seen{upsert}, nil, []Event{ev}); err != nil {
		t.Fatal(err)
	}
	seen, err = st.Seen(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := seen["81866477"]; got.Price != 3.94 || !got.Available || string(got.Snapshot) != `{"ref":"81866477"}` {
		t.Fatalf("Seen = %+v", got)
	}
	events, err := st.Events(ctx, "pegoland", time.Time{}, 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("Events = %+v, %v", events, err)
	}
	if events[0].Type != "product.price_changed" || events[0].Watch != "pegoland" {
		t.Fatalf("event = %+v", events[0])
	}
	if err := st.CommitCheck(ctx, w.ID, nil, []string{"81866477"}, nil); err != nil {
		t.Fatal(err)
	}
	if seen, _ := st.Seen(ctx, w.ID); len(seen) != 0 {
		t.Fatalf("removal left %+v", seen)
	}
	// Events older than the window are filtered out.
	old, err := st.Events(ctx, "pegoland", time.Now().Add(time.Hour), 0)
	if err != nil || len(old) != 0 {
		t.Fatalf("future window returned %+v, %v", old, err)
	}
}

func TestEventsSurviveWatchRemoval(t *testing.T) {
	ctx := t.Context()
	st := open(t)
	w, err := st.AddWatch(ctx, Watch{Name: "x", Profile: "default", Kind: KindProduct, Target: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.CommitCheck(ctx, w.ID, nil, nil, []Event{{Type: "product.new", Watch: "x", Profile: "default", At: time.Now().UTC()}})
	if ok, _ := st.RemoveWatch(ctx, "x"); !ok {
		t.Fatal("remove failed")
	}
	events, err := st.Events(ctx, "", time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("cascade left %d events", len(events))
	}
}
