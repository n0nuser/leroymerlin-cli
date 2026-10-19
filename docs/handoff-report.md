# Handoff report: leroymerlin-cli

Date: 2026-10-19. Branch: `feat/initial`.

## Summary

The cli now covers the read-only surface the project exists for: stores, categories, search with the site's own filters, one product and its availability, per-store pickup stock, and watches that report price and stock changes as events, with optional sinks. The previous "no store selected" caveat is closed.

What landed in this round:

- **Docs site merged.** `docs-site/` now carries the app shell, guides and theme from `feat/docs-site`, and `cmd/docsgen` owns `docs-site/content/docs/reference`. `npm install && npm run build` succeeds. Planned-command placeholders were dropped because the generator deletes anything it does not write.
- **Session harvest.** `auth harvest` opens a real Chrome window (headed), attaches to a running DevTools endpoint when one listens, waits for a DataDome challenge to clear, and reads the cookies and the browser's own user agent over CDP into the existing `ParseExport` + `SaveSession` path. It keeps a profile under `$XDG_DATA_HOME/leroymerlin-cli/chrome`, so a solved challenge survives. This supersedes manual `auth login --cookies` for most interactive users; `auth login` stays for scripts, CI and machines without a display (ADR 0005).
- **Session import.** `auth login --cookies` accepts the `{"userAgent","cookies"}` wrapper a `document.cookie` script writes, keeps the browser's user agent, and therefore replays the cookies with the Chrome that earned them. The plain array and Cookie-Editor wrapper still work.
- **Live access.** Two sessions were imported. The first had no store selected and passed `doctor` immediately. The second (store 037, Salamanca) was a plain cookie array with no user agent: replayed as the default Chrome 133 it was challenged, and `--user-agent` with the browser's Chrome 155 string made the same cookies work. `store current` then resolved `037 / Salamanca`.
- **Captured fixtures.** Nine real responses were captured through the cli transport, one request at a time, and sanitized (cookies, `ktx` tokens and context ids removed):
  - `search_cemento_cola.html` (relevance, page 1)
  - `search_cemento_cola_filtered.html` (price 5–20, price-asc)
  - `search_cemento_cola_page2.html`
  - `category_cementos.html` (leaf category)
  - `product_81866477.html` (available, home delivery only)
  - `product_81866477_store.html` (store 037 selected: pickup + home delivery)
  - `product_81944344.html` (not available)
  - `home_nostore.html` (a session with no store selected)
  - `home_salamanca.html` (store 037 selected)
