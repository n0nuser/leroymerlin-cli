# Review: read surface and per-store stock

Date: 2026-10-19. Branch: `feat/initial`.

## Status

The read surface is complete for one person shopping: stores, categories, search with the site's filters, one product, per-store pickup stock, and watches. The per-store stock caveat from the previous handoff is closed: a session with store 037 (Salamanca) selected was imported and verified live, and the pickup row is in the product HTML, not in a disallowed endpoint.

## What was checked

- `auth login` of a 13-cookie export (Cookie Editor JSON, including HttpOnly `idclient`), then `doctor`.
- `store current` against the home page.
- `product stock` for `81866477` (Cemento cola Pegoland 25 kg), both without and with a store selected.
- `product show` and a `watch add product` / `check` / `events` / `remove` cycle with the store-selected session.
- Parser behaviour against seven no-store captures and two new store captures.
- `auth harvest` for the session: cookie mapping through `ParseExport`, host matching, Chrome resolution, DevTools reachability and the challenge check, all unit-tested without launching a browser.
- The full suite, `make check`, `make docs-check` and the docs-site build.

## Findings

1. **Per-store pickup stock works from the product HTML.** With store 037 selected, `available_deliveries` carries `storeDelivery` (`price 0`, `stock 308`, `stockStatus ONORDER`, `time 2 OPENING_DAY`) beside `homeDelivery`. The pages state `data-main-store-id="037"` / `data-main-store-name="Salamanca"`. The cli reads both; the `MeshingService` fragment stays untouched. **Closed.**
2. **The user agent has to travel with the cookies.** The re-export was a plain cookie array with no user agent. Replayed as the default Chrome 133 it was challenged; passing the browser's Chrome 155 string made the same cookies work. `auth login --user-agent` covers it, and the docs now say the pair is scored together. **Closed for the documented path.**
3. **`store current` lost the id on the home page.** The home header uses `data-store-id`/`data-main-store-name`, while the product page uses `data-main-store-id`. The parser now falls back to `data-store-id`; the live output is `037 / Salamanca`. **Fixed, with a fixture test.**
4. **`ONORDER` is not stock on the shelf.** Pickup with `ONORDER` says the store can order it, not that it is there today. The pretty output now prints the status (`on order`, `in stock`, `out of stock`), free pickup as `free`, and watches diff the status as well as the count (`stock_changed` carries `from_status`/`to_status`). **Fixed.**
5. **The result page embeds about 18 products; the tail is lazy.** Accepted: `search` reports the site's total and the embedded slice instead of making dozens of per-card requests.
6. **Promoted placements break strict price order.** The site pins a product at position 11 on a `price-asc` page. Accepted: the sort is the site's, not re-sorted locally.
7. **Pickup `ONSITE` and `OUTOFSTOCK` are not captured.** The parser carries the raw status and the renderer handles both; only the fixtures for those two states are missing.
8. **`auth harvest` replaces the manual export for interactive users.** It is headed by design, so it needs a display (or a Chrome to attach to), and the browser step is integration-only: the unit tests cover the mapping and resolution, not a launch. `auth login` stays for scripts and CI. **Documented and tested where it can be.**

## Residual risks

- DataDome can re-challenge a session at any time; the fix stays on the user's side (reload, re-export), and the cli exits 5.
- A cookie export without a user agent will keep being fragile. `--user-agent` or a HAR is the reliable path.
- The docs-site and the generated reference must be regenerated when command help changes; CI enforces it.

## Literal output

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
