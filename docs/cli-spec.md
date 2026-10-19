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
auth       login | status | logout
login      (same as auth login)
profile    list | use | remove
store      list
category   list
config     path | list | get | set
doctor
mcp        (serve) | install claude-code|codex|cursor
skills     list | get
update
completion bash|zsh|fish|powershell
```

Planned. These are blocked on captured pages (see §12):

```
search     [keywords...] [--category PATH] [--brand B]... [--min-price N] [--max-price N]
           [--sort relevance|price_asc|price_desc|...] [--in-stock] [--limit N] [--page N]
item       show REF | stock REF        (REF: product URL or reference number)
store      current                     (the store selected in the session)
watch      add (search|item) | list | remove | check | run | events
sink       list | test
```

### auth

- **`auth login [--cookies FILE | --har FILE | --cookies-stdin] [--cookies-format auto|json|netscape|header|har] [--user-agent UA] [--no-check]`**
  - **Input.** Imports a browser export. With no flag and a terminal, it prompts for a path; without a terminal, that is exit 2.
  - **Formats.** Cookie JSON (array or `{"url","cookies"}` wrapper), DevTools cookie lists, Netscape `cookies.txt`, a `Cookie:` header, and HAR 1.2. A HAR is replayed in time order: request cookies, then the response's `Set-Cookie`. It also gives the browser's user agent.
  - **Filtering.** Only cookies for `leroymerlin.es` and its subdomains are kept.
  - **Storage.** The session is saved 0600, atomically.
  - **Check.** Unless `--no-check`, one request to `/` reports `session_valid`. A challenge there does not undo the import.
  - **Refusal.** Refused (exit 2) while `LEROYMERLIN_COOKIEJAR` is set.
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

### doctor

`doctor` runs five checks: config, session, datadome cookie, sitemaps, then one request to `/` with the session.

- **Output:** `{ok,profile,session_valid,challenged,checks:[{name,ok,detail}]}`.
- **Exit codes:** 0 when every check passes, 3 when there is no session, 5 when the storefront challenges the session, otherwise 1.

### mcp

`leroymerlin mcp` is a stdio JSON-RPC 2.0 MCP server.

- **Methods:** `initialize`, `ping`, `tools/list`, `tools/call`.
- **Protocol revisions:** 2024-11-05, 2025-03-26 and 2025-06-18.
- **Tools:** `store_list`, `category_list`, `doctor`.
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
- **No proxy.** The storefront transport ignores `HTTPS_PROXY`/`HTTP_PROXY`, because a proxied connection would use Go's TLS hello instead of Chrome's.
- **Rate:** one request at a time, at least 1.5 s apart plus up to 30% jitter. This holds across invocations too: the time of the last request is kept in `$XDG_STATE_HOME/leroymerlin-cli/last-request`. Two processes started at the same instant can still race.
- **Redirects** are followed within leroymerlin.es only, five at most. The session is never sent to another host.
- **Cookie rotation.** The session is the HTTP client's cookie jar, so `Set-Cookie` on every hop, redirects included, updates the in-memory session. A file session is saved after the command, whether it succeeded or not. An env session never is.

## 10. Config, state, env

| what | path | mode |
|---|---|---|
| config | `$XDG_CONFIG_HOME/leroymerlin-cli/config.toml` (`LEROYMERLIN_CONFIG` overrides) | 0644 |
| sessions | `$XDG_CONFIG_HOME/leroymerlin-cli/sessions/<profile>.json` | 0600, dir 0700, atomic |
| state (watch) | `$XDG_DATA_HOME/leroymerlin-cli/state.db` | planned |
| update stamp | `$XDG_STATE_HOME/leroymerlin-cli/update-check` | 0600 |
| rate-gate stamp | `$XDG_STATE_HOME/leroymerlin-cli/last-request` | 0600 |

`config.toml` keys: `default_profile`. Unknown keys are errors.

Env: `LEROYMERLIN_PROFILE`, `LEROYMERLIN_CONFIG`, `LEROYMERLIN_COOKIEJAR`, `LEROYMERLIN_ERROR_FORMAT`, `LEROYMERLIN_BASE_URL`, `LEROYMERLIN_NO_UPDATE_CHECK`, `NO_COLOR`. Precedence: flags > env > config.

## 11. Safety

- Read-only (ADR 0003).
- Never phones home except for the update check (GitHub, interactive only).
- Ctrl-C exits within one in-flight request, with code 130.

## 12. Open items

- **Search, product, stock and watch** need captured pages: search with filters, page 2, a category page, two product pages, any per-store stock panel, and a store switch. Planned shapes are in §4. The stock source is still unknown: it may be embedded in the product HTML, or it may come from the `productpageservicesfunctionality` fragment that robots.txt disallows.
- **`store current`** needs to know which cookie or context call holds the selected store.
- **HTTP/2** if JA4's ALPN value turns out to matter (ADR 0002).

## 13. Layout

```
cmd/leroymerlin/       entrypoint
cmd/docsgen/           renders docs-site/content/docs/reference from the command tree
internal/cli/          one file per noun; skills/ holds embedded agent docs
internal/leroymerlin/  client: transport, session, sitemaps, (planned) page parsers; testdata/
internal/config/       paths, config.toml, session files
internal/output/       json/jsonl/pretty, error envelope, exit codes
internal/mcp/          stdio server, tool table, harness install
internal/updater/      release check, checksum, replace
docs/                  this spec, ADRs, API discovery, research
```
