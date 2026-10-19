# Code-quality review: `feat/initial`

Date: 2026-10-19. Fixed point: `main` (`a68a12`, the empty scaffold). `git diff main...feat/initial` is 138 files, 31 commits, ~74k inserted lines; roughly 9.1k are Go source and the rest are captured HTML/XML fixtures and the docs-site lockfile. No Go source file reaches 1000 lines; the only files over the threshold are fixtures and `docs-site/package-lock.json`, which are data, not code.

Rubrics: the Cursor thermo-nuclear prompt (structure, simplification, boundaries) and the Pocock two-axis review (documented standards + Fowler smells; the spec). This document records findings first; the status after the fixes is at the end.

## 1. Thermo-nuclear review

### T1. `internal/cli/watch.go` is three files in one trench coat — high

582 lines holding the watch noun (add/list/remove/check/run/events), the whole `sink` noun, and three presentation types (`watchView`, `watchList`, `eventList`). It changes for unrelated reasons: a new watch filter, a new sink type, and a new pretty column all edit the same file. That is Divergent Change and the closest thing this branch has to spaghetti.

Fix: split presentation (`watch_view.go`) and sinks (`sink.go`) out of the command orchestration, then remove the mode boolean in `watchCheckCmd(run bool)` (`internal/cli/watch.go:414`) by giving `check` and `run` their own constructors over a shared selection/check helper.

### T2. Dead state: `Seen.Snapshot` is written and read, never used — high

`internal/store/store.go:55` adds `Snapshot`, the schema stores a `snapshot` column (`:97`), `Seen` reads it (`:209-221`), `CommitCheck` writes it (`:237-243`), and `internal/watch/watch.go:115,126` marshals the full product into it. Nothing ever consumes it. It is a per-product JSON copy kept forever for no reader.

Fix: delete the field, the writes, the read and the marshal work. Old databases keep the harmless defaulted column; new schema drops it.

### T3. The event product payload is defined three times — medium

`watch.productEvent` (`internal/watch/watch.go:30-44`), an anonymous struct in `cli.eventList.Pretty` (`internal/cli/watch.go:211-219`) and `sink.eventProduct` + `price()` (`internal/sink/sink.go:31-37,143-183`) all describe the same JSON: ref, name, URL, price, stock. Add a field and three files can drift.

Fix: one `store.ProductPayload` (with `store.Price`) owns the shape. `watch` builds it, `sink` and the CLI parse it. This removes the anonymous structs and the duplicated price formatting.

### T4. Bespoke helpers where the standard library already has the answer — medium

`store.joinAnd` (`internal/store/store.go:310`) reimplements `strings.Join(parts, " AND ")`; `store.contains` (`:342`) reimplements `strings.Contains`; `store.Events` formats `LIMIT %d` into SQL (`:284`) instead of binding the parameter. The same pattern appears in `cli/harvest.go:145` (`firstNonEmpty`) where `cmp.Or` is the canonical expression.

Fix: delete the helpers, use `strings.Join`, `strings.Contains`, a `?` placeholder, and `cmp.Or`.

### T5. Harvest duplicates its browser launch options — medium

`runCopied` and `runFresh` (`internal/harvest/harvest.go:137-190`) repeat the same `DefaultExecAllocatorOptions + VisibleWindow + ExecPath + UserDataDir` block, differing only in the websocket/port flags. Two copies of launch policy is exactly how the copied and fresh paths drift apart.

Fix: one `launchOptions(path, dir)` builder; the copied path appends its remote dialer and port flags.

### T6. `page.htmlAttr` compiles a regexp per call behind an identity alias — medium

`var attrRe = regexp.MustCompile` (`internal/leroymerlin/page.go:69`) is a function alias, and `htmlAttr` (`:84`) builds and compiles a new pattern for each attribute, several times per page. This is indirection plus avoidable work.

Fix: scan the tag attributes directly with `bytes.Index`, no alias and no per-call compilation.

### T7. Decomposition is otherwise sound — informational

`internal/leroymerlin/session.go` (453), `client.go` (265), `product.go` (263), `harvest` (373 + 225), `store` (349) are large but single-purpose and readable. `internal/cli/search.go`, `product.go`, `catalog.go` keep one noun each. No file-size waiver is needed outside T1.

### T8. Atomicity/orchestration — low

`watchCheckCmd` opens the state database to select watches, closes it, then `checkWatches` opens it again (`internal/cli/watch.go:430-439,389-394`). Not a data bug, but the flow hands state across a reopen it does not need. Pass the open store through.

