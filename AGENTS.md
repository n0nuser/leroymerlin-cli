# leroymerlin-cli

An unofficial, read-only terminal client for leroymerlin.es. It covers stores, categories and stock at the user's store, and it talks to the storefront with the user's own browser session.

## Working in this repo

- Read `docs/cli-spec.md` (the contract) and `docs/leroymerlin-api-discovery.md` (what the site does) before changing behaviour. Update them in the same change.
- `go build ./... && go test ./... && go vet ./...` must be clean, and so must `make check` (gofmt and staticcheck too). After touching a command's help or flags, run `make docs`. CI fails on a stale reference.
- Layering: `cli` orchestrates. `leroymerlin` is network and parsing; `config` is disk; `store` is the local sqlite state; `watch` is the diff between two checks; `sink` delivers events; `harvest` owns the real-Chrome profile copy and CDP session. `leroymerlin` never touches the disk, `config` never touches the network, and `store` never calls the site.
- Every parser is pinned to a real capture in `internal/leroymerlin/testdata/` with a round-trip test. Hand-written fixtures say `synthetic` in their file name. Every bug fix comes with a regression test whose comment names the bug.
- Tests never talk to leroymerlin.es. Use `httptest` and `LEROYMERLIN_BASE_URL`.
- Live captures (HAR, cookie exports) are secrets. Keep them outside the repo; `.gitignore` blocks `*.har` and `*cookies*.json`. Strip cookies, `Set-Cookie` headers and context ids before anything becomes a fixture.
- Do not add a way past the bot protection beyond the session replay of ADR 0001 and ADR 0002: no solvers, no stealth browsers, no proxies.
- Keep the request pace (one at a time, at least 1.5 s apart). No bulk or mirror features.
- Distribution is `go install`, the `curl|sh` script in `scripts/`, the release archives, and the `npm/` wrapper for `npx`. npm publishing runs from `.github/workflows/npm-publish.yml` on `v*` tags through npm Trusted Publishing (OIDC); there is no npm token in the repo. Before the first publish the owner must add a GitHub Actions trusted publisher on npmjs.com (owner `n0nuser`, repo `leroymerlin-cli`, workflow `npm-publish.yml`, environment empty). Homebrew, Scoop and AUR blocks sit commented in `.goreleaser.yaml`; enabling them needs a `homebrew-tap` repo plus `HOMEBREW_TAP_GITHUB_TOKEN`, a `scoop-bucket` repo plus `SCOOP_BUCKET_GITHUB_TOKEN`, and an AUR account plus `AUR_SSH_PRIVATE_KEY`.
- Don't mention, credit or copy other projects in code, docs or commits.

## Language

**Store**:
A physical Leroy Merlin store, identified by the slug of its page `/tiendas/<slug>.html`.
_Avoid_: shop, centre, tienda

**Selected store**:
The store chosen in the browser session ("Elegir mi tienda"). Stock is always for this store.

**Stock**:
Availability of a product at the selected store, as the site reports it.
_Avoid_: inventory, availability (when you mean the store-level number)

**Category**:
A node of the product taxonomy, identified by its **path**: the URL part after `/productos/`.

**Product**:
One article, identified by its 8-digit reference (the number at the end of `/productos/<slug>-<ref>.html`).
_Avoid_: item, SKU (marketplace offers can have their own ids)

**Session**:
The cookies (and user agent) of the user's browser, imported with `auth login` or read from `LEROYMERLIN_COOKIEJAR`. It is not an account login.
_Avoid_: login, token, credentials

**Profile**:
One named session, and therefore one selected store. Use one profile per store to compare stores.

**Challenge**:
The bot protection's answer in place of the page asked for (exit 5). The fix is always on the user's side: refresh the browser session and re-import it.
_Avoid_: ban, captcha (the cli never sees or solves one)

**Watch**:
A saved search or product whose price and stock changes are reported as **Events**.

**Event**:
One change a `watch check` reports: `product.new`, `product.price_changed`, `product.in_stock`, `product.out_of_stock`, `product.stock_changed` or `product.removed`.

**Sink**:
A destination in `config.toml` (`ntfy`, `webhook` or `exec`) that receives a copy of a watch's events.
