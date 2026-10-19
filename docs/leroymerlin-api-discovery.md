# leroymerlin.es API discovery

How the cli talks to leroymerlin.es, and how each fact was found. Everything here comes from observation of the public site. Sections marked **pending capture** need the HAR described in §7.

## 1. Hosts and routing

| host | role | protection |
|---|---|---|
| `www.leroymerlin.es` | storefront: server-rendered HTML plus XHR/fragments | DataDome (`server: DataDome`, `x-datadome: protected`) on every path except robots.txt and sitemaps; Fastly in front (`via: 1.1 varnish`, `x-served-by: cache-…`) |
| `media.adeo.com` | product and store images | none observed |
| `esl-nfc.leroymerlin.es` | in-store shelf-label NFC endpoint | Imperva; needs a signed token from the label, so not usable |
| `api.leroymerlin.es`, `tienda.leroymerlin.es` | none | no DNS |

Repro (phase 0):

```sh
curl -sS -o /dev/null -w '%{http_code}\n' -A 'Mozilla/5.0 … Chrome/141.0.0.0 Safari/537.36' 'https://www.leroymerlin.es/search?q=cemento+cola'
# 403, with the challenge page in internal/leroymerlin/testdata/blocked_datadome.html
```

## 2. Auth model

- **Bot protection.** DataDome answers unknown clients with a 403 whose body loads `ct.captcha-delivery.com/c.js`. It sets a `datadome` cookie (`Max-Age=31536000; Domain=.leroymerlin.es`) and rotates it on most responses.
- **What the cli sends.** The user's browser session (ADR 0001) over a Chrome TLS hello (ADR 0002).
- **The user agent is part of the session.** An export that does not carry one falls back to the default Chrome 133. Replaying a Chrome 155 session with that default was challenged; `auth login --user-agent '<the browser's UA>'` made the same cookies work. Keep the pair together, as a HAR or the `{"userAgent","cookies"}` wrapper does.
- **Block detection.** `IsBlockPage` treats a response as a challenge when it carries DataDome's headers and is a 403 or 429, or when the body contains `captcha-delivery.com`. An origin 403 without those markers is not a challenge.
- **Accounts.** Not needed. A guest session with a store chosen is enough.

```http
GET /search?q=cemento+cola HTTP/2
Host: www.leroymerlin.es
User-Agent: Mozilla/5.0 (X11; Linux x86_64) … Chrome/141.0.0.0 Safari/537.36

HTTP/2 403
content-type: text/html;charset=utf-8
server: DataDome
x-datadome: protected
x-datadome-cid: …
set-cookie: datadome=…; Max-Age=31536000; Domain=.leroymerlin.es; Path=/; Secure; SameSite=Lax

<html lang="en"><head><title>leroymerlin.es</title>…<script data-cfasync="false">var dd={'rt':'c',…,'host':'geo.captcha-delivery.com',…}</script><script src="https://ct.captcha-delivery.com/c.js"></script>…
```

## 3. Catalog

### 3.1 Stores: `GET /sitemap-store1.xml` (no session)

The store list is a standard sitemap with one `<url>` per store. Store pages are `/tiendas/<slug>.html`. The name is in `image:title`. A few entries have no title, so the cli title-cases the slug for those. Slugs starting with `compact-` are the smaller Compact format. The sitemap carries no store id, address or coordinates.

```xml
<url>
  <loc>https://www.leroymerlin.es/tiendas/badalona.html</loc>
  <lastmod>2026-10-18</lastmod>
  <image:image>
    <image:loc>https://media.adeo.com/media/3836030/media.jpeg</image:loc>
    <image:title>Badalona</image:title>
  </image:image>
</url>
```

- **Fixture:** `internal/leroymerlin/testdata/sitemap_store1.xml`, the full capture from 2026-10-19 (127 stores).
- **Repro:** `leroymerlin store list --format pretty`

### 3.2 Categories: `GET /sitemap-category1.xml` (no session)

