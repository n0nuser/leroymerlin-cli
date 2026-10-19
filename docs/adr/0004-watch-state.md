# 0004. Watches live in a local sqlite database

Date: 2026-10-19
Status: accepted

## Context

A watch has to remember what the previous check saw and which changes it has already reported. That state is per profile, has to survive restarts, and must be written atomically: a crash between reporting an event and saving the new state would either lose a change or report it twice.

The project already depends on `modernc.org/sqlite` (pure Go, no cgo) for this kind of state, so a second storage mechanism would add nothing.

## Decision

Watches, the last seen state of every product and the events they produced live in `$XDG_DATA_HOME/leroymerlin-cli/state.db`:

```
watches(id, name UNIQUE, profile, kind, target, sinks, interval_sec, pages, created_at, last_check_at, baselined)
seen(watch_id, ref, price, currency, available, in_stock, store_name, stock, snapshot, first_seen, last_seen)
events(id, watch_id, type, at, product, change)
```

- `target` is kind-specific JSON: the search query, or a product reference and its URL.
- One check writes the new `seen` rows, the removals,
  the events and `last_check_at` in a single transaction.
- The database is opened with WAL, a 5 s busy timeout and one connection, so a timer and a manual `watch check` cannot write over each other.
- Deleting a watch cascades to its seen rows and events.

Watches are local. Nothing about them is sent to leroymerlin.es; a check is the same read a search or a product page does.

## Consequences

- A check that dies before committing reports the same change next time; a check that commits never reports it twice.
- `watch events --since` reads history without touching the site.
- The database is not a mirror of the catalogue: it holds only the products under the watches the user created.
- Sessions stay in `config`, not here, so a state database can be deleted without losing the imported browser session.
