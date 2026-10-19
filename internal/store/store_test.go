package store

import (
	"encoding/json"
	"errors"
	"os"
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
	if _, err := st.WatchByName(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing watch error = %v, want ErrNotFound", err)
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
	upsert := Seen{Ref: "81866477", Name: "Cemento", URL: "https://example/1.html", Price: 3.94, Currency: "EUR", Available: true, InStock: true}
	ev := Event{Type: "product.price_changed", Watch: "pegoland", Profile: "default", At: time.Now().UTC(),
		Product: json.RawMessage(`{"ref":"81866477"}`), Change: json.RawMessage(`{"from":5,"to":3.94}`)}
	if err := st.CommitCheck(ctx, w.ID, []Seen{upsert}, nil, []Event{ev}); err != nil {
		t.Fatal(err)
	}
	seen, err = st.Seen(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := seen["81866477"]; got.Price != 3.94 || !got.Available {
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

func TestOpenFailsWhenTheParentIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, []byte("x"), 0o600)
	if _, err := Open(filepath.Join(file, "state.db")); err == nil {
		t.Fatal("a database under a regular file opened")
	}
}

func TestAddWatchDefaults(t *testing.T) {
	ctx := t.Context()
	st := open(t)
	w, err := st.AddWatch(ctx, Watch{Name: "defaults", Profile: "default", Kind: KindProduct, Target: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if w.Interval != 5*time.Minute || w.Pages != 1 || w.CreatedAt.IsZero() {
		t.Fatalf("defaults = %+v", w)
	}
	ws, _ := st.Watches(ctx, "")
	if len(ws) != 1 || ws[0].Name != "defaults" {
		t.Fatalf("all profiles = %+v", ws)
	}
	if _, err := st.Watches(ctx, "other"); err != nil {
		t.Fatal(err)
	}
}

func TestCommitCheckUpsertsAndEventsFilter(t *testing.T) {
	ctx := t.Context()
	st := open(t)
	w, _ := st.AddWatch(ctx, Watch{Name: "w", Profile: "default", Kind: KindProduct, Target: json.RawMessage(`{}`)})
	first := Seen{Ref: "1", Name: "one", Price: 1, Currency: "EUR"}
	if err := st.CommitCheck(ctx, w.ID, []Seen{first}, nil, []Event{{Type: "product.new", Watch: "w", Profile: "default", At: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	updated := Seen{Ref: "1", Name: "one changed", Price: 2, Currency: "EUR"}
	if err := st.CommitCheck(ctx, w.ID, []Seen{updated}, nil, nil); err != nil {
		t.Fatal(err)
	}
	seen, _ := st.Seen(ctx, w.ID)
	if got := seen["1"]; got.Price != 2 || got.Name != "one changed" {
		t.Fatalf("upsert = %+v", got)
	}
	all, err := st.Events(ctx, "w", time.Time{}, 10)
	if err != nil || len(all) != 1 {
		t.Fatalf("events = %+v %v", all, err)
	}
	if _, err := st.Events(ctx, "other", time.Time{}, 0); err != nil {
		t.Fatal(err)
	}
	if limited, _ := st.Events(ctx, "", time.Time{}, 1); len(limited) != 1 {
		t.Fatalf("limit = %+v", limited)
	}
	missing, err := st.WatchByName(ctx, "nope")
	if !errors.Is(err, ErrNotFound) || missing.Name != "" {
		t.Fatalf("missing = %+v %v", missing, err)
	}
}