There is one `<url>` per category page `/productos/<path>/`, 5,796 entries on 2026-10-19. By depth: 24 at level 1, 352 at level 2, 2,346 at level 3, 2,429 at level 4, 601 at level 5 and 43 at level 6. The name is in `image:title`.

- **Fixture:** `internal/leroymerlin/testdata/sitemap_category1.xml`, a trimmed slice of the 2026-10-19 capture: the top level plus the `herramientas` and `construccion` branches, entries unmodified.
- **Repro:** `leroymerlin category list --parent construccion --depth 1 --format pretty`

### 3.3 Other sitemaps

`sitemap-index.xml` lists `sitemap-productos<N>.xml` (product URLs, about 21 MB each), `sitemap-searchdex*.xml`, `sitemap-services1.xml` and `sitemap-ideas-y-consejos1.xml`. Product URLs have the form `/productos/<slug>-<ref>.html`, where `<ref>` is the 8-digit reference, for example `/productos/cemento-cola-pegoland-blanco-25-kg-81866477.html`. The cli does not use these yet.

### 3.4 Search: `GET /search?q=<terms>`

`/buscador?q=` redirects elsewhere. Results are server-rendered and the query string is small:

| parameter | values | meaning |
|---|---|---|
| `q` | text | keywords |
| `p` | 1-based number | page |
| `sort` | `relevance`, `price-asc`, `price-desc` | order; the page lists these three in `sorts` |
| `filters` | JSON object, URL-encoded | facets; keys below |

Observed filter keys: `attribute-06575` (the Marca facet, value is the brand name as the page lists it, e.g. `SIKA`), `range-price-min` / `range-price-max` (euros as decimal strings), and `vendor` with value `internal` (the "Vendido por Leroy Merlin" toggle). Values with more than one choice are joined with `#|`. Example:

```http
GET /search?q=cemento+cola&p=2&sort=price-asc&filters=%7B%22range-price-min%22%3A%225%22%2C%22range-price-max%22%3A%2220%22%7D
```

The page carries a `guidance-ssr-data` JSON island:

- `body.pagination {currentPage, numberOfPages}`;
- `body.products.number` (the total across pages);
- `body.sorts [{code,label,applied}]`;
- `body.filters {stockAndDeliveryFilters, productFilters}` with the choices and counts the drawer renders;
- `header.query.original` (the text the site searched for).

Products arrive in `cdl_products_list` islands (`<script type="application/json" class="dataTms">`): `{brand, identifier, name, url, offer:{add_to_cart_availability, unitprice_ati, unitprice_tf, seller_name, seller_type}, position, rating, product_is_sponsored}`. `unitprice_ati` is the price with tax, `unitprice_tf` without.

Two facts matter for a parser:

- The page embeds the main result island (18 products in the captures) plus a small 4-product island that is identical on every page (sponsored / recently viewed). The main island is the largest one.
- Beyond the embedded products the cards are shimmer placeholders with a `data-lazy-url` to `ProductThumbnailFragment`, one request per card. The cli does not chase them: `search` reports the site's total and the products the page embedded. This keeps a page to one request.

Category pages (`GET /productos/<path>/`) use the same guidance island and the same product islands, so `search --category PATH` shares the parser and the `p`, `sort` and `filters` parameters.

Captures used: `search_cemento_cola.html` (relevance, page 1), `search_cemento_cola_filtered.html` (price 5–20, price-asc), `search_cemento_cola_page2.html` (page 2), `category_cementos.html` (leaf category). All four are real responses with cookies, ktx tokens and context ids stripped.

### 3.5 Product and stock

Product pages are `/productos/<slug>-<ref>.html`. Two blocks matter:

- **`jsonld_PRODUCT`** (schema.org): `name`, `sku`, `gtin`, `description`, `image`, `offers {price, priceCurrency, availability, url}`, `aggregateRating {ratingValue, reviewCount}`.
- **`cdl_products` dataTms islands**: the same product plus `offer.add_to_cart_availability`, `unitprice_ati` / `unitprice_tf`, seller, and `offer.available_deliveries [{price, stock, stockStatus, time, type}]`.

