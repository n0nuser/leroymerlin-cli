# leroymerlin.es: data access options under DataDome

Desk research, 2026-10-19. No request was sent to leroymerlin.es or its subdomains for this document. Claims come from vendor docs, legal texts, press and third-party write-ups. Where a source is a third-party tool or service, it is described generically and not linked. Anything not verified is marked **unverified**. Baseline facts about the site come from the phase-0 probe in `docs/reference-leroymerlin-feasibility.md`.

## summary

We found no official, public route to per-store stock. We could not locate an ADEO or Leroy Merlin developer portal. The Mirakl marketplace API is for sellers and covers their own offers. The Awin affiliate programme mentions a product catalogue but not stock by store. The only source of real price and per-store stock today is the site's own HTML and fragments, and DataDome protects those.

The practical choices are:

- **(a) user-driven session.** The user exports cookies or a HAR from their own browser, and the CLI replays them with a Chrome-like TLS client.
- **(b) local real browser.** The CLI drives a real Chrome on the user's machine.
- **(c) sitemap-only.** A degraded CLI with no price or stock.
- **(d) ask Leroy Merlin.** Request permission or a feed.

Solver services and residential-proxy unblockers work in principle, but they carry the highest ToS risk and the highest maintenance cost. Option (a) fits a small, read-only, low-rate tool best. Run (d) in parallel, since it is the only option that could give a durable, legitimate channel. Public evidence on how long a cookie stays valid on this site is thin, so that needs a user-run test.

## 1. official, partner and affiliate APIs and feeds

### how it works
- **ADEO / Leroy Merlin developer portal.** Search found none for Leroy Merlin Spain or ADEO. Results only covered seller tooling and an ADEO supplier portal. No results is not proof that none exists, so this is **unverified**.
- **Mirakl seller API.** Leroy Merlin runs its marketplace on Mirakl. Marketplace-integrator guides (not linked) describe the setup:
  - sellers get Mirakl back-office access after onboarding and create an API key there;
  - Spain is one of several country tenants;
  - Mirakl splits data into products (catalogue) and offers (price, stock, shipping, seller terms);
  - only the account master can generate the key;
  - the API pushes and pulls data for a single seller.

  Whether a seller key can read other sellers' offers, Leroy Merlin's own catalogue, or store stock is **unverified**. The official Mirakl seller API index requires seller access and was not reachable.
