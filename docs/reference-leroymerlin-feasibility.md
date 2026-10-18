blocked: every data path on www.leroymerlin.es is behind DataDome. A plain Go `net/http` client gets 403. Getting through needs a Chrome-TLS-fingerprint client plus browser cookies, or a real browser.

# leroymerlin.es feasibility (phase 0)

Checked 2026-10-18 from Spain. Fastly served the responses from its TOJ (Madrid) POP.

## Repro

```sh
UA='Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36'
for p in / "/search?q=cemento+cola" \
         /productos/cemento-cola-pegoland-blanco-25-kg-81866477.html \
         /header-cart-module/backend/rest/cart-data \
         /cdp-due--customer-context-module/api/get-context \
         /api/v1/stores; do
  curl -sS -o /dev/null -A "$UA" -H 'Accept-Language: es-ES' \
       -w "%{http_code} $p\n" "https://www.leroymerlin.es$p"
done
```

Every line prints `403`. Each response carries `server: DataDome`, `x-datadome: protected`, a `datadome=` cookie and a 774-byte captcha-delivery page.

Other probes:

| Probe | Result |
|---|---|
| UAs `curl/8.9`, `okhttp/4.12.0`, `LeroyMerlin/7.0 (Android)`, Googlebot | 403 DataDome |
| `www.leroymerlin.fr`, `www.leroymerlin.pt` | 403 DataDome (same ADEO group setup) |
| `api.leroymerlin.es`, `tienda.leroymerlin.es` | no DNS |
| `m.leroymerlin.es` | 503 |
| `esl-nfc.leroymerlin.es/elmo/nfc/api/v1/url/product/info` | 400, fronted by Imperva (`X-CDN: Imperva`, `incap_ses_*` cookies). This is the in-store shelf-label NFC endpoint and needs a signed token from the label, so it can't serve as a catalog API. |
| `robots.txt`, `sitemap-index.xml`, `sitemap-store1.xml`, `sitemap-productos{1..4}.xml` | **200**, not challenged |
| Automated Chromium, `/buscador?q=…` | `ERR_HTTP_RESPONSE_CODE_FAILURE`. Not retried on `/search`, so inconclusive. |
| The user's real desktop Chrome, `/search?q=cemento+cola` and a product page | loads normally |

## Backend

ADEO storefront, server-rendered HTML. Search results and product data are in the HTML of `/search?q=` and `/productos/<slug>-<ref>.html`. I watched the XHRs in real Chrome on the search and product pages. All of them go to the same DataDome-fronted host:

- `www.leroymerlin.es/header-cart-module/backend/rest/cart-data`
- `www.leroymerlin.es/cdp-due--customer-context-module/api/{get,update}-context` (store/customer context)
- `www.leroymerlin.es/lazy/com.adeo.website.productpageservicesfunctionality.MeshingService`
- `www.leroymerlin.es/components/productdetails/services/rag-productdetail/suggestions`

I found no separate JSON API host (Algolia, or a BFF on another domain). Store stock is in the product page and its lazy fragments, so it sits behind DataDome too.

## What gets through, and what it costs

- **Sitemaps (plain HTTP works).** They list product URLs and stores, but carry no prices, stock or search. That is not enough for `search`, `item` or per-store stock.
- **Chrome-fingerprint TLS client + browser cookies.** Public tooling for this site already exists and documents this route. It uses `refraction-networking/utls` to present Chrome's JA3 and reads the `datadome` and session cookies out of the user's installed browser. Without the browser session, that tooling's own docs say DataDome still challenges it. This is the TLS-fingerprint-client workaround, which counts as heavy.
- **Headless/automated browser.** A real Chrome passes. Driving one from the CLI is the other heavy workaround.
- **Mobile app API.** Not investigated. It needs MITM on a rooted device. ADEO's app traffic probably goes through DataDome's mobile SDK, but that is unverified.

## Open question

I have not tested whether a `datadome` cookie exported from a real browser, sent through plain `net/http` (stock Go TLS), gets through. That would be a light path: a `login --cookies` style session import. The cookie is HttpOnly and replaying it is a bot-detection bypass, so the user should run this test, not the agent:

```sh
curl -sS -o /dev/null -w '%{http_code}\n' -A "$UA" \
  -b 'datadome=<value from devtools>' 'https://www.leroymerlin.es/search?q=cemento+cola'
```

curl's TLS fingerprint differs from Go's, so a 200 here is a hint, not proof. A 403 means the cookie alone is not enough.

## Options (for the user to choose)

1. Stop here.
2. Cookie import + stdlib client, if the test above passes.
3. uTLS Chrome fingerprint + browser cookies. Adds non-stdlib deps, and DataDome can break it at any time.
4. Sitemap-only CLI (product/store lookup by URL, no prices or stock). Drops the main feature.
5. MITM the Android app to look for a less-protected API, then run this gate again.

## Legal / scope

I have not checked for a bug-bounty program yet. Do that before phase 1 if we go ahead. Anything built here stays read-only (catalog and stock) and keeps a low request rate.
