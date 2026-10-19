# 0001. The session comes from the user's browser

Date: 2026-10-19
Status: accepted

## Context

Every page and XHR on www.leroymerlin.es is behind DataDome. A plain HTTP client gets a 403 challenge page, whatever user agent it sends. Only robots.txt and the sitemaps answer without a challenge (phase 0, `docs/reference-leroymerlin-feasibility.md`). The data the cli exists for (search, prices, stock at the user's store) is only in pages the bot protection guards. The research in `docs/research/leroymerlin-access.md` found no official API or feed that carries per-store stock.

Options considered:

1. **Solve the challenge.** Drive a stealth browser or pay a captcha-solving service. This is an explicit bot-detection bypass, carries the highest ToS risk and is an arms race. Rejected.
2. **Drive a real browser from the cli** (Chrome DevTools Protocol). It works, but it is heavy, slow and needs a display. Kept as a possible later backend.
3. **Replay the user's own browser session.** The person shops at leroymerlin.es in Chrome, chooses a store, and exports the cookies (or a HAR). The cli sends those cookies and that browser's user agent.

## Decision

Option 3. `auth login --cookies FILE | --har FILE | --cookies-stdin` imports an export. Only leroymerlin.es cookies are kept and stored in `$XDG_CONFIG_HOME/leroymerlin-cli/sessions/<profile>.json`, mode 0600, written atomically. `LEROYMERLIN_COOKIEJAR=FILE` uses an export without storing it, and the cli refuses to write an env-sourced session to disk. Cookies the site rotates in responses are written back for file sessions only.

No account is needed. No password is ever asked for. The cli never logs in by itself.

## Consequences

- **Re-importing.** When DataDome stops trusting the session, every command exits 5 until the user reloads the site in the same browser and imports the session again. `doctor` tells a challenged session from a working one.
- **Per-store stock.** Stock follows the store selected in the session. Comparing stores means one profile per store.
- **robots.txt.** It disallows `/search?q=*`, `*/productpageservicesfunctionality/*` (the stock service) and `?…storeid`. Those rules target crawlers rather than a client a person drives, but they still apply in spirit. The cli therefore keeps a human pace: requests go one at a time, at least 1.5 s apart, and there is no bulk mode.
- **Read-only.** No cart, checkout or account commands. Those endpoints are scored more strictly, and the project does not need them.
