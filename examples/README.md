# examples

ready-to-run recipes for the real commands. every one prints json unless you pass
`--format pretty`. anything that reads the catalogue needs a session first:

```bash
leroymerlin auth harvest     # or: leroymerlin auth login --cookies FILE
leroymerlin doctor           # confirms the storefront accepts the session
```

## search

```bash
# the plain search
leroymerlin search "cemento cola" --format pretty

# one brand, under 30 €, cheapest first, only what can be added to the cart
leroymerlin search "mortero" --brand SIKA --max-price 30 --sort price-asc --in-stock

# browse a branch of the tree instead of a text search
leroymerlin search --category construccion/cementos-morteros-y-yesos/cementos --limit 10

# page 2, only what leroymerlin itself sells
leroymerlin search "taladro" --page 2 --sold-by-leroy-merlin
```

## product and stock

```bash
# one product, by reference or URL
leroymerlin product show 81866477 --format pretty

# where it is available: store, online, pickup and delivery
leroymerlin product stock 81866477 --format pretty

# script it
leroymerlin product stock 81866477 --format jsonl | jq -r '.deliveries[] | "\(.type) \(.status) (\(.stock))"'
```

## store and category

```bash
leroymerlin store list --query madrid --format pretty
leroymerlin store current --format pretty
leroymerlin category list --parent construccion --depth 1 --format pretty
```

## watch

```bash
# save a search and a product
leroymerlin watch add search "cemento cola" --max-price 20 --name cola
leroymerlin watch add product 81866477 --name pegoland

# the first check reports changes only (the add baselined them)
leroymerlin watch check --all --format pretty

# feed the events into a pipe: one json object per line
leroymerlin watch check --all --format jsonl | jq -r 'select(.type=="product.price_changed") | .product.url'

# history for a watch
leroymerlin watch events cola --since 24h --format pretty
```

`watch run --all` loops forever at the shortest interval of the selected watches;
with [examples/config.toml](config.toml) that is every 5 minutes.

## agents

```bash
leroymerlin mcp install claude-code     # also: codex, cursor
leroymerlin skills get leroymerlin-usage
```

## sharing the result

```bash
# a table for a person
leroymerlin search "cemento cola" --format pretty

# one result per line for a script
leroymerlin store list --format jsonl | jq -r .slug
```
