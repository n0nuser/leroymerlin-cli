# Troubleshooting

Most failures here are a stale browser session or the site's IP reputation, not a broken command. Start with the two diagnostics, then find your case below.

## start with these two commands

### `leroymerlin doctor`

Runs the whole chain: config, the stored session, the `datadome` cookie, the user agent, the sitemaps, and one request to the storefront. Each line says `ok` or `FAIL` and why.

```bash
leroymerlin doctor --format pretty
```

Exit `3` means there is no session for the active profile; exit `5` means the storefront challenged the session. Both point at the fixes below.

### `leroymerlin auth status --check`

Shows which profile and session source are active (`file`, `env` or `none`), the cookie names (never values), the user agent and the import time, then makes one request to check whether the storefront accepts it.

```bash
leroymerlin auth status --check --format pretty
```

If this passes but a command still fails, the problem is that command's data, not the session. If it fails with a challenge, the session needs refreshing.

Add `--error-format json` to either command when a script needs the machine-readable envelope.

## questions

### "the cli says `bot protection challenged the request`"

The site's DataDome layer refused the request; the cli exits `5` and prints:

```
leroymerlin: leroymerlin.es bot protection challenged the request. Open leroymerlin.es in the browser your session came from, pass any check it shows, then re-import the session
```

Import a fresh session, then check it:

```bash
leroymerlin auth harvest
leroymerlin doctor --format pretty
```

Or, if you already have an export:

```bash
leroymerlin auth login --cookies ~/Downloads/leroymerlin-cookies.json
```

If `LEROYMERLIN_COOKIEJAR` is set, `auth login` refuses (`... is set, so a stored session would be ignored. Unset it to log in`). Unset it, or keep using the env export and refresh that file instead.

### "my normal browser loads the site fine, but the cli is challenged"

The browser is using its live session; the cli replays a **stored copy** of one. Once that copy is stale, the site challenges it even though the browser still works. This is the most common case.

```bash
leroymerlin auth harvest
leroymerlin doctor --format pretty
```

`auth harvest` reads the session out of your real Chrome profile, so it usually fixes this in one step.

### "`auth harvest` opens a Chrome window and still gets blocked"

By default `auth harvest` does not use an empty browser. It copies your real Chrome profile's cookie store (`Local State`, `Default/Cookies` or `Default/Network/Cookies`, `Default/Preferences`) into a private temporary directory, launches Chrome on that copy and reads the trusted `datadome` cookie from it. The copy and the window are removed when it exits.

If it is still blocked:

- Make sure it picked the right profile. `--profile ~/.config/google-chrome` (env `LEROYMERLIN_CHROME_PROFILE`) names the **Chrome** profile; the session profile is chosen with `LEROYMERLIN_PROFILE` or `default_profile`.
- If the timeout says Chrome could not decrypt the copied cookies, the OS keyring must be available to the same user (GNOME Keyring, KWallet, macOS Keychain, Windows DPAPI). Unlock it and try again.
- With no real profile found it falls back to a fresh persistent profile under `$XDG_DATA_HOME/leroymerlin-cli/chrome`. A fresh profile has no trust and may need a hand-cleared captcha.
- `auth harvest` needs a display. On a headless machine, or over SSH without one, use `auth login` with an export instead.
- If the window shows the site but the cli is still challenged, go to the IP item below.

### "the page says `Access temporarily restricted` and shows an incident ID"

That is an IP-reputation block from DataDome, not a cookie problem. Re-importing will not help while the block lasts. It is temporary: usually about one to four hours, occasionally longer.

Stop hitting the site from that network, lower your request rate (or stop the watch loop), wait, then try once. If the first request is still blocked, wait longer rather than retrying in a loop.

### "how do I export cookies?"

`auth login` accepts:

- a Cookie-Editor JSON array (also its `{"url","cookies"}` and `{"userAgent","cookies"}` wrappers);
- a Netscape `cookies.txt`;
- a HAR 1.2 saved from DevTools with "Allow to generate HAR with sensitive data";
- a raw `Cookie:` header.

Only `leroymerlin.es` cookies are kept, so a whole-browser export is fine. Export after choosing a store. An **encrypted** Cookie-Editor export must be decrypted first: the cli reads the plain formats, not the encrypted blob. A plain JSON array carries no user agent, so pass the browser's own string if the site challenges it:

```bash
leroymerlin auth login --cookies ~/Downloads/leroymerlin-cookies.json \
  --user-agent 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36'
```

For scripts and CI, point `LEROYMERLIN_COOKIEJAR` at the export instead; it is read on each run and never written to disk.

### "stock only shows online and home delivery, no store pickup"

No store is selected in the session. Check:

```bash
leroymerlin store current --format pretty
```

Exit `4` says it plainly: choose a store on leroymerlin.es ("Elegir mi tienda"), then re-run `auth harvest` (or re-import the session). With a store selected, `product stock REF` adds a `storeDelivery` row with `in stock`, `on order` or `out of stock`, and a price of `0` means free pickup. Use one `--profile` per store to compare stores.

### "will this get me banned or is it allowed?"

The cli is read-only, uses a guest browser session, never logs in, and spaces requests at least 1.5 seconds apart with no bulk or mirror mode. It is a personal shopping tool, not a crawler.

The site's robots.txt disallows some of the paths the tool reads, including search queries with filters, sort or limit parameters and the `productpageservicesfunctionality` fragment; `docs/leroymerlin-api-discovery.md` records exactly what. Keep the rate low, keep watch loops small, and use it at your own risk. The project is unofficial and not affiliated with Leroy Merlin.

## still stuck?

Run `leroymerlin doctor --error-format json` and include its output, plus the command and exit code, in an issue. When the site changes shape, exit `7` already carries a prefilled issue link on stderr. The interface contract is in `docs/cli-spec.md`; endpoint facts are in `docs/leroymerlin-api-discovery.md`.