Per-watch commits in `checkWatches` are intentional: one watch failing must not discard another's check. No change.

## 2. Pocock — Standards

Documented standards: `AGENTS.md` (layering, fixtures, pacing, no other-project credits), `README.md`, `docs/cli-spec.md`, `docs/adr/*`. No `CODING_STANDARDS.md` or `CONTRIBUTING.md` exists. `gofmt`, `go vet` and staticcheck are clean, so tooling-enforced issues are out of scope.

### S1. Hard: argument errors bypass the usage exit code — high

Only flag-parse errors go through `output.Usagef` (via `SetFlagErrorFunc`). Every `Args: cobra.ExactArgs/NoArgs/MaximumNArgs` error is a plain error, so `leroymerlin product show` or `leroymerlin auth harvest extra` exits `1`, while `docs/cli-spec.md` §7 promises `2 | usage: bad flag, argument, ...`. There are 30 such sites under `internal/cli/`.

Fix: positional-argument helpers that wrap cobra's checks in `output.Usagef`, used everywhere; one regression test.

### S2. Duplicated watch-add validation — medium

The same `validWatchName` block is in `watchAddSearchCmd` (`internal/cli/watch.go:265`), `watchAddProductCmd` (`:290`) and again in `addWatch` (`:97`), with the earlier copies existing only to return the right error before a session/network call.

Fix: one `prepareWatchAdd` that validates name, interval and sinks; `addWatch` receives the validated value. One place to change, same ordering.

### S3. `watchCheckCmd(run bool)` is a mode flag on a command constructor — medium

`internal/cli/watch.go:414-471` changes the `Use`, `Short` and control flow on a boolean, and `watchRunCmd` is a one-line wrapper. One-off booleans that fork a function are the exact pattern the rubric calls out.

Fix: two constructors, each with its own `RunE`, sharing `selectWatches`/`checkWatches`; delete the wrapper.

### S4. `selectWatches` masks database errors as "no watch" — medium

`internal/cli/watch.go:368-372` turns every `WatchByName` error into `no watch named %q`. A corrupt or locked database is reported as a user typo.

Fix: `store.ErrNotFound` sentinel; only that becomes the usage/not-found message, other errors surface.

### S5. `mcp.install` can silently overwrite a malformed entry — low

`internal/mcp/install.go:82-85`: `entry, _ := servers[ServerName].(map[string]any)`. A string/array entry becomes `nil`, then `action` becomes `"added"` and the entry is replaced. The TOML path carefully refuses a form it does not understand; the JSON path should too.

Fix: error when the key exists but is not an object.

### S6. Small consistency issues — low

- `cardProduct.Rating` (`internal/leroymerlin/search.go:151`) has no `json` tag while its neighbours do.
- `checkSearch` names its query variable `pages` (`internal/watch/watch.go:87`).

Both were taken in this pass. (A suspected sitemap title-case panic was checked and dismissed: `strings.Fields` never yields an empty token.)

### Smells that are judgement calls, deferred

- **Primitive Obsession:** `SearchQuery.MinPrice/MaxPrice` are strings to preserve the user's decimal spelling. A `Price` type would add conversion noise without removing a real defect; deferred.
- **Repeated Switches:** `describeTarget`, `eventPayload` and `deliveryLine` switch on a small kind. Each has a single call site and a different output shape; a dispatcher would be more machinery, not less. Deferred.
- **Refused Bequest:** none; there is no inheritance.
- **Middle Man:** the `productView`/`storeRefView`/`watchView` wrappers exist only to hang `Pretty` on a type. That is the established rendering convention in this repo (see `storeList`, `categoryList`), so it is suppressed by the repo standard.
- **Speculative Generality:** the `first_seen`/`last_seen` columns on `seen` are written but never read. They are kept because existing state databases declare them `NOT NULL` without a default; removing them needs a migration, not a code deletion. Deferred with that reason. (`Watch.Baselined` was dead and was removed in this pass.)
- **Message Chains:** none of consequence; `Client.Search` hides the fetch+parse walk.

## 3. Pocock — Spec

Spec: `docs/cli-spec.md`, with endpoint facts in `docs/leroymerlin-api-discovery.md`. ADRs are standards as well as design records.

### P1. Hard: exit code for bad arguments — high

> `| 2 | usage: bad flag, argument, profile name or config key |`

See S1: positional-argument errors exit 1. **Fix (same change as S1).**

### P2. `watch add search` does not honour two of the flags it claims — high

