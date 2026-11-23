# Critique: proposal-robust.md

## 1. Verdict

I would build on its skeleton (pure `MakePlan`/`Classify`, one `Cycle`, `flock` plus rename), but not on its rules as written.
The one thing that must change: the scheduling and classification rules contradict each other, so that every row reads `stale` after every close (S1) and a ticker without a quote is refetched every 5 seconds forever (S2).
About a third of the mechanisms (wake detection, majority-promotion to `Schema`, multi-config merge with pruning, `Router`) cost more than they protect for 20 tickers.

## 2. Fact check

| Claim | Verdict | Source |
|---|---|---|
| Bubble Tea runs each `tea.Cmd` in its own goroutine and returns the result as a message | Right. No concurrency limit. On quit it neither waits for nor cancels running commands and drops their results, which the design does not mention | https://raw.githubusercontent.com/charmbracelet/bubbletea/v1.3.10/tea.go |
| `tea.Tick` must be re-armed on each beat | Right: "sends a single message" | https://pkg.go.dev/github.com/charmbracelet/bubbletea#Tick |
| `View` runs often enough for ages to advance | Right, but only because of the heartbeat: `View` runs after each `Update`, not on its own | same `tea.go` |
| Go timers use the monotonic clock, which some systems stop during sleep | Right. The conclusion drawn ("`tea.Tick(60s)` breaks across sleep") is overstated: the timer fires at most 60 s after wake | https://pkg.go.dev/time#hdr-Monotonic_Clocks, https://github.com/golang/go/issues/24595 |
| `Round(0)` makes comparisons use wall time | Right, but it is needed on every in-process `time.Now()` value, not only "deadlines". Two times that both carry a monotonic reading are compared by it "alone, ignoring the wall clock". Times read back from JSON carry none | https://pkg.go.dev/time#hdr-Monotonic_Clocks |
| "Clock changes cannot desynchronise it" (Summary 2) | Wrong. Every persisted deadline is wall-clock, so a clock step moves all of them (M1) | follows from the design itself |
| `flock` is released by the kernel when the process dies | Right in effect: released when all descriptors of the open file description close. Two `open()` calls conflict even inside one process, which the design needs | https://man7.org/linux/man-pages/man2/flock.2.html |
| `flock` is unreliable on network home directories | Partly: emulated with `fcntl` locks on NFS since Linux 2.6.12 | same |
| Bubble Tea import path and API version | Not addressed. pkg.go.dev shows v1.3.10 with "the highest tagged major version is v2"; the design does not say which it targets (D27 backlog) | https://pkg.go.dev/github.com/charmbracelet/bubbletea |
| Yahoo `Retry-After`, unknown-symbol response, holiday trading period | Could not be confirmed; marked [unverified] by the designer too | — |
| Blocks last minutes (back-off caps at 30 min) | Incomplete: the project's own research records IP blocks of more than 24 hours. The cap gives 48 probes a day, which is acceptable | `research/market-data-sources-2026-11.md` line 75 |

## 3. Findings

**S1. Serious — in normal use every row is `stale` after the close, never `closed`.** `MakePlan` stops fetching 20 minutes after session end; `Classify` gives `Closed` only to a quote fetched *after* session end + 20 minutes. The two windows do not overlap. View open all day, B3 closes at 17:00: the last fetch happens at about 17:19:30, the next would be due at 17:20:30 and is not made. From 17:20 until the next open, every B3 row says `stale` and `print --json` reports `"state":"stale"`. Variant: laptop closed at 16:30, opened at 20:00; the quote is from 16:30, nothing is due until tomorrow, and the row shows a pre-close price as the last value all evening. Violates D15 and spec 7.2. Fix: make a closed-market ticker due whenever its quote was fetched before session end + settle time.

**S2. Serious — a ticker with no quote is refetched every 5 seconds, forever.** "Due = no quote" has no per-ticker back-off; `Entry.LastAttempt` exists but `MakePlan` does not use it, and back-off is driven only by source-scope errors. The user types `PETR44` in the config. Yahoo answers not-found, a ticker-scope error, so `Health` stays `Healthy`. The view requests it every 5 s, markets open or closed: about 17,000 requests a day. With no view, a status bar calling `print` every 2 s does the same through the lock. This is the load most likely to earn the block the design exists to avoid, and the block then silences the other 22 rows. Violates D19 ("increasing waits"). Fix: per-ticker back-off from `Entry.LastAttempt` and `LastErr`.

