// Package store is the local state database: watches, what each watch has
// already seen, and the events it produced. SQLite via modernc (no cgo).
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Kind of watch target. Stored as text so the schema reads well in sqlite3.
const (
	KindSearch  = "search"
	KindProduct = "product"
)

// ErrExists is returned when a watch name is taken.
var ErrExists = errors.New("watch exists")

// ErrNotFound is returned when a watch does not exist.
var ErrNotFound = errors.New("watch not found")

// Watch is the persisted form. Target is kind-specific JSON: search params or
// a product reference.
type Watch struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Profile     string          `json:"profile"`
	Kind        string          `json:"kind"`
	Target      json.RawMessage `json:"target"`
	Sinks       []string        `json:"sinks,omitempty"`
	Interval    time.Duration   `json:"interval"`
	Pages       int             `json:"pages"`
	CreatedAt   time.Time       `json:"created_at"`
	LastCheckAt time.Time       `json:"last_check_at,omitempty"`
}

// Seen is the last known state of one product under one watch.
type Seen struct {
	Ref         string  `json:"ref"`
	Name        string  `json:"name,omitempty"`
	URL         string  `json:"url,omitempty"`
	Price       float64 `json:"price,omitempty"`
	Currency    string  `json:"currency,omitempty"`
	Available   bool    `json:"available"`
	InStock     bool    `json:"in_stock"`
	Store       string  `json:"store,omitempty"`
	Stock       int     `json:"stock,omitempty"`
	StockStatus string  `json:"stock_status,omitempty"`
}

// Price is the amount an event carries.
type Price struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// ProductPayload is the product object inside an event. watch writes it; the
// CLI and the sinks read it, so the shape has one owner.
type ProductPayload struct {
	Ref         string `json:"ref"`
	Name        string `json:"name,omitempty"`
	URL         string `json:"url,omitempty"`
	Price       *Price `json:"price,omitempty"`
	Available   bool   `json:"available"`
	InStock     bool   `json:"in_stock"`
	Store       string `json:"store,omitempty"`
	Stock       int    `json:"stock,omitempty"`
	StockStatus string `json:"stock_status,omitempty"`
}

// Event is one reported change.
type Event struct {
	Type    string          `json:"type"`
	Watch   string          `json:"watch"`
	Profile string          `json:"profile"`
	At      time.Time       `json:"at"`
	Product json.RawMessage `json:"product,omitempty"`
	Change  json.RawMessage `json:"change,omitempty"`
}

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS watches (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  profile TEXT NOT NULL,
  kind TEXT NOT NULL,
  target TEXT NOT NULL,
  sinks TEXT NOT NULL DEFAULT '[]',
  interval_sec INTEGER NOT NULL,
  pages INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  last_check_at TEXT
);
CREATE TABLE IF NOT EXISTS seen (
  watch_id INTEGER NOT NULL REFERENCES watches(id) ON DELETE CASCADE,
  ref TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL DEFAULT '',
  price REAL NOT NULL DEFAULT 0,
  currency TEXT NOT NULL DEFAULT '',
  available INTEGER NOT NULL DEFAULT 0,
  in_stock INTEGER NOT NULL DEFAULT 0,
  store_name TEXT NOT NULL DEFAULT '',
  stock INTEGER NOT NULL DEFAULT 0,
  stock_status TEXT NOT NULL DEFAULT '',
  first_seen TEXT NOT NULL,
  last_seen TEXT NOT NULL,
  PRIMARY KEY (watch_id, ref)
);
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY,
  watch_id INTEGER NOT NULL REFERENCES watches(id) ON DELETE CASCADE,
  type TEXT NOT NULL,
  at TEXT NOT NULL,
  product TEXT,
  change TEXT
);
CREATE INDEX IF NOT EXISTS events_watch_at ON events(watch_id, at);
`

// Open opens (or creates) the state database.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	// One writer at a time keeps a scheduled check and a manual one from
	// tripping over each other.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init state db: %w", err)
	}
	// Databases created before the column existed get it added in place.
	_, _ = db.Exec(`ALTER TABLE seen ADD COLUMN stock_status TEXT NOT NULL DEFAULT ''`)
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// AddWatch inserts a watch; a taken name is ErrExists.
func (s *Store) AddWatch(ctx context.Context, w Watch) (Watch, error) {
	if w.Interval <= 0 {
		w.Interval = 5 * time.Minute
	}
	if w.Pages <= 0 {
		w.Pages = 1
	}
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now().UTC()
	}
	raw, err := json.Marshal(w.Sinks)
	if err != nil {
		return Watch{}, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO watches (name, profile, kind, target, sinks, interval_sec, pages, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		w.Name, w.Profile, w.Kind, string(w.Target), string(raw), int64(w.Interval.Seconds()), w.Pages,
		w.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		if isUnique(err) {
			return Watch{}, ErrExists
		}
		return Watch{}, err
	}
	w.ID, _ = res.LastInsertId()
	return w, nil
}

// Watches lists watches; an empty profile lists every profile's.
func (s *Store) Watches(ctx context.Context, profile string) ([]Watch, error) {
	q := `SELECT id, name, profile, kind, target, sinks, interval_sec, pages, created_at, last_check_at FROM watches`
	var args []any
	if profile != "" {
		q += ` WHERE profile = ?`
		args = append(args, profile)
	}
	q += ` ORDER BY name`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Watch
	for rows.Next() {
		w, err := scanWatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// WatchByName loads one watch.
func (s *Store) WatchByName(ctx context.Context, name string) (Watch, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, profile, kind, target, sinks, interval_sec, pages, created_at, last_check_at FROM watches WHERE name = ?`, name)
	return scanWatch(row)
}

