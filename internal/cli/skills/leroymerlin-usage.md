# leroymerlin-cli usage

`leroymerlin` is an unofficial, read-only terminal client for leroymerlin.es (Spain). Output is JSON on stdout by default. Use `--format jsonl` to stream one object per line and `--format pretty` for humans. Errors go to stderr; `--error-format json` gives a machine-readable envelope.

## Sessions

leroymerlin.es is behind bot protection. The cli replays the session of the user's own browser, so it never logs in by itself and never asks for a password.

- `leroymerlin auth login --cookies FILE` or `--har FILE` imports the session. The user exports it from Chrome after choosing a store. No account is needed.
- `leroymerlin auth status` shows cookie names (never values). Add `--check` to also ask the storefront whether the session works, at the cost of one request.
- `LEROYMERLIN_COOKIEJAR=FILE` uses an export without storing it. Use it for CI.
- Stock is for the store selected in that browser session. Use one `--profile` per store.

Without a session only `store list`, `category list` and `doctor` work.

## Commands

| command | needs session | output |
|---|---|---|
| `store list [--query Q] [--compact]` | no | `[{slug,name,compact,url}]` |
| `category list [--parent PATH] [--depth N] [--query Q]` | no | `[{path,name,depth,parent,url}]` |
| `doctor` | no | `{ok,profile,session_valid,challenged,checks:[{name,ok,detail}]}` |
| `store current` | yes | `{id,name}` of the selected store, or exit 4 |
| `search [KEYWORDS] [--category PATH] [--brand B] [--min-price N] [--max-price N] [--in-stock] [--sold-by-leroy-merlin] [--sort relevance\|price-asc\|price-desc] [--page N] [--limit N]` | yes | `{query,page,pages,total,sort,filters,products:[...],shown}` |
| `product show REF` / `product stock REF` | yes | a product, or `{ref,name,store,online,deliveries}` |
| `watch add search / add product`, `watch list`, `watch check --all`, `watch events` | yes (add/check) | watch or events |
| `sink list`, `sink test NAME` | no | sinks from config.toml |
| `auth login / status / logout` | | session summary |
| `profile list / use / remove` | | |
| `config path / list / get / set` | | |

`REF` is the 8-digit product reference or a product URL. `search` reads the products the page embeds (about 18) and reports the site's total; it does not chase the lazy tail, so keep loops to a few pages.

Without a selected store the product page reports online availability and home delivery only. `store current` exits 4 in that case: the fix is a cookie export that includes HttpOnly cookies (devtools or a HAR), taken after choosing a store.

## Exit codes

| code | meaning | what to do |
|---|---|---|
| 0 | success | |
| 1 | generic failure | |
| 2 | usage: bad flag or argument | fix the call |
| 3 | auth: no session | ask the user to export cookies and run `auth login` |
| 4 | not found | |
| 5 | bot protection challenged the session, or rate limited | ask the user to reload leroymerlin.es in their browser and re-import. **Do not retry in a loop.** |
| 6 | network or timeout | retry later |
| 7 | the site changed its pages | report it with the issue link on stderr |
| 130 | interrupted | |

## Etiquette

Requests are sequential and spaced at least 1.5 s apart. Keep agent loops small: the cli is for one person shopping, not for crawling the catalogue.