> `**watch add search [keywords...] [--name N] ...** saves a search with the same flags as `search`.`

`internal/cli/search.go:56` ignores `f.inStock` when building the query, and `:118-129` filters only inside the `search` command; `f.limit` is likewise only applied there. `watch add search --in-stock --limit 5` stores neither, so checks silently watch the unfiltered, unlimited result. `--page` was also accepted and then ignored by the check loop (which always starts at page 1). This is a requirement that looks implemented but is wrong.

Fix: move `InStock` and `Limit` into `leroymerlin.SearchQuery`, apply them after parsing in `Client.Search`, and delete the CLI-only filter. Both `search` and watch checks then share one implementation. Reject `--page` on `watch add`, where it has no meaning; `--pages` is the watch flag. Add client and watch tests.

### P3. `watch run` uses one watch's interval for all — medium

> `**watch run [NAME] [--all]` repeats until interrupted.` and `The interval defaults to `watch.interval` from config, then 5m.`

`internal/cli/watch.go:458` sleeps `ws[0].Interval`, so with several watches the loop cadence is whichever sorts first, not each watch's own interval. Fix: use the minimum interval across the selected watches (the loop cannot be more frequent than the fastest watch), and keep the sleep cancellable. Add a unit test for the helper.

### P4. `doctor` does six checks, the spec says five — medium

> `doctor` runs five checks: config, session, datadome cookie, sitemaps, then one request to `/` with the session.`

`internal/cli/misc.go:165-200` adds a `user agent` check. The behaviour is good and the output documents it; the spec is stale. **Fix the spec**, not the code.

### P5. `auth status --check` pretty output omits the datadome expiry — low

> `prints source ... whether `datadome` is present and its expiry ...`

`internal/cli/auth.go:203-208` prints `datadome true`, while the JSON `session.datadome_expires` carries the date. Fix: print the date in pretty when known.

### P6. Documented env/state list is incomplete — low

> `Env: LEROYMERLIN_PROFILE, LEROYMERLIN_CONFIG, LEROYMERLIN_COOKIEJAR, LEROYMERLIN_ERROR_FORMAT, LEROYMERLIN_BASE_URL, LEROYMERLIN_NO_UPDATE_CHECK, NO_COLOR.`

The harvest reads `LEROYMERLIN_CHROME_URL`, `LEROYMERLIN_CHROME` and `LEROYMERLIN_CHROME_PROFILE` (`internal/cli/harvest.go:19-23`). Fix the spec (and the layout list in §13, which omits `internal/harvest`).

### P7. Price and limit validation is a shape check, not a validation — low

> `--min-price N`, `--max-price N`, `--limit N`

`internal/cli/search.go:74-82` rejects a leading `-` and multiple dots but accepts `abc`, `1e999` and `--limit -3` (which silently means no limit). These are "looks implemented but wrong" validations. Fix with `strconv.ParseFloat` and non-negative checks; tests for both.

### P8. Spec and API-discovery are otherwise faithful — verified

- search filters/sort/pagination, the embedded-slice limitation, product/stock shapes, store-selected pickup rows, harvest profile copying, watch events and sinks all match the documents.
- `internal/leroymerlin/client.go` headers/client hints match §9; the rate gate and cookie rotation match §9/§10.
- No undocumented feature was found: `store current`, the MCP tools and harvest's `source`/`chrome_profile` are all in the spec.

## Fix status after this branch

- **Fixed:** T1, T2, T3, T4, T5, T6, T8; S1-S6; P1-P7. T1 split into `watch.go` (482 lines), `watch_view.go` (82) and `sink.go` (97); P2 moved `InStock`/`Limit` into `SearchQuery` and applies them in `Client.Search`, so `search` and watched searches share one implementation, and `watch add --page` is rejected instead of ignored; P3 uses the minimum interval among the selected watches; S1 adds `noArgs`/`exactArgs`/`maxArgs`, so bad arguments exit 2.
- **Deferred:** T7 (no action needed); the judgement-call smells in the Standards section (price type, switches, the `seen` timestamps, schema migration/`isUnique`) are left with reasons.
- Files over 1000 lines: none in code; only captured fixtures and `docs-site/package-lock.json`.
- Final suite: 99 test functions, 102 runs including subtests.

## Finding counts

| rubric | high | medium | low | informational |
|---|---:|---:|---:|---:|
| thermo-nuclear | 2 | 4 | 1 | 1 |
| standards | 1 | 3 | 3 | — |
| spec | 2 | 2 | 3 | — |