- **Search.** `/search?q=` and `/productos/<path>/` are parsed from the `guidance-ssr-data` island and the `cdl_products_list` islands. Exposed filters: `--brand` (the site's Marca facet), `--min-price` / `--max-price` (its range), `--sold-by-leroy-merlin` (its vendor toggle), `--sort relevance|price-asc|price-desc`, `--page`, `--category`, `--in-stock` (applied to the embedded slice), `--limit`.
- **Product and stock.** `product show REF` reads `jsonld_PRODUCT` plus the offer island; `product stock REF` narrows to store, online availability and deliveries. With a store selected the page adds a `storeDelivery` pickup row (`status` `ONSITE`/`ONORDER`/`OUTOFSTOCK`, price 0 = free) beside home delivery. `item` is an alias. A bare reference costs one request because the site redirects `/search?q=<ref>` to the product page.
- **Store.** `store current` reports the selected store (`037 / Salamanca` live) or exits 4 when the session has none; the home header's `data-store-id` fallback was needed for the id.
- **Watch.** `watch add search|product`, `list`, `remove`, `check`, `run`, `events`. State lives in a local sqlite database (`internal/store`), the diff lives in `internal/watch`, and a check commits state + events in one transaction. Event types: `product.new`, `product.price_changed`, `product.in_stock`, `product.out_of_stock`, `product.stock_changed` (count or status), `product.removed`.
- **Sinks.** `sink list|test`, with `ntfy`, `webhook` and `exec` from `[sinks.NAME]` in `config.toml`, delivered best effort.
- **MCP.** `search`, `product_show`, `product_stock` and `store_current` join `store_list`, `category_list` and `doctor`.
- **Docs.** `docs/cli-spec.md`, `docs/leroymerlin-api-discovery.md`, `docs/review.md`, `README.md`, `AGENTS.md`, the agent skill and a new ADR 0004 describe all of it. The command reference is regenerated; `make docs-check` is clean.

## Verification

- `go build ./...`, `go vet ./...` and `go test ./... -count=1`: clean.
- `make check` (vet, staticcheck, tests, gofmt): clean.
- `make docs-check`: clean.
- `cd docs-site && npm install && npm run build`: the static export succeeds and includes the new `search`, `product`, `watch` and `sink` reference pages.
- 85 test functions, 88 runs including subtests. Parser tests are pinned to the real captures, including the store-selected product and home pages; `watch`/`store` have diff and transaction tests; `harvest` tests the cookie mapping, host matching, Chrome resolution and the challenge check without launching a browser; the CLI tests use `httptest` and never talk to leroymerlin.es.
- Live smoke test: `search "cemento cola"`, a filtered/sorted search, page 2, a category, two products, `store current` (`037 / Salamanca`), `product stock` with pickup rows, and a product watch add/check/remove. All at the normal 1.5 s pace.
- Live harvest smoke test against a fresh Chrome profile: the browser launched headed, the page cleared and the harvest stored six cookies with the browser's Chrome 155 user agent. `doctor` then challenged that fresh cookie; a later run in the same profile hit a challenge and waited for a human (none present). The port's waiting path is exercised; a human-cleared challenge is what earns a cookie the CLI replay is trusted with.

## Open caveats

1. **Pickup `ONORDER` is verified; `ONSITE` and `OUTOFSTOCK` are not captured.** Store 037's page states `storeDelivery` with `stockStatus ONORDER` (order to the store, 2 opening days) beside `homeDelivery ONSITE`. The parser carries the raw status and the renderer maps all three; only the two missing fixture states are unconfirmed.
2. **The user agent must travel with the cookies.** A plain cookie array replayed as the default Chrome 133 was challenged; the browser's Chrome 155 string with the same cookies worked. `auth harvest` always carries it; a manual export should be a HAR or the `{"userAgent","cookies"}` wrapper, or pass `--user-agent`.
3. **The result page embeds about 18 products.** The site lazy-loads the rest one card at a time. `search` reports the site's `total` and `pages` and the products the page embedded; it does not fan out into per-card requests. `shown` says how many are in the output after `--in-stock`/`--limit`.
4. **Promoted placements** mean a `price-asc` page is not strictly ascending (one pinned product at position 11 in the capture). The sort is the site's, not re-sorted locally.
5. **`search --in-stock`** filters the embedded slice client-side because the site offers no stock facet on a search. `--sold-by-leroy-merlin` uses the site's own toggle.
6. **`auth harvest` needs a display** for the browser it launches, or a Chrome already listening on a DevTools port to attach to. No test launches a browser; the browser step is integration only.
7. **Brand values** are passed as typed. The site's Marca facet lists brands in upper case (`SIKA`); lower-case input may not match. This matches what the site itself accepts.

## Literal output

### `leroymerlin --help`

```
Search leroymerlin.es and check stock at your store, from the terminal.
Unofficial and read-only: no cart, no checkout, no account changes.

The site is behind bot protection, so the cli replays the session of your own
browser: export leroymerlin.es cookies (or a HAR) and run `leroymerlin auth login`.
Store and category lists come from the public sitemaps and work without it.

Output is JSON by default so every command pipes into jq. Use --format pretty
for humans. Errors go to stderr; exit codes are in docs/cli-spec.md.

Examples:
  leroymerlin search "cemento cola" --format pretty
  leroymerlin product stock 81866477
  leroymerlin store list --query madrid
  leroymerlin watch add search "cemento cola" --max-price 20 --name cola
  leroymerlin auth login --cookies ~/Downloads/leroymerlin-cookies.json
  leroymerlin doctor

Agent usage: leroymerlin skills get leroymerlin-usage

Usage:
  leroymerlin [command]

Available Commands:
  auth        Import, inspect or forget the browser session
  category    The product category tree
  completion  Generate the autocompletion script for the specified shell
  config      Read and write config.toml
  doctor      Check config, session, sitemaps and whether the storefront accepts the session
  help        Help about any command
  login       Import leroymerlin.es cookies or a HAR from your browser
  mcp         Serve the read commands to an agent over MCP (stdio)
  product     Read one product and its stock
  profile     Several sessions side by side, one per store
  search      Search the catalogue, or browse one category
  sink        Sinks from config.toml: where watch events are copied
  skills      Embedded usage docs for agents and humans
  store       Leroy Merlin stores in Spain
  update      Update leroymerlin to the latest stable release
  watch       Track a search or a product and report only what changed

Flags:
      --debug                 log every http request to stderr (cookies never printed)
      --error-format string   error rendering on stderr: text or json (default "text")
      --format string         output format: json, jsonl, pretty (default "json")
  -h, --help                  help for leroymerlin
      --no-color              disable colour in pretty output (also NO_COLOR)
      --no-input              never prompt; fail when input is missing
      --profile string        profile (one session, one store) to act as (env LEROYMERLIN_PROFILE)
  -v, --version               version for leroymerlin

Use "leroymerlin [command] --help" for more information about a command.
```

### `leroymerlin search "cemento cola" --format pretty`

```
"cemento cola": 737 products, page 1/10, relevance
REF       PRICE           BRAND         NAME                                                  URL
81866477  3.94 EUR        PUMA          Cemento cola Pegoland blanco 25 kg                    https://www.leroymerlin.es/productos/cemento-cola-pegoland-blanco-25-kg-81866477.html
81928499  4.57 EUR        AXTON         Mortero cola para interior/exterior AXTON blanco 25…  https://www.leroymerlin.es/productos/mortero-cola-para-interior-exterior-axton-blanco-25-kg-81928499.html
81944344  14.99 EUR  out  AXTON         Mortero cola AXTON flexible gel blanco 25 Kg          https://www.leroymerlin.es/productos/mortero-cola-axton-flexible-gel-blanco-25-kg-81944344.html
13925520  10.24 EUR       PUMA          Mortero cola porcelánico flexible Pegoland blanco 2…  https://www.leroymerlin.es/productos/mortero-cola-porcelanico-flexible-pegoland-blanco-25-kg-13925520.html
19712224  6.49 EUR        BEISSIER      Cemento cola en pasta BEISSIER 1 kg                   https://www.leroymerlin.es/productos/cemento-cola-en-pasta-beissier-1-kg-19712224.html
81928410  8.99 EUR        AXTON         Mortero cola porcelánico gel AXTON blanco 25 kg       https://www.leroymerlin.es/productos/mortero-cola-porcelanico-gel-axton-blanco-25-kg-81928410.html
85539169  6.29 EUR        AXTON         Mortero cola AXTON flexible 5 kg blanco               https://www.leroymerlin.es/productos/mortero-cola-axton-flexible-5-kg-blanco-85539169.html
90112007  15.70 EUR       KERAKOLL      Mortero cola Flex H40 gel blanco KERAKOLL de 20kg     https://www.leroymerlin.es/productos/mortero-cola-flex-h40-gel-blanco-kerakoll-de-20kg-90112007.html
81928502  6.25 EUR        AXTON         Mortero cola AXTON blanco para exterior 25 kg         https://www.leroymerlin.es/productos/mortero-cola-axton-blanco-para-exterior-25-kg-81928502.html
85539177  4.57 EUR                      Mortero cola interior exterior 25 kg gris             https://www.leroymerlin.es/productos/mortero-cola-interior-exterior-25-kg-gris-85539177.html
83622365  10.79 EUR  out  GRUPOPUMA     Mortero cola felino flex S1 25 kg blanco              https://www.leroymerlin.es/productos/mortero-cola-felino-flex-s1-25-kg-blanco-83622365.html
85539172  13.77 EUR       AXTON         Mortero cola AXTON flexible 25 kg blanco              https://www.leroymerlin.es/productos/mortero-cola-axton-flexible-25-kg-blanco-85539172.html
85539163  10.08 EUR       AXTON         Mortero cola porcelanico AXTON 25 kg blanco           https://www.leroymerlin.es/productos/mortero-cola-porcelanico-axton-25-kg-blanco-85539163.html
754691    5.55 EUR        BEISSIER      Cemento cola extra en polvo BEISSIER 2kg              https://www.leroymerlin.es/productos/cemento-cola-extra-en-polvo-beissier-2kg-754691.html
86672593  5.72 EUR        CEMENTOS CA…  Mortero cola deco interior CEMENTOS CAPA 25 kg gris   https://www.leroymerlin.es/productos/mortero-cola-deco-interior-cementos-capa-25-kg-gris-86672593.html
85810328  32.00 EUR       CEMENTOS CA…  Cemento Cola Porcelanico Flexible C2 TE S1 Saco 25K…  https://www.leroymerlin.es/productos/cemento-cola-porcelanico-flexible-c2-te-s1-saco-25kg-blanca-envio-gratuito-vegara-stone-85810328.html
85539167  14.06 EUR       AXTON         Mortero cola AXTON flexible 25 kg gris                https://www.leroymerlin.es/productos/mortero-cola-axton-flexible-25-kg-gris-85539167.html
81948529  14.99 EUR       AXTON         Mortero cola AXTON flexible gel blanco 25 Kg          https://www.leroymerlin.es/productos/mortero-cola-axton-flexible-gel-blanco-25-kg-81948529.html
more pages: --page 2
```

### `leroymerlin store list --query madrid --format pretty`

```
SLUG            NAME            TYPE
madrid-barajas  Madrid Barajas  store
urban-madrid    Urban Madrid    store
2 stores
```

### `leroymerlin store current --format pretty`

```
id    037
name  Salamanca
```

### `leroymerlin product stock 81866477 --format pretty`

```
ref       81866477
name      Cemento cola Pegoland blanco 25 kg
store     Salamanca (037)
online    true
delivery  store pickup, 2 opening days, on order, 308 available, free
delivery  home delivery, 5 opening days, in stock, 308 available, 19.90 EUR
```
