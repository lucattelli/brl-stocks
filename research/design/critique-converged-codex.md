# Critique of the converged design — Codex, round 1

Critic: Codex CLI 0.170.0, model `gpt-6.2-astra`, high reasoning, read-only, 2026-11-23. Reviewed `converged.md` as first written (revision 1), with `critic-brief-converged.md`. The text below is the critic's final message, unedited; the coordinator only added this header. 66,759 tokens.

## Verdict

I would build on the provider boundary and pure rendering code, but I would reject `Cycle` as written.  
The essential change is to distinguish a queued row, a request actually sent, and a result committed to disk.  
Today one timestamp stands for all three. A healthy slow source, a killed writer, and a second process can each expose the difference.

## Fact check

| Claim | Verdict | Source |
|---|---|---|
| Bubble Tea v2 uses `charm.land/bubbletea/v2`. `Tick` delivers one message and needs rearming. | **Right.** v2.1.2 was published November 18, 2026. v1.3.10 dates to September 17, 2025. v2 commands support asynchronous I/O, but filesystem calls made inside `Update` still block it. | [v2 API and release](https://pkg.go.dev/charm.land/bubbletea/v2@v2.1.2), [v1 release](https://pkg.go.dev/github.com/charmbracelet/bubbletea@v1.3.10) |
| `time.Now().Round(0)` makes age calculations include sleep. | **Right**, assuming the wall clock remains correct. It strips the monotonic reading. Linux `CLOCK_MONOTONIC` excludes suspend time. | [Go time](https://pkg.go.dev/time#hdr-Monotonic_Clocks), [Linux clocks](https://man7.org/linux/man-pages/man3/clock_gettime.3.html) |
| Nonblocking `flock` coordinates writers across cache replacement and releases on death. | **Right** on a stable, separate local lock file when the last descriptor closes. A stopped process retains it. The claim that this limits support to Linux and macOS is **wrong**. FreeBSD also exposes `Flock`. | [flock(2)](https://man7.org/linux/man-pages/man2/flock.2.html), [Go FreeBSD implementation](https://go.dev/src/syscall/zsyscall_freebsd_amd64.go) |
| Same-directory rename protects readers. `os.Stat` follows the config symlink. Two equal stamps prove a save finished. | First two **right** on the proposed local Unix filesystem. Last **wrong**. A writer can pause, and replacement files can have identical size and mtime. | [rename(2)](https://man7.org/linux/man-pages/man2/rename.2.html), [stat(2)](https://man7.org/linux/man-pages/man2/stat.2.html) |
| The Go types shown suffice for the package graph and JSON cache. | **Wrong as written.** Go forbids import cycles. A struct map key with only `MarshalText` can be written, but decoding requires `UnmarshalText` or a custom decoder. | [Go specification](https://go.dev/ref/spec#Import_declarations), [JSON decoding](https://pkg.go.dev/encoding/json#Unmarshal) |
| One keyless Yahoo chart call per instrument supplies quote fields, bars, period and zone. | **Supported for the sampled instruments.** F2/F13–15 also measured the delays. Yahoo lists the proposed symbols. Holiday periods, FX hours, regular-only last prices, closing timestamps and a complete refusal mapping remain **[unverified]**. HTTP defines 429, but allows 404 to conceal a forbidden resource. | [Recorded observations](research/grilling-log.md), [Yahoo indices](https://finance.yahoo.com/markets/world-indices/), [FX](https://finance.yahoo.com/quote/BRL%3DX/history/), [PETR4](https://finance.yahoo.com/quote/PETR4.SA/), [BRK-B](https://finance.yahoo.com/quote/BRK-B/), [RFC 6585](https://www.rfc-editor.org/rfc/rfc6585#section-4), [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110#section-15.5.5) |

## Findings

1. **Fatal — Four workers cannot deliver the forced refresh.** Press `r` with 23 eligible entries and a healthy source taking five seconds per answer. Only four requests start within two seconds. The last wave cannot start before second 25, and the view deadline is 15 seconds. Rows the user explicitly refreshed remain untouched. This fails §6.3, S7.1 and QR1 even with the latency used by QR11.

2. **Fatal — The build and cache round-trip both break at package boundaries.** `app.Run` launches `tui`, whose model imports `app.Deps`. Those imports form a cycle. After breaking it, a successful fetch followed by an offline restart still loses every cached row if `Load` uses the declared JSON key type. D48 silently converts the decoding error to an empty cache, defeating S15.1, QR2 and QR3. Move the shared dependency type out of the cycle and specify the inverse key decoder, including `IDX` and `FX`.

3. **Serious — A round publishes attempts it never made.** Kill the holder after it stamps 23 rows and sends four requests. The lock releases, but `print --refresh` two seconds later skips all 23, including 19 never contacted. Kill it after a 429 but before its final write and the next process can miss the block entirely. Conversely, suspend the live holder indefinitely and every contender immediately returns cached prices forever, even after the network recovers. The HTTP deadline cannot release a suspended process’s lock. D38, S13.3, QR6.

4. **Serious — Failure handling invents failures and can hammer a recovering source.** Two connections fail while a third ticker would answer. The engine marks even unattempted rows stale and backs them off, contrary to §6.3. At minute five of a block, let the probe time out or return `NotFound`. Neither outcome clears `Refusals` nor advances `BlockedUntil`. The next beat or forced print probes again, ahead of the hourly not-found check. A mistyped first ticker can monopolize recovery. D39, S6.8, S7.8.

5. **Serious — Losing the cache loses global pacing.** With an unwritable directory and fast answers, 300 ordinary prints can send **6,900 requests** in ten minutes before counting the view. With immediate refusals, eleven forced prints already exceed QR5 in about 20 seconds. Truncating a writable cache during a block likewise lets the next fresh process fetch before the wait expires. The view’s lock-free `loadedMsg` also has no stated merge rule protecting its last good quotes from an empty reload. D48 covers silent cold starts. D49 grants no writable-cache exemption.

6. **Serious — Config identity and cache ownership disagree.** Replace `AAPL` with `MSFT` while the old view is fetching. A new print sees `MSFT`, loses the lock and prints it empty. With different `--config` files, successive successful writers repeatedly prune each other’s quotes. An old in-memory entry can also resurrect a deliberately pruned quote on re-add. Meanwhile, an interval-only edit changes no ticker list, and `PlanInput` lacks the current config hash needed to retry an unknown ticker. A same-size edit preserving mtime can be missed indefinitely. §6.4, S8.4, S11.3–4, S15.6.

7. **Serious — Session evidence is used inconsistently.** After a 14-hour sleep, one US ticker obtains today’s session while another still holds yesterday’s. The header says open, but `Classify(entry, interval, now)` can label yesterday’s successful quote fresh. At close, a final trade stamped 15:59:58 for a 16:00 close triggers 30 minutes of needless polling, including ordinary `print` calls despite S13.5. At the predicted next open on Thanksgiving, even an accurate old session triggers holiday probes. A falsely current session produces an open header all day. D40, D47, §6.1, S5.9, S6.7. [NYSE calendar](https://www.nyse.com/trade/hours-calendars)

8. **Serious — One quote per ticker does not bound cache size.** Start at the opening bell with one five-minute bar. By close each serialized `Series` contains dozens. The cache can grow far beyond 10% without retaining a second quote. S15.3, QR8.

9. **Serious — Day-one I/O contracts are incomplete.** A slow `Stat` inside `Update` delays `q` beyond QR11. Moving reads into commands needs ordering too. A late `loadedMsg` can overwrite a newer `cycleMsg`. A default HTTP client following a redirect to another host breaches QR12. **Minor:** `Getenv` cannot distinguish `NO_COLOR=` from absence for S16.3. [Go HTTP client](https://pkg.go.dev/net/http#Client), [Go environment lookup](https://pkg.go.dev/os#LookupEnv)

## Provisional choices

- **X1 — Conditional.** The try-lock suppresses duplicate healthy rounds cheaply. A short lock around a bounded claim has the stronger case when a holder stalls.
- **X2 — Keep.** One instrument matches Yahoo and makes request accounting testable. Batching earns its place when a second provider actually supports it.
- **X3 — Conditional.** One file makes an ordinary commit atomic. Independent block storage survives quote-cache replacement and pruning.
- **X4 — Keep.** Message-driven `Update` is sufficient. The strongest case for a separate transition function is testing out-of-order completions and config changes during a fetch.
- **X5 — Reject the stated merge.** The pre-fetch and final writes have identical attempt stamps but different results. Disk authority for completed writes avoids that tie. Memory recovery still matters after truncation.
- **Bubble Tea v2 — Keep.** Its command and timer APIs fit. Existing v1 integration would favor v1, but none is identified.
- **New choices — Revise all three.** `MarketState` must inform row classification. The unwritable-cache fallback needs an explicit resolution with D49. Config-change detection needs identity scoped to the config being observed.
- **Excess.** Drop the two-connection cutoff and its speculative row failures. The cost is finishing more requests during an outage. `LastProbe` and `RetryAfter` currently affect no decision and can be removed without changing behavior.

## What holds up

With a writable cache, one config and prompt successes, the view plus 300 ordinary prints share roughly **230 requests** in ten minutes, or **253** counting both endpoints, below QR4’s 345. With continuous 429s, prompt cancellation and a committed block, even counting all four initial requests plus the minute-five probe gives **five**. The next probe is at minute 15. Forced prints add zero. The separate lock file survives quote-file rename, and wall-clock age survives sleep.