// RemoveWatch deletes a watch and everything it produced.
func (s *Store) RemoveWatch(ctx context.Context, name string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM watches WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Seen is the last known state of everything under one watch.
func (s *Store) Seen(ctx context.Context, watchID int64) (map[string]Seen, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ref, name, url, price, currency, available, in_stock, store_name, stock, stock_status FROM seen WHERE watch_id = ?`, watchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Seen{}
	for rows.Next() {
		var k Seen
		if err := rows.Scan(&k.Ref, &k.Name, &k.URL, &k.Price, &k.Currency, &k.Available, &k.InStock, &k.Store, &k.Stock, &k.StockStatus); err != nil {
			return nil, err
		}
		out[k.Ref] = k
	}
	return out, rows.Err()
}

// CommitCheck writes a check's new state and its events in one transaction, so
// a crash never reports the same change twice.
func (s *Store) CommitCheck(ctx context.Context, watchID int64, upserts []Seen, removed []string, events []Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, k := range upserts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO seen (watch_id, ref, name, url, price, currency, available, in_stock, store_name, stock, stock_status, first_seen, last_seen)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(watch_id, ref) DO UPDATE SET name=excluded.name, url=excluded.url, price=excluded.price,
			  currency=excluded.currency, available=excluded.available, in_stock=excluded.in_stock,
			  store_name=excluded.store_name, stock=excluded.stock, stock_status=excluded.stock_status,
			  last_seen=excluded.last_seen`,
			watchID, k.Ref, k.Name, k.URL, k.Price, k.Currency, k.Available, k.InStock, k.Store, k.Stock, k.StockStatus, now, now); err != nil {
			return err
		}
	}
	for _, ref := range removed {
		if _, err := tx.ExecContext(ctx, `DELETE FROM seen WHERE watch_id = ? AND ref = ?`, watchID, ref); err != nil {
			return err
		}
	}
	for _, ev := range events {
		if _, err := tx.ExecContext(ctx, `INSERT INTO events (watch_id, type, at, product, change) VALUES (?, ?, ?, ?, ?)`,
			watchID, ev.Type, ev.At.UTC().Format(time.RFC3339Nano), string(ev.Product), string(ev.Change)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE watches SET last_check_at = ? WHERE id = ?`, now, watchID); err != nil {
		return err
	}
	return tx.Commit()
}

// Events returns stored events, newest last, optionally for one watch name and
// only after since.
func (s *Store) Events(ctx context.Context, name string, since time.Time, limit int) ([]Event, error) {
	q := `SELECT e.type, w.name, w.profile, e.at, e.product, e.change
		FROM events e JOIN watches w ON w.id = e.watch_id`
	var args []any
	var where []string
	if name != "" {
		where = append(where, `w.name = ?`)
		args = append(args, name)
	}
	if !since.IsZero() {
		where = append(where, `e.at >= ?`)
		args = append(args, since.UTC().Format(time.RFC3339Nano))
	}
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	q += ` ORDER BY e.at DESC, e.id DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var ev Event
		var at, product, change string
		if err := rows.Scan(&ev.Type, &ev.Watch, &ev.Profile, &at, &product, &change); err != nil {
			return nil, err
		}
		ev.At, _ = time.Parse(time.RFC3339Nano, at)
		if product != "" {
			ev.Product = json.RawMessage(product)
		}
		if change != "" {
			ev.Change = json.RawMessage(change)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

func scanWatch(r rowScanner) (Watch, error) {
	var w Watch
	var target, sinks, created string
	var last sql.NullString
	var interval int64
	if err := r.Scan(&w.ID, &w.Name, &w.Profile, &w.Kind, &target, &sinks, &interval, &w.Pages, &created, &last); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Watch{}, ErrNotFound
		}
		return Watch{}, err
	}
	w.Target = json.RawMessage(target)
	_ = json.Unmarshal([]byte(sinks), &w.Sinks)
	w.Interval = time.Duration(interval) * time.Second
	w.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if last.Valid && last.String != "" {
		w.LastCheckAt, _ = time.Parse(time.RFC3339Nano, last.String)
	}
	return w, nil
}

func isUnique(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "constraint failed"))
}
