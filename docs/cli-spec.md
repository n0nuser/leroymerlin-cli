# leroymerlin-cli spec

Status: draft, 2026-10-19. Vocabulary is in `../AGENTS.md`. Endpoint facts are in `leroymerlin-api-discovery.md`.

## 1. Name

The binary is `leroymerlin` and the module is `github.com/n0nuser/leroymerlin-cli`. It is a single static Go binary.

## 2. One-liner

Search leroymerlin.es and check stock at your store from the terminal. Unofficial and read-only.

## 3. Usage

```
leroymerlin [global flags] <noun> <verb> [args] [flags]
```

Commands are noun-verb. The one exception is `login`, a top-level shortcut for `auth login`.

## 4. Command tree

Shipped:

```
auth       login | harvest | status | logout
login      (same as auth login)
profile    list | use | remove
search     [keywords...] (filters, sort, pages)
product    show REF | stock REF        (alias: item)
store      list | current
category   list
watch      add (search|product) | list | remove | check | run | events
sink       list | test
config     path | list | get | set
doctor
mcp        (serve) | install claude-code|codex|cursor
skills     list | get
update
completion bash|zsh|fish|powershell
```

### auth

- **`auth login [--cookies FILE | --har FILE | --cookies-stdin] [--cookies-format auto|json|netscape|header|har] [--user-agent UA] [--no-check]`**
  - **Input.** Imports a browser export. With no flag and a terminal, it prompts for a path; without a terminal, that is exit 2.
  - **Formats.** Cookie JSON (a plain array, the `{"url","cookies"}` wrapper, or the `{"userAgent","cookies"}` wrapper a `document.cookie` script writes), DevTools cookie lists, Netscape `cookies.txt`, a `Cookie:` header, and HAR 1.2. A HAR is replayed in time order: request cookies, then the response's `Set-Cookie`. A HAR and the `userAgent` wrapper also give the browser's user agent, so the cookies and the handshake match the browser that earned them.
  - **Filtering.** Only cookies for `leroymerlin.es` and its subdomains are kept.
  - **Storage.** The session is saved 0600, atomically.
  - **Check.** Unless `--no-check`, one request to `/` reports `session_valid`. A challenge there does not undo the import.
  - **Refusal.** Refused (exit 2) while `LEROYMERLIN_COOKIEJAR` is set.
