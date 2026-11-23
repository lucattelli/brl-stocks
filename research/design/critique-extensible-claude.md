# Critique: proposal-extensible

## 1. Verdict

I would build on the pure `Plan`/`render` core and the lock-free, rename-written cache file, but not on the transaction around them.
The one thing that must change: the fetch must not run inside the cache lock. That choice causes three of the six serious findings below.
About a third of the structure (the `Step` event/effect layer, `Router`, `Store`, `Base`/`Merge`) serves features the log excludes and should be cut. No finding is fatal.

## 2. Fact check

| Claim | Verdict | Source |
|---|---|---|
| `tea.Tick` sends one message and must be re-armed | Right | https://pkg.go.dev/github.com/charmbracelet/bubbletea#Tick |
| Bubble Tea runs each command in its own goroutine | Not confirmed from the docs, which only say commands "run asynchronously" | same page |
| The design targets a known Bubble Tea version | Unaddressed. The cited page is v1.3.10 and flags a v2 major, where `View()` returns `tea.View` and key presses are `KeyPressMsg`. D27 left the version open | https://pkg.go.dev/github.com/charmbracelet/bubbletea/v2 |
| `Round(0)` strips the monotonic reading; the monotonic clock may stop in sleep | Right | https://pkg.go.dev/time#hdr-Monotonic_Clocks |
| The pending tick fires within 5 s of waking | Right on Linux: Go timers use `CLOCK_MONOTONIC`, which pauses in suspend | https://github.com/golang/go/issues/24595 |
| The kernel releases `flock` when the process dies | Right (released when all descriptors are closed) | https://man7.org/linux/man-pages/man2/flock.2.html |
| `Update` takes the lock "waiting until ctx ends" | Wrong as written: `syscall.Flock(fd, how)` takes no context and blocks in the kernel. Only a `LOCK_NB` polling loop gives this | https://pkg.go.dev/syscall?GOOS=linux, flock(2) |
| (implied) one process holds the lock once | Wrong: two `open` calls in one process get independent locks and block each other | flock(2) |
| `flock` unreliable on network home directories | Right: NFS emulates it with byte-range locks | flock(2) |
| `map[Instrument]Entry` round-trips as `"B3:PETR4"` keys | Right only if `Instrument` implements `TextMarshaler`/`TextUnmarshaler`; the design does not say so | https://pkg.go.dev/encoding/json |
| `^GSPC`, `BRL=X` mappings [unverified] | Right: both are in the project's own report (`market-data-sources-2026-11.md` lines 21, 41) | local |
| Yahoo sends `Retry-After` on 429 | Could not be confirmed; `max(…, 60 s)` makes it harmless | none |
| Session fields are correct at the open, on holidays and for FX | Could not be confirmed (F14) | none |

## 3. Findings

**S1. Serious. A hung source turns the lock into an outage for every process.** `Timeout` is not source-wide, so nothing cancels the batch. Yahoo accepts connections and never answers; with the spec's 10 s timeout (the design names none) the batch is 23 instruments / 5 workers = 5 rounds = 50 s, lock held throughout. At t=30 s `Step` declares the fetch lost and emits a second `Refresh`, which opens the lock file again and blocks against its own process. At t=45 s it returns `ErrBusy`, so the view reports a busy cache that it caused. Meanwhile every status-bar call blocks for its 3 s `LockWait` on a 2 s schedule, so calls stack. The cycle repeats each retry, and the source-wide back-off never engages. Fix: fetch outside the lock; use a non-blocking try-lock only as a "someone is fetching" marker, and print at once when it is taken.

**S2. Serious. A cache error becomes either no data or a request storm.** The fetch runs inside `Store.Update`. (a) First run, or `XDG_CACHE_HOME` pointing at a missing or read-only folder: the lock file cannot be opened, `Update` fails before fetching, and the screen stays empty with a working network. (b) Disk full: the lock and the fetch succeed, the temp-file write fails, `Update` returns an error. The design does not say the fetched snapshot is still returned. If it is not, `Step`'s memory still shows everything due and emits `Refresh` on every 5 s tick: 23 requests per tick, about 276 a minute, with no back-off because back-off covers only source failures. Fix: return fetched data regardless of the write result, and back off on cache errors.

**S3. Serious. The closing price is never fetched for B3 (violates D15, spec 7.2 "shows the last close").** `NextDue` allows one fetch at the session end. B3 quotes lag about 15 minutes (F15), so the fetch at 17:00:03 returns the 16:45 price; the next fetch is at tomorrow's open. All evening and all weekend the row shows a pre-close price and day change, labelled closed. B3's closing auction makes the difference real. Fix: keep the entry due until `Quote.Time` reaches the session end or 30 minutes have passed.

