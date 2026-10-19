# 0005. Harvest the session from a real Chrome window

Date: 2026-10-19
Status: accepted

## Context

[ADR 0001](0001-session-from-the-browser.md) gets past DataDome by replaying the user's browser
session, imported with `auth login --cookies FILE` or `--har FILE`. That works, but it asks the user
to install a cookie-export extension (or record a HAR), find the file and pass it, every time the
session expires. It is the wrong kind of friction for a command a person runs on their own machine,
and a plain cookie export without a user agent is fragile: the same cookies were challenged when
replayed with the default user agent and accepted with the browser's own (see
`docs/leroymerlin-api-discovery.md` §2).

The browser already holds the session. Chrome exposes it over the DevTools protocol, and the cli can
drive a browser it starts itself. Two constraints shape how:

- **Chrome 136+ ignores `--remote-debugging-port` on the default profile.** Attaching to the Chrome a
  person is already using therefore does not work, even with the flag set.
- **A brand-new Chrome profile has no trusted `datadome` cookie.** A fresh profile can load the site
  and still get challenged when the cli replays what it read, because DataDome trust was never
  earned in that profile.

What does work is a copy of the real profile: Chrome decrypts the copied cookie store with the same
OS keyring, the trusted cookie travels with it, and because the copy is not the default profile the
DevTools port opens.

## Decision

Harvest from a copy of the user's real Chrome profile by default.

- `auth harvest` resolves the profile from `--profile`, then `LEROYMERLIN_CHROME_PROFILE`, then
  auto-detection: `~/.config/google-chrome`, `~/.config/chromium`, `~/.config/google-chrome-beta`,
  `~/.config/chromium-browser` on Linux, and the usual `Application Support` / `LocalAppData`
  locations on macOS and Windows. A profile qualifies when it has `Default/Cookies` or
  `Default/Network/Cookies`.
- It copies **only** `Local State` (the key reference), the cookie store (`Default/Cookies` or
  `Default/Network/Cookies`, plus `-journal`, `-wal`, `-shm` when present) and `Default/Preferences`
  into a fresh `0700` directory under `$XDG_DATA_HOME/leroymerlin-cli`. The rest of the profile is
  never read or copied.
- It launches Chrome (or Chromium, Brave, Edge) **headed** on that copy, with an explicit free
  `--remote-debugging-port` bound to `127.0.0.1` (not `localhost`, which can resolve to `::1`). The
  copy is what makes the port work on Chrome 136+.
- The copy and the browser are removed on every path out, including errors: `PrepareProfile` returns
  a cleanup that `Run` defers with a named return, and the allocator's cancel kills the browser
  first. A copy is only made when a profile is found; a failed copy is removed before returning.
- It navigates to `https://www.leroymerlin.es/` and polls every second, up to `--timeout` (default 5
  minutes), until the challenge is clear: the title is no longer the challenge's, the body no longer
  asks to enable JavaScript, no `captcha-delivery` frame is present, and a `datadome` cookie exists.
  It never tries to solve the challenge itself; the window stays open for a person.
- **Attach instead of launch** when a DevTools endpoint is already up: `--browser-url`, then
  `LEROYMERLIN_CHROME_URL`, then a probe of `http://127.0.0.1:9222`.
- **Fresh profile as the last fallback** when no real profile is found. It persists under
  `$XDG_DATA_HOME/leroymerlin-cli/chrome`, and it may need a human-cleared captcha because it starts
  without trust.
- It reads the cookies and `navigator.userAgent` over CDP, renders them as a Cookie-Editor-shaped
  JSON export with a `userAgent` field, and hands them to `ParseExport` and `config.SaveSession` —
  the same code `auth login` uses. `--user-agent` overrides the browser's.
- The command refuses while `LEROYMERLIN_COOKIEJAR` is set: an env session would shadow the one it
  is about to store.
- The DevTools protocol comes from `github.com/chromedp/chromedp` (pure Go). No browser is bundled
  or downloaded; the system browser is used.

## Consequences

- **The copy is a cookie store.** It is `0700`, exists only for the run, and is removed on exit. A
  hard kill (`SIGKILL`, power loss) can leave one behind under
  `$XDG_DATA_HOME/leroymerlin-cli/harvest-profile-*`; it is still `0700` and holds only the cookie
  store, not the profile.
- **The OS keyring must be available to the same user.** On Linux Chrome decrypts v11 cookies with a
  key in the login keyring (or the desktop's equivalent). If the copy cannot be decrypted, the
  window shows a logged-out site and the harvest times out with that hint instead of storing a
  session that does not work. The same rule covers macOS Keychain and Windows DPAPI.
- **Chrome 136+ cannot be attached to on its default profile.** The copy is the supported path;
  `--browser-url` only works for a browser started with a non-default profile and a debugging port.
- **A fresh profile is a last resort.** It has no trust, so it may wait for a captcha. A real profile
  supplies the trusted cookie and normally clears immediately.
- **The profile is not in the session directory.** The copy is under the data directory; `auth
  logout` removes the stored session file, not the copy (which is already gone).
- **Not a bypass.** The browser is a real browser, and a human clears the challenge. When DataDome
  challenges again, the harvest waits rather than solving.
- **`auth login` stays**, for scripts, CI (`LEROYMERLIN_COOKIEJAR`), a machine without a display, and
  a session a person already has as a file. For most interactive users the harvest replaces the
  manual export.
- **Dependency policy.** `chromedp` is added for this; the TLS hello remains `utls`, the HTTPS client
  remains `net/http`. No test launches a browser: the browser step is integration only, and the
  profile detection, copy plan, cleanup, port and cookie mapping are unit-tested.
