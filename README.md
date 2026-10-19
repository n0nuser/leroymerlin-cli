<p align="center">
  <img src=".github/assets/leroymerlin-cli-logo.svg" width="520" alt="leroymerlin cli logo">
</p>

<p align="center">
  <a href="https://github.com/n0nuser/leroymerlin-cli/releases"><img src="https://img.shields.io/github/v/release/n0nuser/leroymerlin-cli?display_name=tag&style=flat-square&label=release&color=1c1917" alt="release badge"></a>
  <a href="https://github.com/n0nuser/leroymerlin-cli/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/n0nuser/leroymerlin-cli/ci.yml?branch=main&style=flat-square&label=ci&color=1c1917" alt="ci badge"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-mit-1c1917?style=flat-square" alt="license badge"></a>
  <a href="https://pkg.go.dev/github.com/n0nuser/leroymerlin-cli"><img src="https://pkg.go.dev/badge/github.com/n0nuser/leroymerlin-cli.svg" alt="pkg.go.dev"></a>
</p>

---

`leroymerlin` is an unofficial terminal cli for leroymerlin.es. It lists stores and categories and checks stock at the store you buy from, so you know whether the bag of cement is on the shelf before you drive there. It compiles to one static Go binary. Output is json by default so everything pipes into `jq`; `--format pretty` is for humans. It is read-only: no cart, no checkout, nothing on your account.