In the capture **without** a selected store `available_deliveries` has one entry, `type: homeDelivery`. With store 037 (Salamanca) selected it has two:

```json
[
  {"price": 0.0, "stock": 308, "stockStatus": "ONORDER", "time": "2 OPENING_DAY", "type": "storeDelivery"},
  {"price": 19.9, "stock": 308, "stockStatus": "ONSITE",  "time": "5 OPENING_DAY", "type": "homeDelivery"}
]
```

`storeDelivery` is pickup at the selected store: price 0 means free, `stockStatus` is `ONSITE` (on the shelf), `ONORDER` (order to the store) or `OUTOFSTOCK`, and `time` is the estimate. The stock number is what the offer states; the status is what tells shelf from order. The cli reports both and watches changes in either.

The page states the selected store directly:

```html
<div ... data-main-store-id="037" data-main-store-name="Salamanca" ...>
```

The home page states it as `data-store-id="037"` plus `data-main-store-name="Salamanca"` on the context header, which is what `store current` reads. The session cookies carry `store_id`, `lmes_store_id` and `store` next to the context and bot-protection cookies.

`cdl_user_context_main_store` is the tracking layer's copy; it is `"no cookie here"` only when no store is selected.

Per-store stock therefore needs no disallowed endpoint: the pickup row is in the product HTML. The lazy `MeshingService` fragment (`/lazy/com.adeo.website.productpageservicesfunctionality.MeshingService?ktx=<token>`, robots-disallowed) is still never requested, and the cli has no parser for it.

Captures used: `product_81866477.html` (no store, home delivery only), `product_81866477_store.html` (store 037, pickup + home delivery), `product_81944344.html` (not available, no delivery rows), `home_nostore.html` and `home_salamanca.html` for the context. All real, stripped.

### 3.6 Cart, account, store selection

Out of scope (ADR 0003). Choosing a store is a browser action ("Elegir mi tienda"); the cli reads the selection, it never changes it.

## 4. Cart

Out of scope (ADR 0003).

## 5. Region / store binding

Verified with a session that had store 037 (Salamanca, postcode 37001) selected:

- the home page renders `data-store-id="037"` and `data-main-store-name="Salamanca"` on the context header;
- a product page renders `data-main-store-id="037"` and `data-main-store-name="Salamanca"` and adds a `storeDelivery` row to `available_deliveries`;
- the cookies include `store_id`, `lmes_store_id` and `store`.

`store current` reads the home page attributes; `product stock` reads the product page. Both report the same store. Comparing stores still means one profile per store, because the selection is part of the session.

## 6. Account auth

Not used. Sessions are guest browser sessions imported by the user (ADR 0001).

## 7. Capture still needed

Nothing blocks the read surface. Two optional captures would close the last small gaps:

1. A product that the selected store has **on the shelf** (`stockStatus: ONSITE` on a `storeDelivery` row), to confirm the human rendering beside the `ONORDER` row in the current fixture.
2. A product that the store cannot serve, for the `OUTOFSTOCK` branch.

Sanitize them the same way: cookies, `Set-Cookie` headers, ktx tokens and context ids removed before they land in `testdata/`.

## 8. robots.txt (V53, read 2026-10-19)

- **Disallowed for all user agents** (among others): `/search?q=*`, `/buscador`, `/*filters=*`, `/*sort=*`, `/*limit=*`, `/*/*?facet=*`, `*/productpageservicesfunctionality/*`, `/*?*storeid`, `/header-cart*`, `/cart`, `/checkout*`, `/login*`, `/mylm/*`.
- **Allowed:** product and category pages without those parameters, and the sitemaps.
- **How the cli responds.** It is a client one person drives, not a crawler, but it keeps to a human pace and has no bulk or mirror mode. See ADR 0001.
