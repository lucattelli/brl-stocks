# Critique: proposal-simplest

## 1. Verdict

I would build on this design: nothing in it is fatal, and the lock-free cache mostly survives attack.
The one thing that must change: the view must re-read the cache file on its tick, not only when its in-memory snapshot says something is due (S1).
Close behind: failing entries must obey market hours (S2), and "stale" needs a definition (S3).

## 2. Fact check

| Claim | Verdict | Source |
|---|---|---|
| Bubble Tea runs each command separately; `Batch` is concurrent | Right | https://pkg.go.dev/github.com/charmbracelet/bubbletea#Batch |
| `tea.Tick` sends one message and must be re-armed; the callback receives the time | Right. The callback runs inside the command, so the `stat` there does not block `Update` | https://pkg.go.dev/github.com/charmbracelet/bubbletea#Tick |
| The monotonic clock may stop during sleep; `Round(0)` strips it | Right. Times read back from JSON never carry a monotonic reading either, so cache comparisons are wall-clock regardless | https://pkg.go.dev/time#hdr-Monotonic_Clocks |
| "The pending tick fires within a second" of wake | Plausible, not confirmed. Go timers run on the monotonic clock; whether it pauses in suspend is platform-dependent and still disputed. Either way the tick fires at most about 1 s after wake | https://golang.org/issue/24595 |
| Rename is atomic on one filesystem | Right on Linux. Go's docs add: not atomic on non-Unix platforms, which matters if published (D1) | https://pkg.go.dev/os#Rename |
| `os.CreateTemp` then rename is enough | Incomplete: "it is the caller's responsibility to remove the file", so a process killed before the rename leaves a temp file behind | https://pkg.go.dev/os#CreateTemp |
| A lost update costs "one refetch" | Wrong. The claim stamp survives and the result is lost, so the entry is *not* refetched: the old quote is served for one more interval (S6) | Design §2B, by reasoning |
| Yahoo signals blocks as 429/401/403 | Partly confirmed: 429 waves and IP blocks over 24 h are documented. Several reports say default library User-Agents also get rejected | `market-data-sources-2026-11.md` §2.3; https://github.com/ranaroussi/yfinance/discussions/2431 |
| Import path `github.com/charmbracelet/bubbletea` | Right for v1 (v1.3.10). A v2 exists whose `View` no longer returns a string; its release status could not be confirmed. The design does not say which it targets | https://pkg.go.dev/github.com/charmbracelet/bubbletea/v2 |
| Yahoo field names, session data on weekends and holidays | Could not be confirmed (the design marks them unverified too) | — |

## 3. Findings

**S1. Serious: the view shows prices up to two intervals old whenever another process wins the claim.**
The view decides from its in-memory `snap` and only reads the file inside `Refresh`. Walk-through ii, replayed: entries become due at T. The status bar's call at T+0.2 s claims and starts fetching. The view's tick at T+0.6 s sees "due", calls `Refresh`, which loads the file, sees the claim, fetches nothing and returns the *old* quotes with `AttemptAt = T`. The bar's fetch lands at T+1.5 s. The view now believes nothing is due until T+60 s and never opens the file again. Result: with the status bar running, the open view shows quotes fetched at T−60 s until T+60 s, while the status bar next to it shows newer prices. The bar wins this race roughly one minute in four (1 s tick against a 2 s bar). The walk-through's "the other sees the claim" hides this.
Fix: on each tick when not fetching, load the cache (or `stat` it, as is already done for the config) and merge.

**S2. Serious: failing entries are retried forever, market open or not (violates D15).**
Rule 2 (`Fails > 0`: due) comes before the market-hours rules. Scenario: Friday 16:50, Yahoo starts an IP block (the research records blocks longer than 24 hours). All entries have `Fails > 0`. B3 closes at 17:00, but every 300 s a round of up to five requests goes out, driven by the status bar even with the view closed, until Monday: about 3,900 requests into an active block, exactly the behaviour that prolongs one. Walk-through i says "closed-market entries are never requested"; that holds only for entries that were not failing. Same rule: a typo such as `PETR44` (`not_found`) is requested every five minutes, day and night, forever.
Fix: for an entry that has a quote, apply rules 3 to 5 even when `Fails > 0`.