- **Awin affiliate programme.** Leroy Merlin España launched an Awin programme ([Alimarket](https://www.alimarket.es/construccion/noticia/327301/leroy-merlin-lanza-su-programa-de-afiliacion-awin--un-nuevo-recurso-para-impulsar-ventas-online), [Marketing4eCommerce](https://marketing4ecommerce.net/leroy-merlin-lanza-su-propio-programa-de-afiliados-para-potenciar-sus-ventas-online/)). The Awin merchant profile ([Awin profile 20598](https://ui.awin.com/merchant-profile/20598)) lists:
  - 6% CPA on own-brand products;
  - 3% on marketplace products also sold on the web;
  - "Catálogo de productos actualizado para el uso de los afiliados".

  The profile does not name a data feed, its fields, or stock. We found no programme on other affiliate networks, which is not proof there is none.
- **Price comparison / Google Shopping local inventory.** Leroy Merlin may publish product and local-inventory feeds to Google Merchant Center or comparison sites. That data goes to the merchant's channels and is not exposed as a public API. We found no source either way, so this is **unverified**. Google renders Google Shopping results, and scraping them is a separate ToS problem with Google.

### dependencies
- Mirakl: an approved seller account on the Spain tenant. That needs commercial onboarding, which a hobby project can't get.
- Awin: a publisher account the advertiser accepts. The catalogue is typically a CSV/XML feed under the programme terms. Feed format and fields for this advertiser are **unverified**.
- Comparison channels: you have to be the merchant, so this does not apply to us.

### reliability and maintenance
- An official feed, if we got one, would be stable and cheap to maintain. An affiliate catalogue feed would likely give name, URL, EAN/SKU, image and price, refreshed on the feed schedule (daily is typical for Awin; **unverified** here). Nothing we found mentions per-store stock, so a feed likely misses the main requirement.
- The Mirakl seller API gives offer data for that seller only (**unverified** beyond that). It cannot give per-store stock for Leroy Merlin's own retail stores.

### tos and legal risk
- Low for the Awin route if used inside the programme terms. Affiliate terms usually restrict use of the catalogue to promoting the merchant, though, so a general-purpose public CLI is likely out of scope (terms not read: **unverified**).
- Using a seller key we are not entitled to, just to read the catalogue, would breach the Mirakl operator terms. Not recommended.

## 2. the android / ios app api

### how it works
- **Package name: not confirmed.** The App Store listing `id370400343` names Leroy Merlin France as the seller ([App Store](https://apps.apple.com/app/id370400343)), which suggests one multi-country app. The Google Play package id is **unverified**. The Spain app is popular: over 1 million downloads, and 24% of online purchases go through it ([Alimarket](https://www.alimarket.es/construccion/noticia_amp/378159/leroy-merlin-estrena-nueva-app-para-mejorar-la-experiencia-de-compra), [Interempresas](https://www.interempresas.net/Ferreteria/582514-App-de-Leroy-Merlin-representa-24-por-ciento-de-su-venta-digital-ano-despues-de-su.html)).
- **Backend hosts and protocol: not found** in any public APK write-up. The phase-0 probe found:
  - `api.leroymerlin.es` has no DNS;
  - `m.leroymerlin.es` returns 503;
  - a spoofed `LeroyMerlin/7.0 (Android)` UA on the website gets 403.

  So if app traffic is separate, it uses other hosts: **unverified**.
- **SDK inventory.** We found no public tracker/SDK report for the app, so whether the APK contains DataDome's SDK is **unverified**. Public APK SDK-report databases can answer this once the package id is known.
- **Protection that would apply if the SDK is present.**
  - DataDome's Android SDK is an OkHttp interceptor. It handles challenges and cookie sync, and collects touch events and behavioural signals ([Android SDK docs](https://docs.datadome.co/docs/sdk-android)).
  - DataDome's checklist says apps need the mobile SDK for the Slider and Device Check ([checklist](https://docs.datadome.co/docs/checklist-before-activating-protection)).
  - The Android page does not document attestation or certificate pinning. Pinning in the host app is unverified.

### dependencies
- A rooted device or emulator, a MITM proxy, a dynamic-instrumentation tool to get past pinning, and APK decompilation. Not a Go-only path.

### reliability and maintenance
- Unknown. Mobile APIs sometimes skip web-only checks. But we saw page-level blocking on the web, and DataDome ships mobile tooling, so the app could just put the same wall somewhere else.
- Requests would have to replicate the SDK's headers and cookie flow, and any app update can change auth.
- High effort, uncertain payoff. The phase-0 note already lists this route as untested.

### tos and legal risk
- Reverse engineering an app can breach the app store terms and the EULA. EU law allows decompilation only for interoperability, in narrow cases (Directive 2009/24/EC art. 6; not read for this document, **unverified**).
- Defeating the SDK is a bot-detection bypass. Risk is higher than the web route.

## 3. existing public tools

Generic summary of what is publicly available. Individual tools are not named here.

- **A Go CLI for leroymerlin.es.** It does search, product detail, basket pricing, cart and read-only checkout, and shows stock for the store selected in the session.
  - It gets through by presenting Chrome's TLS ClientHello via uTLS and replaying the user's signed-in browser cookies, imported from the browser, a HAR, or a pasted cookie.
  - Its docs say that without a session even search gets 403, that the clearance "rotates and expires", and that cart endpoints are scored more strictly.
  - It looks like a single-maintainer proof of concept with little public use.
- **Hosted scraper "actors" on a scraping marketplace.** The one found targets leroymerlin.fr only (name, price, brand, sku, rating; a detail mode adds EAN and specs).
  - It runs a browser session over French residential proxies plus a commercial unblocker API.
  - Its page reported category pages blocked, and detail pages blocked since late August 2026 (measured 2026-09-17).
  - It has no store-level stock.
  - This is evidence that blocking is tightening.
- **Commercial scraping services** also advertise Leroy Merlin data. Domains and fields are **unverified**.

We found no other public scraper for leroymerlin.es. Product pages embed structured data, so parsing should be simple once access works.

### dependencies / reliability / tos
- These tools need either a signed-in desktop browser or paid proxies and unblockers. They are exposed to the same DataDome tightening seen on .fr. Reliability is unproven, and per-store stock only works for the store the session has selected. Legal risk is the same as options 4 and 5.

## 4. uTLS and fingerprint clients with cookie import

### how it works
- **Libraries.** [refraction-networking/utls](https://github.com/refraction-networking/utls) is a Go library for ClientHello mimicry. Other libraries in Go and Python wrap it, or wrap a patched curl, to impersonate TLS/JA3 and HTTP/2 fingerprints. None of them claim to solve JS challenges.
- **What DataDome scores.**
  - DataDome's engineering post calls TLS fingerprinting (hashes of cipher suites, extensions, JA3) one reliable signal among a very large number of signals ([DataDome engineering post](https://datadome.co/engineering/how-tls-fingerprinting-reinforces-datadomes-protection/)).
  - Its scraping page names JA4 as more granular, and lists HTTP fingerprints including header order ([DataDome scraping detection](https://datadome.co/threats/detect-web-scraping-attacks/)).
  - DataDome's State of Bots 2024 says some libraries let bot developers present a consistent TLS fingerprint ([report](https://datadome.co/threat-research/the-state-of-bots-2024-changes-to-bot-ecosystem/)).
  - Only third-party vendor blogs claim HTTP/2 fingerprint scoring. Nothing we read from DataDome confirms it.
- **Cookie lifetime.**
  - DataDome's cookie table lists the `datadome` cookie with a 1-year lifetime, on web and mobile, used for server-side and client-side detection ([docs](https://docs.datadome.co/docs/cookie-session-storage)). Customers can set anything from 7 days to 1 year ([data privacy](https://docs.datadome.co/docs/data-privacy)).
  - That is the cookie's expiry, not how long it stays trusted. The cookie carries an encrypted score and state, and the server can re-challenge at any time.
  - Third-party blogs claim the cookie is bound to IP or fingerprint. DataDome's cookie docs say no such thing. Treat it as plausible, not documented.
- **Implication for us.** The safest replay setup uses the same machine, IP, TLS profile and UA as the browser that earned the cookie. The cookie is HttpOnly and DataDome's JS tag rewrites it, so a static copy will go stale ([DataDome docs](https://docs.datadome.co/docs/cookie-session-storage)).

### dependencies
- Go: utls, plus cookie-file or HAR import. utls does not give a full browser HTTP/2 stack, so a plain `net/http` transport over utls needs care. That is our own engineering note, not a cited fact.

### reliability and maintenance
- Medium. It works while DataDome trusts the cookie and does not demand a JS challenge.
- It needs updates when Chrome changes its ClientHello (extension shuffling, new groups) and when DataDome adds checks. Expect periodic breakage and a re-login step.

### tos and legal risk
- Medium; see the legal section below. Using the user's own session at a low request rate is less aggressive than bulk proxies, but it still circumvents a technical measure.
- Whether Spanish law's protection of technical measures (Art. 160 TRLPI, which covers works) applies to a retail site is **unverified**.

## 5. headless browsers and solvers

### how it works
- **Stealth automation.**
  - Third-party write-ups report that mainstream automation frameworks with stealth patches get detected reliably. They describe patched or CDP-only Chromium and Firefox forks as the current baseline, and users report even those still get flagged on headless Linux hosts.
  - Phase 0 saw an automated Chromium request fail on `/buscador`. That result is inconclusive.
  - In Go, the equivalent is driving a real, headful Chrome with the user's profile over the Chrome DevTools Protocol. Per phase 0, the user's real Chrome loads the site normally.
  - DataDome documents its detection of headless browsers ([example page](https://datadome.co/headless-browsers/chromeless/)).
- **Solvers.** Captcha-solving services take the captcha URL, a UA and usually a proxy, and return a cookie. The result is tied to the proxy/IP and UA that will use it. Pricing and docs were not retrieved, so this is **unverified**. Commercial unblocker APIs charge per request.

### dependencies
- Chrome installed on the user's machine plus a CDP driver library, or a Python/Node stealth stack. Solvers need an API key, a paid balance and residential proxies.

### reliability and maintenance
- **Real headful Chrome with a persistent profile:** medium-high success, but high resource cost. Awkward for a CLI: slow start, and it needs a display.
- **Stealth stacks:** an arms race. Write-ups note a solver can go from working to failing within a week.
- **Solvers:** per-request cost, and a third party in the data path.

### tos and legal risk
- Stealth automation, and solver services above all, are explicit bot-detection bypasses. This is the highest ToS and unfair-competition risk, and it is hard to justify as "personal use". Not recommended as a shipped feature.
- A visible browser that the user launches, logs into and uses is a different thing (see 6).

## 6. other credible routes

- **Sitemaps.** `sitemap-index.xml`, `sitemap-store1.xml` and `sitemap-productos1..4.xml` returned 200 to plain HTTP in phase 0.
  - They give product and store URLs, with no price or stock: enough for a catalogue index and a store list, not for the main requirement.
  - Lowest legal risk, since the site publishes them for crawlers.
  - The robots.txt content was not read, so it is **unverified**. Check it before using any path.
- **Web archives / search engine caches.** Not researched in depth. Crawl coverage of a DataDome site is probably patchy and stale, so prices and stock would be out of date. **Unverified**; low value.
- **schema.org via search engines.** Product pages embed structured data, but a search engine's indexed copy has no per-store stock. Low value.
- **Store locator.** The store list is in `sitemap-store1.xml` (phase 0), so store addresses and phone numbers come from there. Coordinates and ids need the site (unverified).
- **In-store APIs.** The `esl-nfc` endpoint sits behind Imperva and needs a token from the shelf label (phase 0). Not usable.
- **Ask Leroy Merlin.** Ask for a licence or a feed. Ryanair v PR Aviation shows a site owner can restrict automated extraction by contract when the data has no database protection ([summary](https://www.scl.org/3280-the-cjeu-takes-flight-databases-and-air-fares/)). Written permission is the only clean route. Also check for a bug-bounty programme or security.txt (not checked yet, per phase 0).
- **User-driven session.** The user browses leroymerlin.es in their own Chrome, selects their store, and exports cookies or a HAR. Then either:
  - the CLI replays the session at a low rate (the same mechanism as option 4, under explicit user control); or
  - a browser extension reads the page the user is viewing and passes structured data to the CLI. This is our own design, with no source.

  The extension makes no automated requests, so it avoids the circumvention argument entirely. It only covers the products and stores the user visits.

## legal and terms notes (Spain / EU)

Not legal advice. Below is what we could verify.

- **Directive 96/9/EC** ([EUR-Lex 31996L0009](https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:31996L0009)).
  - The sui generis right (Art. 7) lets the maker of a database built with substantial investment prevent extraction or re-use of the whole or a substantial part. It also covers repeated, systematic extraction of insubstantial parts when that conflicts with normal exploitation or causes unreasonable prejudice.
  - A lawful user may take insubstantial parts (Art. 8). Contract terms contrary to Arts. 6(1) and 8 are void (Art. 15).
  - A retail catalogue and store stock could qualify if the investment went into obtaining or verifying the data rather than creating it. That is a question of fact, **unverified** for this site.
  - A CLI running a handful of queries for one user extracts insubstantial parts. A CLI used to mirror the catalogue would risk Art. 7.
- **Ryanair v PR Aviation (C-30/14, 15 Jan 2015).**
  - Secondary summaries say the Court held that the Directive's limits on contractual restrictions don't apply when a database has neither copyright nor sui generis protection, so the owner may restrict use by contract ([SCL summary](https://www.scl.org/3280-the-cjeu-takes-flight-databases-and-air-fares/), [Kluwer IP blog](https://copyrightblog.kluweriplaw.com/category/jurisdiction-2/page/90/)).
  - The judgment text itself was not retrieved, so paragraph references are unverified.
  - Effect: if the leroymerlin.es terms forbid automated extraction and the user accepted them, Leroy Merlin has a breach-of-contract claim even without a database right. Whether mere browsing binds a visitor to site terms in Spain is not settled in the sources we read.
- **Leroy Merlin terms.**
  - The aviso legal is at `leroymerlin.es/aviso-legal.html`, owner LEROY MERLIN ESPAÑA, S.L.U. We did not fetch it (constraint), so any clause on scraping or automated access is **unverified**.
  - Read it, plus the marketplace regulations page and the robots.txt `Disallow` set, before shipping.
- **Technical measures.** DataDome is an access control. Getting around it may breach the ToS, and could support an unfair-competition or computer-misuse argument. Spanish law specifics were not researched: **unverified**.
- **GDPR / data protection authorities.**
  - Catalogue, price and stock data is not personal data. The CNIL scraping guidance ([CNIL](https://www.cnil.fr/fr/focus-interet-legitime-collecte-par-moissonnage)) and the AEPD criterion ([AEPD](https://www.aepd.es/informes-y-resoluciones/criterios-juridicos-aepd/herramientas-data-scraping-para-evaluacion-de-tendencias)) only apply if we collect personal data.
  - Marketplace seller names are mostly businesses, but some are sole traders.
  - A replayed login session (cart, account) touches the user's own personal data, so keep the tool to the anonymous catalogue.

## comparison

| approach | per-store stock | robustness | maintenance | legal/tos risk | notes |
|---|---|---|---|---|---|
| 1a. Awin catalogue feed | no evidence of it | high | low | low if within terms; terms likely restrict reuse | needs publisher approval; may still help with price and basic catalogue |
| 1b. Mirakl seller API | no (own offers only; unverified) | high | low | high if used without entitlement | requires seller status |
| 1c. Google local inventory / comparison feeds | would carry local stock, but not reachable | n/a | n/a | n/a | merchant-side only |
| 2. Mobile app API | unknown, likely yes | unknown | high | medium-high | package id and protection unverified |
| 3. Existing public tools | yes for the session store (CLI); no (hosted .fr scraper) | low-medium | n/a (third party) | same as 4/5 | evidence of continued blocking on .fr |
| 4. uTLS + cookie import | yes, for the store chosen in the session | medium | medium | medium | replays the user's own session; re-login when it expires |
| 5a. Real browser (CDP, user profile) | yes | medium-high | medium | medium | slow, needs display |
| 5b. Stealth browsers, solvers, unblockers | yes | low-medium (arms race) | high | high | explicit bypass; paid |
| 6a. Sitemaps only | no | high | low | low | catalogue index and store list only |
| 6b. Cache / web archives | no | n/a | n/a | low-medium | stale, unverified coverage |
| 6c. Ask for permission / feed | possible if granted | high | low | none | slow, outcome unknown |
| 6d. User browser extension / HAR import | only for visited pages | high (page markup changes only) | low-medium | low-medium | no automated requests in the extension variant |

## ranked recommendation

1. **Ask Leroy Merlin (and Awin) for permission or a feed, in parallel with everything else.** This is the only route that can be both legitimate and durable, and it costs an email and some waiting. Per-store stock is unlikely to be offered, so don't block development on it.
2. **User-driven session: cookie/HAR import with a Chrome-fingerprint TLS client** (option 4 plus the user-browser part of 6). This matches the per-store stock requirement best and costs the least to build. Stock is for the store the user selected in their browser.
   - Expect a re-login whenever DataDome re-challenges.
   - Send requests one at a time, at a low rate.
   - No account or cart features.
3. **Browser-extension / DOM-read variant of the user-driven session.** It only covers pages the user opens and adds another deliverable. In return it is the most defensible legally and the most robust to DataDome changes. A good fallback if 2 breaks.
4. **Sitemap-only mode** as the always-available baseline for `store list` and URL lookup. No price or stock. Low risk, low value.
5. **Drive a local, headful Chrome with the user's profile (CDP).** It works when 2 does not, but it is heavier and slower. Treat it as an optional backend.
6. **Mobile app API.** Only worth a time-boxed look if 2 and 3 fail and the legal review allows it. Protection and package id are unknown.
7. **Not recommended:** solver services, stealth-browser farms, residential-proxy unblockers, mirroring the catalogue. These carry the highest risk and cost. The bot-detection bypass is also the part most likely to draw a ToS or legal response.

## key uncertainties
- Whether the `datadome` cookie alone (with stock TLS) works when taken from a browser session, and how long a trusted cookie lasts on leroymerlin.es. Untested. DataDome's docs only give a 1-year cookie lifetime, not how long the cookie stays trusted.
- Whether DataDome binds the cookie to IP or fingerprint. Only third parties claim it does.
- The aviso legal text, the robots.txt content and any bug-bounty policy. Not read.
- Whether any Mirakl or Awin feed carries stock. Unverified.
- The Android package id, backend hosts and SDK presence. Unverified.
- The Ryanair v PR Aviation holding. Taken from secondary summaries only; the judgment text was not retrieved.
