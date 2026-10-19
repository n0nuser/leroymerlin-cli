# 0002. Chrome TLS fingerprint via utls, the one networking exception

Date: 2026-10-19
Status: accepted

## Context

DataDome scores the TLS ClientHello along with the cookie, the user agent and other signals. Go's `crypto/tls` hello is distinctive, so a request carrying a real Chrome session cookie and a Chrome user agent, but sent with a Go hello, is an inconsistent pair. Avoiding inconsistent pairs like that is the point of replaying the browser's session (ADR 0001).

The project keeps to the standard library for networking otherwise.

## Decision

Requests to the storefront go through `github.com/refraction-networking/utls` with `HelloChrome_Auto` (Chrome 133 in utls v1.8.2). The TLS layer is the only non-stdlib piece of the HTTP stack, and it is isolated in `internal/leroymerlin/transport.go` behind `ChromeTransport()`.

- **ALPN is pinned to `http/1.1`.** That lets a stock `net/http.Transport` carry the connection. JA3 hashes extension types, not ALPN values, so the JA3 stays Chrome's. JA4 does encode the first ALPN value (`h1` instead of Chrome's `h2`). If that turns out to matter, the fix is HTTP/2 over the same dialer, and nothing outside `transport.go` changes.
- **The user agent comes from the session.** A HAR import records the browser's user agent and its `Sec-CH-UA*` client hints. Otherwise a Chrome 133 default is sent with matching hints, and `--user-agent` can override it.
- **No proxy.** `net/http` runs its own TLS through a proxy tunnel, so the storefront transport ignores proxy environment variables.
- **Other HTTP traffic uses the stdlib.** The updater (GitHub) is not the storefront and uses plain `net/http`.

Dependency policy, following the reference layout: cobra and pflag (CLI), adrg/xdg (paths), pelletier/go-toml/v2 (config), utls (this ADR). modernc.org/sqlite arrives with watch state. Nothing else.

## Consequences

- **Fingerprint drift.** Chrome changes its hello every few releases. Updating utls is part of keeping the cli working, and `doctor` is how breakage shows up (exit 5, `challenged: true`).
- **Not a solver.** utls does not run JavaScript and cannot pass a challenge. It only avoids presenting a Go-shaped handshake beside a browser's cookie.
- **Testing.** Tests use a local TLS server to check that the hello completes a handshake and that certificates are still verified (`transport_test.go`).