[documentation](https://n0nuser.github.io/leroymerlin-cli/docs/) | [github](https://github.com/n0nuser/leroymerlin-cli)

## for AI agents

register the read commands as an MCP server in your harness:

```bash
leroymerlin mcp install claude-code   # also: codex, cursor
```

or read the embedded agent notes:

```bash
leroymerlin skills get leroymerlin-usage
```

confirm the wiring with one read (needs a session):

```bash
leroymerlin search "cemento cola" --limit 3 --format pretty
```

## why

buying materials means asking one question many times: does my store have this, and how many. the website answers it one click at a time. this cli answers it from a script, an agent, or a shell loop, with the same json every time.

## status

| | |
|---|---|
| `store list`, `category list` | work now, from the public sitemaps, no session needed |
| `auth harvest` (a real Chrome window), `auth login` (cookies or HAR), `doctor`, `profile`, `config`, `mcp`, `update` | work now |
| `search`, `product show`, `product stock`, `watch`, `sink` | work now, with your browser session |
| per-store pickup stock | works when the session has a store selected; otherwise stock shows online availability and home delivery |

## install

### one-liner

linux and macos:

```bash
curl -fsSL https://raw.githubusercontent.com/n0nuser/leroymerlin-cli/main/scripts/install.sh | sh
```

installs the latest release into `~/.local/bin`, verifying it against the release's
`checksums.txt` first. set `LEROYMERLIN_INSTALL_DIR` (or `PREFIX`) for another
directory, or `VERSION=<x.y.z>` to pin a release. on windows, use the archive or
`go install` below.

### go install

```bash
go install github.com/n0nuser/leroymerlin-cli/cmd/leroymerlin@latest
leroymerlin --version
```

### npx

publishing the wrapper is automated in `.github/workflows/npm-publish.yml`, which
runs on every `v*` tag and publishes through npm Trusted Publishing (OIDC). there
is **no npm token** in this repository. the one step left for the account owner on
npmjs.com, after the package exists the first time: open it, go to
**Settings -> Trusted Publishers -> Add a publisher -> GitHub Actions**, and set
owner `n0nuser`, repository `leroymerlin-cli`, workflow `npm-publish.yml`,
environment (empty). until that exists the workflow fails; it is separate from
the binary `release.yml` and does not affect releases.

after the first publish:

```bash
npx leroymerlin-cli search "cemento cola" --limit 3
```

the wrapper downloads the matching release binary on install and execs it; the
package lives in [npm/](npm).

### release binaries

download the archive and its checksums first, then verify before extracting:

```bash
gh release download --repo n0nuser/leroymerlin-cli --pattern 'leroymerlin_*_linux_amd64.tar.gz'
gh release download --repo n0nuser/leroymerlin-cli --pattern checksums.txt
sha256sum -c --ignore-missing checksums.txt
tar xzf leroymerlin_*_linux_amd64.tar.gz leroymerlin && install -m 755 leroymerlin ~/.local/bin/
```

every release ships `checksums.txt` with a github provenance attestation:

```sh
gh attestation verify checksums.txt --repo n0nuser/leroymerlin-cli
```

### from source

```bash
git clone https://github.com/n0nuser/leroymerlin-cli && cd leroymerlin-cli
make build     # bin/leroymerlin
make check     # vet, staticcheck, tests, gofmt
```

check the install:

```bash
leroymerlin auth harvest
leroymerlin store list --query madrid --format pretty
leroymerlin doctor
```

`go test ./...` is fully offline. one live smoke test is opt-in and needs an
imported session; it makes a single real search request:

```bash
LEROYMERLIN_LIVE_TEST=1 go test ./internal/cli -run TestLiveSearch -count=1
```

ready-to-run recipes are in [examples/](examples).

### other package managers

homebrew, scoop and aur are wired but disabled in `.goreleaser.yaml`, commented
out. each needs something this account does not have yet, and enabling one
without it fails `release.yml`:

- **homebrew**: a `n0nuser/homebrew-tap` repository and a
  `HOMEBREW_TAP_GITHUB_TOKEN` secret with write access to it.
- **scoop**: a `n0nuser/scoop-bucket` repository and a
  `SCOOP_BUCKET_GITHUB_TOKEN` secret with write access to it.
- **aur**: an AUR account for `n0nuser` and an `AUR_SSH_PRIVATE_KEY` secret
  registered with aur.archlinux.org.

## session

leroymerlin.es is behind bot protection, which answers scripts with a challenge page. the cli never solves challenges. it replays the session your own browser already earned, with your cookies, your browser's user agent and a chrome-like tls handshake, so the site sees the same browser it already trusts. no account and no password are involved.

the easy path is `auth harvest`: it copies the cookie store of your real chrome profile into a temporary directory, opens chrome on that copy, waits while you clear any check by hand, and reads the cookies and user agent straight out of it. chrome 136+ ignores `--remote-debugging-port` on the default profile, so the copy is what makes the DevTools port work; the trusted `datadome` cookie comes with it.

```bash
leroymerlin auth harvest
leroymerlin auth status
```

the copy holds only `Local State`, `Default/Cookies` (or `Default/Network/Cookies`) and `Default/Preferences`, is `0700`, and is deleted along with the browser on every exit path, errors included. it auto-detects `~/.config/google-chrome` then `~/.config/chromium`; `--profile` (env `LEROYMERLIN_CHROME_PROFILE`) picks another chrome profile directory. that `--profile` is the chrome profile, not the session profile: use `LEROYMERLIN_PROFILE` or `default_profile` for the latter.

it attaches to a chrome already started with `--remote-debugging-port` when one is listening (`--browser-url`, `LEROYMERLIN_CHROME_URL`, or `http://127.0.0.1:9222`). with no real profile it launches a fresh one under `~/.local/share/leroymerlin-cli/chrome`, which may need a hand-cleared captcha. pick your store in the window before it finishes: stock follows it.

the manual path still works, for scripts, ci and machines without a display. open leroymerlin.es in chrome, pick your store, then export:

- **HAR**: devtools → network. in devtools settings → preferences → network, enable "allow to generate HAR with sensitive data", reload, then "export HAR (with sensitive data)". a HAR also records your user agent
- **cookies**: a cookie-export extension, saved as json or cookies.txt

```bash
leroymerlin auth login --har ~/Downloads/leroymerlin.har
leroymerlin auth login --cookies ~/Downloads/leroymerlin-cookies.json
leroymerlin auth status
```

only leroymerlin.es cookies are kept, so a whole-browser export is fine. the session is stored `0600` under `~/.config/leroymerlin-cli/sessions/`, and cookies the site rotates are saved back. when the site stops trusting the session, commands exit `5`: reload the site in the same browser and import again. `doctor` says which case you are in.

for ci, point `LEROYMERLIN_COOKIEJAR` at an export. it is read on every run and never written to disk:

```bash
export LEROYMERLIN_COOKIEJAR=~/secrets/leroymerlin-cookies.json
leroymerlin doctor
```

stock follows the store in the session, so use one profile per store to compare:

```bash
leroymerlin auth login --har madrid.har --profile madrid
leroymerlin auth login --har getafe.har --profile getafe
leroymerlin profile use madrid
```

if `product stock` or `store current` says the session has no store, the export was taken before choosing one. open leroymerlin.es, pick a store ("Elegir mi tienda"), and export again; `store current` shows the one it found. keep the browser's user agent with the cookies: an export without it is replayed as the default chrome and may be challenged, so pass `--user-agent` when needed.

## updates

```bash
leroymerlin update --check
leroymerlin update --yes
```

interactive commands check for a new stable release at most once a day and ask before installing. scripts, mcp and `--no-input` never check. `LEROYMERLIN_NO_UPDATE_CHECK=1` turns the check off. archives are verified against `checksums.txt` before the binary is replaced. `go install` builds update with `go install`.

## command surface

| command | what it does |
|---|---|
| `search` | search the catalogue, or browse a category, with the site's filters and sort |
| `product show / stock` | one product, its price and where it is available (`item` works too) |
| `watch add / list / check / events` | saved searches and products, reported as change events |
| `sink list / test` | copy watch events to ntfy, a webhook or a script |
| `auth harvest` | open a real Chrome, read the session out of it (no export file) |
| `auth login / status / logout` | import an export, inspect or forget the browser session (`login` is a shortcut) |
| `profile list / use / remove` | one session per store, side by side |
| `store list / current` | every store from the public sitemap, and the one the session selected |
| `category list` | the category tree, from the public sitemap |
| `doctor` | config, session, sitemaps, and whether the storefront accepts the session |
| `config path / list / get / set` | config.toml |
| `mcp`, `mcp install claude-code\|codex\|cursor` | the read commands as mcp tools |
| `skills list / get` | embedded docs for agents |
| `update` | self-update from github releases |
| `completion bash\|zsh\|fish\|powershell` | shell completion |

global flags: `--profile`, `--format json|jsonl|pretty`, `--no-color`, `--error-format text|json`, `--no-input`, `--debug`.

## examples

```bash
leroymerlin store list --compact --format pretty
leroymerlin store list --format jsonl | jq -r .slug
leroymerlin category list --parent construccion --depth 1 --format pretty
leroymerlin category list --query cemento
leroymerlin search "cemento cola" --format pretty
leroymerlin search "mortero" --brand SIKA --max-price 30 --sort price-asc
leroymerlin search --category construccion/cementos-morteros-y-yesos/cementos --in-stock
leroymerlin product stock 81866477 --format pretty
leroymerlin watch add search "cemento cola" --max-price 20 --name cola
leroymerlin watch check --all --format jsonl | jq -r 'select(.type=="product.price_changed") | .product.url'
leroymerlin doctor --error-format json
leroymerlin mcp install claude-code
```

## exit codes

`0` ok, `1` generic, `2` usage, `3` no session, `4` not found, `5` challenged by the bot protection or rate limited, `6` network, `7` the site changed (stderr carries a prefilled issue link), `130` interrupted.

## manners

requests go one at a time, at least 1.5 seconds apart, and that spacing carries over between runs. there is no bulk mode and there will not be one. the site's robots.txt asks crawlers to stay away from search and stock pages. this is a tool for one person shopping, so keep scripts and agent loops small.

## documentation

- [docs/cli-spec.md](docs/cli-spec.md): the interface contract
- [docs/leroymerlin-api-discovery.md](docs/leroymerlin-api-discovery.md): what the site does, endpoint by endpoint
- [docs/troubleshooting.md](docs/troubleshooting.md): common failures and the fix
- [examples/](examples): ready-to-run recipes and a sample config.toml
- [scripts/install.sh](scripts/install.sh): the curl|sh installer
- [npm/](npm): the npx wrapper (not published yet)
- [docs/adr](docs/adr): decisions (session replay, tls fingerprint, read-only)
- [AGENTS.md](AGENTS.md): vocabulary and rules for contributors and agents

## contributing

issues and pull requests are welcome. when the site changes, the cli prints a prefilled issue link, and that is the most useful report you can file.

## disclaimer

this is an unofficial client. it is not affiliated with, endorsed by or sponsored by Leroy Merlin, and "Leroy Merlin" is used only to say which website it works with. it reads public pages with your own browser session for interoperability, at a human pace, and never buys anything. the site's terms may restrict automated access, and its robots.txt asks crawlers to stay off search and stock pages. use it responsibly, for your own shopping, and at your own risk.

## license

[mit license](LICENSE)