- **`auth harvest [--profile DIR] [--browser-url URL] [--chrome PATH] [--user-agent UA] [--timeout D]`** reads the session out of a real Chrome profile, so an export file is not needed (ADR 0005).
  - **Chrome profile.** `--profile`, else `LEROYMERLIN_CHROME_PROFILE`, else auto-detection: `~/.config/google-chrome`, `~/.config/chromium`, `~/.config/google-chrome-beta`, `~/.config/chromium-browser` (macOS and Windows use their `Application Support` / `LocalAppData` roots). A profile qualifies when it has `Default/Cookies` or `Default/Network/Cookies`. This `--profile` is the **Chrome** profile directory, not the session profile: choose the session profile with `LEROYMERLIN_PROFILE` or `default_profile`, because the local flag shadows the global one inside this command.
  - **The copy.** Only `Local State`, the cookie store (`Default/Cookies` or `Default/Network/Cookies`, with `-journal`/`-wal`/`-shm`) and `Default/Preferences` are copied into a `0700` directory under `$XDG_DATA_HOME/leroymerlin-cli`. Chrome 136+ ignores `--remote-debugging-port` on the default profile, so the copy is what lets the port open; the trusted `datadome` cookie travels with it. The copy and the browser are removed on every exit path, errors included.
  - **Launch.** `--chrome` / `LEROYMERLIN_CHROME`, else a common browser name in `PATH`, else the usual install locations. Headed, on the copied profile, with a free DevTools port bound to `127.0.0.1`.
  - **Attach mode.** `--browser-url`, else `LEROYMERLIN_CHROME_URL`, else a probe of `http://127.0.0.1:9222`; when one answers, that Chrome is driven instead and no profile is copied.
  - **Fallback.** With no real profile found, it launches a fresh persistent profile at `$XDG_DATA_HOME/leroymerlin-cli/chrome`. A fresh profile has no trust, so it may need a hand-cleared captcha.
  - **Wait.** It opens `https://www.leroymerlin.es/` and polls every second, up to `--timeout` (default 5m), until the page is the storefront (title is not the challenge's, no `captcha-delivery` frame, no JavaScript wall) and a `datadome` cookie exists. The window stays open so a person can clear a captcha by hand; the cli never solves it. If a copied profile cannot be decrypted (the OS keyring must be available to the same user), the timeout says so.
  - **Read.** Cookies and `navigator.userAgent` come over CDP, are rendered as a `{"userAgent","cookies"}` export (the shape `auth login` reads) and go through the same `ParseExport` and `SaveSession` (0600). `--user-agent` overrides the browser's.
  - **Output.** `{profile,path,import_format,skipped_other_domains,session,source,chrome_profile}`. `source` is `attach`, `profile` or `fresh`.
  - **Refusal.** Refused (exit 2) while `LEROYMERLIN_COOKIEJAR` is set, because that session would shadow the stored one.
- **`auth status [--check]`** prints source (`file`, `env`, `none`), cookie names, whether `datadome` is present and its expiry, the user agent and the import time. Values are never printed.
- **`auth logout`** deletes this profile's session file. The browser session is not touched.

### profile

- **`profile list`**, **`profile use NAME`** (writes `default_profile`), **`profile remove NAME [--yes]`**.
- **Selection order:** `--profile` > `LEROYMERLIN_PROFILE` > `default_profile` > `default`.
- **Names:** `[A-Za-z0-9][A-Za-z0-9_-]{0,63}`, so a name cannot escape the sessions directory.

### store / category

- **`store list [--query Q] [--compact]`** reads `/sitemap-store1.xml`. Output: `[{slug,name,compact,url}]`, sorted by name. No session needed.
- **`category list [--parent PATH] [--depth N] [--query Q]`** reads `/sitemap-category1.xml`. Output: `[{path,name,depth,parent,url}]`, sorted by path. `PATH` is the URL after `/productos/`. An unknown `--parent` is exit 4.
- **Query matching:** case-insensitive, with Spanish accents folded.

### search

- **`search [keywords...] [--category PATH] [--brand B]... [--min-price N] [--max-price N] [--in-stock] [--sold-by-leroy-merlin] [--sort relevance|price-asc|price-desc] [--page N] [--limit N]`**
  - **Target.** Keywords search `/search?q=`; `--category` browses `/productos/<path>/` instead. Giving both is a usage error, because the site has no category filter on a text search.
  - **Filters.** `--brand` maps to the site's Marca facet, `--min-price`/`--max-price` to its price range, `--sold-by-leroy-merlin` to its "Vendido por Leroy Merlin" toggle.
  - **Availability.** `--in-stock` is applied by the cli to the products the page embeds: the site has no stock facet on a search without a selected store.
  - **Output.** `{query,category,page,pages,total,sort,sorts,filters,products:[{ref,name,brand,url,price,price_without_tax,rating,seller,seller_type,available,sponsored,position}],shown}`.
  - **Embedded slice.** Results are server-rendered, but the site lazy-loads the tail of every page card by card. The cli reads only what the page embedded (18 products in the 2026-10-19 captures) and reports `total` and `pages` as the site states them. It never fans out into per-card requests.

### product

- **`product show REF`** reads one product. `REF` is the 8-digit reference or a full product URL. A bare reference costs one request: the site redirects `/search?q=<ref>` to the product page, which is then parsed as the product.
  - **Output.** `{ref,name,brand,url,description,gtin,image,price,price_without_tax,in_stock,available,rating,reviews,seller,seller_type,store,deliveries}`.
- **`product stock REF`** narrows that to `{ref,name,store,online,deliveries}`.
  - **`store`** is the store selected in the session, or `null` when there is none.
  - **`deliveries`** is what the page states: `{type,stock,status,time,price}`. With a store selected the rows are `storeDelivery` (pickup at that store, free) and `homeDelivery`; `status` is `ONSITE`, `ONORDER` or `OUTOFSTOCK`, and a price of 0 means free.
- **`item`** is an alias for `product`.

### watch / sink

- **`watch add search [keywords...] [--name N] [--interval D] [--pages N] [--notify SINK]... [--emit-initial]`** saves a search with the same flags as `search`.
- **`watch add product REF [--name N] [--interval D] [--notify SINK]... [--emit-initial]`** saves one product and its URL.
  - A watch is baselined when it is created: the first check reports only changes. `--emit-initial` reports the current products as `product.new` instead.
  - The interval defaults to `watch.interval` from config, then 5m; the floor is 30s.
- **`watch list [--all-profiles]`**, **`watch remove NAME [--yes]`**.
- **`watch check [NAME] [--all]`** fetches each watch once, prints its events and commits the new state. **`watch run [NAME] [--all]`** repeats until interrupted.
- **`watch events [NAME] [--since 24h] [--limit N]`** reads stored events.
- **Event types.** `product.new`, `product.price_changed` (`{from,to}`), `product.in_stock`, `product.out_of_stock`, `product.stock_changed` (`{from,to,from_status,to_status}`, when a store reports a count or status), `product.removed`.
- **Event shape.** `{type,watch,profile,at,product:{ref,name,url,price,available,in_stock,store,stock},change}`.
- **Sinks.** `--notify NAME` copies each event to `[sinks.NAME]` in config.toml, type `ntfy`, `webhook` or `exec`. A failing sink is reported on stderr and never undoes the check. An `exec` sink receives one event as JSON on stdin.
- **`sink list`**, **`sink test NAME`**.

### store current

Prints the store selected in the session (`{id,name}`). Without one it exits 4 and explains how to choose a store. Stock always follows that store.

### doctor

`doctor` runs five checks: config, session, datadome cookie, sitemaps, then one request to `/` with the session.

- **Output:** `{ok,profile,session_valid,challenged,checks:[{name,ok,detail}]}`.
- **Exit codes:** 0 when every check passes, 3 when there is no session, 5 when the storefront challenges the session, otherwise 1.

### mcp

`leroymerlin mcp` is a stdio JSON-RPC 2.0 MCP server.

- **Methods:** `initialize`, `ping`, `tools/list`, `tools/call`.
- **Protocol revisions:** 2024-11-05, 2025-03-26 and 2025-06-18.
- **Tools:** `store_list`, `category_list`, `store_current`, `search`, `product_show`, `product_stock`, `doctor`.
- **How a tool runs.** Each tool runs its CLI argv in-process with `--no-input --error-format json`, so the output is what the command prints. A failed command comes back as `isError: true` with the error envelope.
- **Errors.** Unknown tools or arguments, and wrongly typed arguments, return JSON-RPC `-32602`.
- **Install.** `mcp install` writes `~/.claude.json`, `~/.cursor/mcp.json` (both JSON, other keys kept) or `~/.codex/config.toml` (edited as text, comments kept). It is idempotent and reports `added`, `updated` or `unchanged`.

### update

Checks the latest stable GitHub release, verifies the archive's SHA-256 against `checksums.txt` and replaces the binary atomically.

- **Development builds** are refused with exit 2.
- **Automatic check.** Interactive commands check at most once a day, except `update`, `mcp`, `auth`, `login`, `completion` and `help`. Never with `--no-input` or `LEROYMERLIN_NO_UPDATE_CHECK=1`.

## 5. Global flags

| flag | default | meaning |
|---|---|---|
| `--profile NAME` | config default | which session (and so which store) acts |
| `--format json\|jsonl\|pretty` | `json` | stdout rendering |
| `--no-color` | off | also `NO_COLOR`, `TERM=dumb`, non-TTY |
| `--error-format text\|json` | `text` | stderr error rendering (env `LEROYMERLIN_ERROR_FORMAT`) |
| `--no-input` | off | never prompt |
| `--debug` | off | request log on stderr: method, path, status, size, time. Cookies never appear |

## 6. I/O contract

- stdout carries data only: one JSON document, or one object per line with `jsonl`.
- stderr carries errors, prompts and debug output.
- Prompts appear only when stdin, stdout and stderr are all TTYs and `--no-input` is absent.
- Secrets never come from argv. Cookies come from a file, stdin or `LEROYMERLIN_COOKIEJAR`.
- Remote text has control characters stripped before it reaches a terminal.

## 7. Exit codes

| code | meaning |
|---|---|
| 0 | success |
| 1 | generic failure |
| 2 | usage: bad flag, argument, profile name or config key |
| 3 | auth: no session |
| 4 | not found |
| 5 | blocked: DataDome challenge (recognised by `Server: DataDome` / `X-Datadome` headers or a `captcha-delivery.com` body, not by status alone), or 429 |
| 6 | network, timeout or 5xx |
| 7 | the site answered with a status or shape the cli does not expect |
| 130 | interrupted |

## 8. Errors

- **Text errors** are one sentence that says what happened and what to do.
- **`--error-format json`** prints `{code,category,retryable,message,http_status,endpoint,suggested_commands,issue_url}`.
- **Exit 7** also prints an `issue_url` that prefills `.github/ISSUE_TEMPLATE/api-change.yml`. It includes:
  - version and OS/arch;
  - the command path (no argument values);
  - the endpoint and status;
  - a 300-byte body excerpt with token-like strings redacted.

## 9. Network behaviour

- **Base URL:** `https://www.leroymerlin.es`. `LEROYMERLIN_BASE_URL` overrides it, for tests.
- **TLS:** a Chrome hello via utls with ALPN `http/1.1` (ADR 0002).
- **Headers:** browser navigation headers; `Accept-Language: es-ES`; the session's user agent, or a Chrome 133 default. The `Sec-CH-UA`, `-Mobile` and `-Platform` client hints come from the HAR, or match the default user agent. With a custom `--user-agent` and no HAR, no hints are sent.
- **User agent:** cookies and the user agent are scored together. A cookie export with no user agent is replayed as the default Chrome 133 and may be challenged even when the cookies are fresh; `--user-agent` with the browser's string fixes it. Prefer an export that carries it.
- **No proxy.** The storefront transport ignores `HTTPS_PROXY`/`HTTP_PROXY`, because a proxied connection would use Go's TLS hello instead of Chrome's.
- **Rate:** one request at a time, at least 1.5 s apart plus up to 30% jitter. This holds across invocations too: the time of the last request is kept in `$XDG_STATE_HOME/leroymerlin-cli/last-request`. Two processes started at the same instant can still race.
- **Redirects** are followed within leroymerlin.es only, five at most. The session is never sent to another host.
- **`auth harvest` does not use this client.** It drives a real browser over CDP; the rate gate and the transport below apply to the commands that read pages with `net/http`. Chrome 136+ does not open a DevTools port on its default profile, which is why the harvest launches a copy. The copy's cookies are read-only for the CLI: the browser owns the session while it runs.
- **Cookie rotation.** The session is the HTTP client's cookie jar, so `Set-Cookie` on every hop, redirects included, updates the in-memory session. A file session is saved after the command, whether it succeeded or not. An env session never is.

## 10. Config, state, env

| what | path | mode |
|---|---|---|
| config | `$XDG_CONFIG_HOME/leroymerlin-cli/config.toml` (`LEROYMERLIN_CONFIG` overrides) | 0644 |
| sessions | `$XDG_CONFIG_HOME/leroymerlin-cli/sessions/<profile>.json` | 0600, dir 0700, atomic |
| state (watch) | `$XDG_DATA_HOME/leroymerlin-cli/state.db` | 0600 file, dir 0700, sqlite WAL |
| harvest profile (fresh fallback) | `$XDG_DATA_HOME/leroymerlin-cli/chrome` | browser profile, dir 0700 |
| harvest copy | `$XDG_DATA_HOME/leroymerlin-cli/harvest-profile-*` | 0700, removed on exit |
| update stamp | `$XDG_STATE_HOME/leroymerlin-cli/update-check` | 0600 |
| rate-gate stamp | `$XDG_STATE_HOME/leroymerlin-cli/last-request` | 0600 |

`config.toml` keys: `default_profile`, `watch.interval`, and one `[sinks.NAME]` table per sink (`type`, plus `url`/`topic`/`token_file` or `command`). Unknown keys are errors.

Env: `LEROYMERLIN_PROFILE`, `LEROYMERLIN_CONFIG`, `LEROYMERLIN_COOKIEJAR`, `LEROYMERLIN_ERROR_FORMAT`, `LEROYMERLIN_BASE_URL`, `LEROYMERLIN_NO_UPDATE_CHECK`, `NO_COLOR`. Precedence: flags > env > config.

## 11. Safety

- Read-only (ADR 0003).
- Never phones home except for the update check (GitHub, interactive only).
- Ctrl-C exits within one in-flight request, with code 130.

## 12. Open items

- **Pickup status branches.** Store 037 (Salamanca) is verified for `storeDelivery` with `ONORDER` and home delivery with `ONSITE`. A capture of pickup `ONSITE` and pickup `OUTOFSTOCK` would pin the remaining human labels; the parser already carries the raw status.
- **The lazy tail of a result page.** The site embeds about 18 products and lazy-loads the rest card by card. `search` reports the embedded slice and the site's total; chasing the tail would be dozens of requests per page.
- **HTTP/2** if JA4's ALPN value turns out to matter (ADR 0002).

## 13. Layout

```
cmd/leroymerlin/       entrypoint
cmd/docsgen/           renders docs-site/content/docs/reference from the command tree
internal/cli/          one file per noun; skills/ holds embedded agent docs
internal/leroymerlin/  client: transport, session, sitemaps, page parsers; testdata/
internal/config/       paths, config.toml, session files
internal/output/       json/jsonl/pretty, error envelope, exit codes
internal/store/        sqlite state: watches, seen products, events
internal/watch/        diff logic between two checks
internal/sink/         ntfy, webhook and exec delivery
internal/mcp/          stdio server, tool table, harness install
internal/updater/      release check, checksum, replace
docs/                  this spec, ADRs, API discovery, research
```