**S3. Serious — batches of one meet the "more than half" rule.** Probes fetch one ticker, and a newly added ticker is fetched "alone". One undecodable response is then 100 % of the batch and is promoted to `Schema`. Markets open, user adds a thinly traded FII whose response has null series: the whole watchlist stops refreshing for 5 minutes, then 30, with "schema" on the status line, and the state is persisted for every process. Fix: drop the promotion, or require at least 3 failures.

**S4. Serious — "one request per holiday" has nowhere to live.** `MakePlan` is pure over `State`, and `State` has no field for "next check moved". On Thursday 2026-11-26 (US Thanksgiving) at 09:30 New York time, US tickers pass the predicted open and become due. If Yahoo returns Wednesday's period, the prediction computed from it is still in the past: the tickers are due every interval all day and shown `stale` with no error to explain it. Fix: persist a per-instrument `NextCheck`.

**S5. Serious — the 30-second rule for `r` contradicts `MakePlan`, and force is undefined.** The walk-through says `r` probes during a block after 30 s; the signature comment says nothing runs before `NotBefore` or `LastAttempt+5s`. It is not stated whether `force` beats `NotBefore`, and whether `print --refresh` gets the same treatment. If it does, a status bar using `--refresh` probes a blocked source every 30 s. If it does not, D32's "refresh now" does nothing for up to 30 minutes.

**M1. Minor — wall-clock steps poison persisted state.** The clock is a day ahead at boot, one cycle runs, then NTP corrects it. `Health.LastAttempt` is in the future, so `MakePlan` returns nothing for a day. Merge keeps "newest `FetchedAt`", so the future-dated entries beat every fresh fetch and stay `stale`. Fetched data should simply win; future stamps should be discarded on load.

**M2. Minor — monotonic trap.** `beatMsg` and `lastBeat` come from a timer and carry monotonic readings. Subtracting them on Linux after 14 hours of sleep gives about 1 second, so wake is never detected. The same applies to `FetchedAt` held in memory: age reads near zero and a 14-hour-old row stays `ok`. The design is correct only if `Engine.Now` is the single place that strips the reading; it should say so.

**M3. Minor — empty config flicker.** Editors write through a symlink in place (truncate, then write). A zero-byte file is valid TOML with no tickers, so the watchlist vanishes for a beat.

**E1. Excess — wake detection.** Wall-clock planning already handles wake, and the 5-second network back-off covers the missing network. "Set `NotBefore` to now + 3 s" can also shorten a persisted block. Delete it; nothing is lost.

**E2. Excess — merge across different `--config` files and 7-day pruning.** No decision asks for two configs. Re-reading under the lock and overlaying fetched entries is enough.

**E3. Excess — `provider.Router`, `Auth`, `Unsupported`, HTML-means-`Blocked`.** Version 1 ships one provider (spec 6.1). Keep the interface; build the router when a second provider exists. The HTML heuristic turns a captive portal into a shared 2 to 30 minute block.

**E4. Excess — the 20-minute settle window for US tickers**, whose lag is 1 second (F15): 20 extra cycles a day. Use one fetch after close.

**Missing.** A User-Agent setting, although the project's research records the chart endpoint rejecting non-browser values. Cleanup of temp files left by a killed `print`. Cancelling the context when `q` is pressed during a cycle. The built-in watchlist and notice when there is no config (D35). Which currency `Validate` expects for `IDX` (`^BVSP` is BRL, `^GSPC` is USD). How a failed attempt merges with an existing quote. Whether the view's own cache write triggers its own reload.

## 4. What holds up

- **`flock` plus re-read under the lock.** I tried overlapping `print` calls, view plus `print`, and a crash while holding the lock. At most one process fetches, and no stale lock survives. The man page confirms the semantics.
- **Temp file, `fsync`, rename, with lock-free readers.** No interleaving produces a partial read. Walk-through (v) is correct, and keeping `source.v1.json` separate does preserve a block.
- **Persisted source health.** Walk-through (i) is right: about 6 requests in ten minutes, and `print` makes none.
- **State recomputed at render.** No path lets an old quote keep `ok`, given M2's discipline. It errs toward `stale`, which is the safe direction but is also the cause of S1.
- **Stat-per-second config reload.** It follows the symlink, never writes (D29), works while markets are closed, and an invalid file keeps the old list.
- **One message per cycle and no I/O in `Update`.** This matches how Bubble Tea runs commands.
- **Schema number in the file name.** Old and new binaries cannot corrupt each other.