**S4. Serious. `r` does nothing when it is most wanted (changes D32, spec 7.1).** `Force` respects a source's wait, and `Network` is source-wide. The laptop reconnects after five failed probes, the user presses `r`, and nothing happens for up to 60 s, with no notice specified. Fix: `r` overrides a `Network` wait, not `RateLimited` or `Blocked`.

**S5. Serious. The obvious implementation of the lock wait leaks the lock.** Wrapping a blocking `Flock` in a goroutine and abandoning it at `ctx` end leaves a waiter that later acquires the lock with nobody to release it. In the long-lived view, every later `Update` in every process then returns `ErrBusy` until restart. Fix: specify `LOCK_NB` polling.

**S6. Serious if F14 fails. A probe at the boundary can sleep a market for a day.** After a close, the next attempt is exactly at the next start time. If the source still reports the previous session at 10:00:03 (or the local clock runs a few seconds fast), the rule "otherwise → next start" fires again and B3 is shown closed all Monday. The designer names this weak point but not the trigger. Fix: re-probe on a short back-off while the returned session is still in the past.

**M1. Minor.** The status bar polls every 2 s and the view every 5 s, so the bar usually wins the lock and becomes the fetcher: its call blocks 1 to 3 s each minute. When its wait runs out the output is "marked stale", but spec 9.2 defines `stale` as after a failed fetch.

**M2. Minor.** Walk-through (v) is too kind. With markets closed the view plans nothing, so it never rewrites the file; the next one-shot refetches all 23.

**M3. Minor.** A view left open for days with schema 1 beside a newly installed binary with schema 2: each reads the other's file as empty and overwrites it. Load doubles and shared back-off is lost each minute.

**M4. Minor.** A mistyped ticker is retried forever, including while markets are closed. The class of in-flight requests cancelled by a 429 is unspecified. USD/BRL probably reports a 24-hour session, so the view may never go quiet [unverified].

**M5. Minor.** `Quote` uses bare `float64`. A missing low or a zero previous close yields 0 or NaN, and `json.Marshal` rejects NaN, which breaks "always valid JSON" and the cache write.

**M6. Minor.** `NextDue(e, interval)` has no `now`, yet its rules depend on it. Crashed writers leave `.tmp` files that nothing removes.

**E1. Excess. `monitor.Step` with `Event`/`Effect`.** Bubble Tea's `Update` is already a function from model and message to model and command, testable without a terminal. The layer defines every event and effect twice plus a translation. One `r` press crosses `tea.KeyMsg` → `tui.Update` → `monitor.Key` → `Step` → `Effect` → `tea.Cmd` → `Deps.Refresh` → `Service` → `Store.Update(fn)` → `Plan` → `Router` → `yahoo`: eleven hops, seven packages. Its stated later uses (alerts, detail view, editing) are non-goals under D12 and D13. Deleting it loses effects-as-data assertions.

**E2. Excess. Second-provider machinery:** `Router`, `Supports`, the factory map, `Settings{Getenv}`, the optional history interface, `Auth`, `SourceOf`, health keyed by source name. D7 asks for the interface only. `Router` is itself a `Provider`, which proves it can be added later without touching callers. Nothing is lost.

**E3. Excess. `cache.Store` interface.** One implementation; the design already tests `File` on `t.TempDir()`.

**E4. Excess. `Base` and `Merge`.** They make memory a second source of truth to save 23 requests after a truncation, and cause the double `Plan` of weak point 3.

**E5. Excess.** Seven-day pruning for 23 records, `fsync` for a cache that tolerates being empty, `Market: Global` and `Kind`, `LockWait`/`ErrBusy` once S1 is fixed.

**Missing on day one:** creating the cache folder; the per-request timeout and User-Agent (the endpoint has rejected non-browser values); the rule mapping an entry to `ok`/`closed`/`stale`/`error`; colour and pipe detection (D25) given a pure `render`; the no-config fallback (D35) and a config deleted while running; time zone data for `LoadLocation`; the Bubble Tea major version.

## 4. What holds up

- **Lock-free readers with temp file and rename.** I found no interleaving where a reader sees partial data.
- **Per-instrument freshness.** One `Plan` rule delivers D14, D15 and D23 for mixed open and closed markets, and makes walk-through (ii) cost no requests while entries are fresh.
- **Wall-clock heartbeat.** Walk-through (iii) is correct on Linux; a timer aimed at the next fetch would not be.
- **Back-off state in the shared file.** Walk-through (i) checks out: at most 5 requests, then probes at 60, 180 and 480 s, and one-shots send nothing.
- **Config by `stat` through the symlink.** It never writes the config folder (D29, D35), and a failed parse keeps the old config.
- **Failures as data.** This makes exit code 0 and valid output natural, apart from M5.
- **No dependencies** beyond the allowed four.
