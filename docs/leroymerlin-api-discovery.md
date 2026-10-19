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

### 3.4 Search: pending capture

- **Path.** `GET /search?q=<terms>`. `/buscador?q=` redirects elsewhere.
- **Rendering.** Results are server-rendered HTML.
- **Seen so far.** Phase 0 saw these XHRs on the search page: `/header-cart-module/backend/rest/cart-data` and `/cdp-due--customer-context-module/api/get-context`. Neither carries results.
- **Still to capture.** The filter, sort and pagination parameters. robots.txt disallows `filters=`, `sort=`, `limit=` and `facet=`, which suggests those are the parameter names.

### 3.5 Product and per-store stock: pending capture

Product pages are `/productos/<slug>-<ref>.html`. The product page in phase 0 loaded these XHRs:

- `/cdp-due--customer-context-module/api/{get,update}-context`. This is the customer and store context. It is the likely carrier of the selected store.
- `/lazy/com.adeo.website.productpageservicesfunctionality.MeshingService`. This is probably the stock and delivery service. robots.txt disallows `*/productpageservicesfunctionality/*`.
- `/components/productdetails/services/rag-productdetail/suggestions`
- `/lazy/loyalty-*.fragment.adeo`

Still to capture: where price and stock live, whether that is JSON-LD, embedded state, or the fragment above, and how the selected store is encoded.

## 4. Cart

Out of scope (ADR 0003).

## 5. Region / store binding: pending capture

Choosing a store ("Elegir mi tienda") sets the store for stock. The mechanism is probably a cookie or the context module, but it has not been seen yet.

## 6. Account auth

Not used. Sessions are guest browser sessions imported by the user (ADR 0001).

## 7. Capture still needed

These are in Chrome DevTools, without logging in, with "Allow to generate HAR with sensitive data" on, then "Export HAR (with sensitive data)":

1. Choose a store.
2. Search "cemento cola".
3. Run the same search with a brand filter and a price sort.
4. Open page 2.
5. Open a category page.
6. Open two product pages, one in stock at the store and one not.
7. Open any panel that shows stock in other stores.
8. Switch to another store and reload one product.

Then repeat the flow once with different inputs. Fixtures cut from the HAR are sanitized: cookies, `Set-Cookie` headers and context ids are removed before they land in `testdata/`.

## 8. robots.txt (V53, read 2026-10-19)

- **Disallowed for all user agents** (among others): `/search?q=*`, `/buscador`, `/*filters=*`, `/*sort=*`, `/*limit=*`, `/*/*?facet=*`, `*/productpageservicesfunctionality/*`, `/*?*storeid`, `/header-cart*`, `/cart`, `/checkout*`, `/login*`, `/mylm/*`.
- **Allowed:** product and category pages without those parameters, and the sitemaps.
- **How the cli responds.** It is a client one person drives, not a crawler, but it keeps to a human pace and has no bulk or mirror mode. See ADR 0001.