**S3. Serious: `State` is never defined, and neither obvious definition works.**
`stale` and `ok` are the user-visible contract (D19, spec 9.2), yet the design gives only a signature. If stale means `Fails > 0`, the lagging view in S1 and an entry whose claimer was killed show old prices as `ok`. If stale means "older than one interval", every row flips to stale during each round, and the status bar blinks once a minute.
Fix: define it (for example `Fails > 0`, or market open and `FetchedAt` older than two intervals) and test it against S1.

**S4. Minor: backoff counts from the claim, not from the failure, and the 15 s cap leaves entries in limbo.**
Network up but no route (VPN down, captive portal): each request hangs 10 s, the round ends at 15 s. `AttemptAt + 5 s` and `+ 10 s` are already past, so the first two backoff steps do nothing: three 15 s rounds run back to back, and each blocks a one-shot call for 15 s. In such a round only about 10 of 23 entries are started (five at a time, 10 s each); what is recorded for the 13 never started is unspecified.

**S5. Minor: `refresh_seconds` has no lower bound.** `refresh_seconds = 0` makes `wait(0)` zero: the view sends 23 requests per second and every status-bar call fetches.

**S6. Minor: a lost result makes a price go backwards.** Entry sets differ between processes once markets or failures split the cadence. A saves results; B, which loaded a millisecond earlier, saves its claim over them. The view A has the new price in memory, then its next `Refresh` replaces `snap` from the file with the older quote. One interval, millisecond window.

**S7. Minor: `fetching` must start `true`.** `Init` launches a refresh, but the first tick one second later sees `fetching == false` and launches a second one, breaking the "one Refresh per process" guarantee.

**S8. Minor: a wall clock set backwards freezes entries.** An `AttemptAt` in the future means "not due" until the clock catches up.

**S9. Minor: first run with two processes.** The status bar starts while the view holds the first claim: nothing due, no quotes, so every row prints as `error` with no cause.

**S10. Minor: recovery lag.** The 300 s ceiling means up to five minutes of stale rows after the source recovers; P13 proposed 60 s.

**Excess.**
- The "+20 minutes" in rule 3 is a B3 fact hard-coded in `core` and applied to US and FX. Rule 4 already picks up the close.
- `Unsupported`, the routing-provider sketch and `Error.Status`: no decision needs them yet.
- Seven-day pruning: deleting it costs a few dead records.

**Missing on day one.**
- The User-Agent (spec P10). Reports say default library values are rejected; that would arrive as 429, be classed `rate_limited` and trigger the whole backoff machinery for a configuration bug.
- Creating the cache folder, `XDG_CACHE_HOME`, removing temp files after a failed or interrupted save.
- Yahoo series contain nulls; `[]float64` cannot hold them, so the parser must skip them.
- How the one-shot table shows the error cause.
- Which Bubble Tea major version.

## 4. What holds up

- **The duplicate-claim race.** I could not widen it beyond the few milliseconds between load and save, and its cost really is one duplicated round. A lock would not fix S1 to S3.
- **Rule 3's projection.** It self-corrects after the first fetch of a day: half days, daylight-saving shifts and whatever session Yahoo reports on a weekend all converge, with at most one spurious fetch. Holidays cost polling, which the spec accepts.
- **Wake from sleep (iii).** Wall-clock comparison plus a one-second tick has no timer state to go wrong; a mid-flight request simply times out.
- **Config reload (iv).** `stat` follows the symlink, "mtime differs" (not "newer") survives `git checkout`, a vanished file is ignored, and nothing is ever written to the config folder (D29, D35).
- **Truncated cache (v).** Empty means everything due, and the view keeps its in-memory quotes, so D19 holds.
- **Half-written reads.** On Linux a reader sees the old or the new file; a zero-length file after a crash falls into case v.
