# Critique of the converged design — Codex, round 2

Critic: Codex CLI 0.170.0, model `gpt-6.2-astra`, high reasoning, read-only, 2026-11-23, a fresh session that did not see round 1. Reviewed revision 2 of `converged.md`, with `critic-brief-converged.md` plus a note naming the changed parts. The text below is the critic's final message, unedited; the coordinator only added this header. 80,876 tokens.

## Verdict

I would keep the shared engine and provider boundary, but I would not build the refresh protocol as written.  
The required change is to distinguish **which rows were actually requested** from **whether the source may receive another request**. A round timestamp cannot answer both questions.  
Revision 2 handles ordinary view/`print` overlap. It still fails approved criteria under slow healthy replies, lost block state, and ordinary market transitions.

## Fact check

| Claim | Verdict | Source |
|---|---|---|
| Bubble Tea v2 exists at `charm.land/bubbletea/v2`. Commands perform I/O, `Tick` needs rearming, and concurrent command results can arrive out of order. | Correct. v2.1.2 was published November 18, 2026. `View()` returns `tea.View`. | [Versioned API](https://pkg.go.dev/charm.land/bubbletea/v2@v2.1.2) |
| `time.Now().Round(0)` lets freshness calculations count sleep. | Correct. It strips the monotonic reading. Wall-clock corrections can also move apparent ages backwards. Linux `CLOCK_MONOTONIC` excludes suspend. | [Go time](https://pkg.go.dev/time#hdr-Monotonic_Clocks), [Linux clocks](https://man7.org/linux/man-pages/man3/clock_gettime.3.html) |
| Nonblocking `flock` permits immediate fallback. A stopped holder retains its lock. | Correct on local Linux. “On death only” is wrong. Explicit unlock or closing the last descriptor referring to the open file description releases it. The proposed syscall is not portable to Windows. | [flock(2)](https://man7.org/linux/man-pages/man2/flock.2.html) |
| Same-directory rename gives readers a complete cache. `stat` follows the config symlink. | Correct for cooperating local writers. An already-open reader can still return the **old** complete file after replacement. | [rename(2)](https://man7.org/linux/man-pages/man2/rename.2.html), [stat(2)](https://man7.org/linux/man-pages/man2/stat.2.html) |
| Text methods make `Instrument` a JSON map key. Passing `Snapshot` by value isolates commands. | The key works in both directions. Isolation would be false: copying the struct leaves its maps shared. Snapshot ownership needs defining. | [Go JSON](https://pkg.go.dev/encoding/json), [Go value representation](https://go.dev/ref/spec#Representation_of_values) |
| HTTP cancellation and cross-host redirect rejection are available. | Correct with request contexts and `CheckRedirect`. Cancellation cannot retract a request already received. | [Go HTTP](https://pkg.go.dev/net/http) |
| One keyless Yahoo chart call supplies quote fields, five-minute series, regular period and zone. The symbol mappings work. | Supported for the instruments sampled in F2/F13–F15/F22. Yahoo also confirms the class-share, S&P and FX symbols. Holiday periods, FX hours, extended-hours exclusion, closing latency and exhaustive refusal signatures remain **[unverified]**. HTTP 429 means rate limiting. | [Recorded observations](/home/lucattelli/Programming/prototyping/brl-stocks/research/grilling-log.md), [Yahoo class share and index](https://finance.yahoo.com/quote/BRK-B/), [Yahoo FX](https://finance.yahoo.com/quote/BRL%3DX/history/), [RFC 6585](https://www.rfc-editor.org/rfc/rfc6585#section-4) |

## Findings

1. **Fatal — Healthy replies defeat forced refresh.** Press `r` with 20 eligible B3 rows and four-second successful replies. Eight requests start immediately, eight at four seconds, four at eight seconds. Twelve miss S7.1. Separately, start a round at `t=0`, send a late-wave row at `t=8`, finish at `t=9`, then force at `t=10`. The round stamp permits requesting that row again after two seconds. Conversely, a round for one symbol suppresses an untouched symbol at `t=2`. Both contradict §6.5’s per-row guard. “Refresh starts” changes D50.

2. **Fatal — Eight requests per round is not QR5’s rolling budget.** With intact state and dispatch stopped on first refusal, ten minutes of 429s cost at most eight initial requests plus one probe at minute five: **9 ≤ 10**, even with `print --refresh` every two seconds. Now truncate the cache at minute one and replace it with a pre-block copy at minute two. Each next forced caller can send eight. That is **16 after the first refusal**, excluding the original round. Killing the holder after receipt of a 429 but before its rename also loses the refusal. The persisted stamp buys only five seconds. Writing before cancelling additionally requires an explicit stop on worker dispatch during the write. D38, D49, QR5.

3. **Serious — A busy lock silently drops work.** A view fetching list A holds the lock. A `print` with list B and five-minute-old quotes loses `TryLock` and prints without requesting B. S13.3 fails even if the holder is healthy. Stop the holder mid-round and every subsequent caller can serve ageing prices indefinitely, including after the 20-second deadline. Kill it instead and the lock clears, but completed, uncommitted requests can be repeated.

4. **Serious — The merge has no ordering rule for source health.** A reader opens the pre-block cache. A cycle commits a refusal, then the old `loadedMsg` arrives. `LastAttempt` orders entries only. Applying old health can erase the countdown; always retaining blocked health can resurrect it after recovery. Replacing disk with an older valid quote also lets `Cycle` republish it because disk wins for existing keys. After truncation the screen can retain quotes, yet a closed-market cycle with nothing due returns without restoring the file. The next `print` starts cold.

5. **Serious — Closing and reopening are guessed incorrectly.** A US closing correction at 16:08 is missed after the five-minute cutoff. A B3 close already delivered at +2 minutes is polled until +30. Ordinary `print` also inherits settling calls despite S13.5. Seven holiday probes per entry mean **161 requests for 23 affected entries** against §6.1’s zero. If old session data persists through the first half-hour of a delayed opening, probing stops for the day. B3 actually opened at 13:00 on Ash Wednesday 2026. After 14 hours asleep, yesterday’s sessions likewise leave old prices looking fresh until one current session arrives (S5.4, S5.7, S6.7). [B3 calendar](https://www.b3.com.br/pt_br/noticias/calendario-de-negociacao-da-b3-confira-o-funcionamento-da-bolsa-em-2026.htm)

6. **Serious — Probe and display rules lose special cases.** After another config triggers a block, switch to a list consisting entirely of never-quoted, not-found symbols. At expiry there is no eligible probe, even when the hourly retry matures. Conversely, a formerly quoted, now-not-found symbol qualifies as the first quoted probe inside its hourly wait. A probe returning HTTP 500 or malformed HTTP 200 has no explicit transition. `Classify` also needs precedence: a first timeout has `Fail != nil` and no quote, yet §6.2 requires `no_data`; an unknown ticker requires `not_found` (D39, S8.1).

7. **Serious — Config timestamps and retained quotes contradict reload semantics.** Replace the config through its symlink with a different inode and preserved older mtime. The view detects the edit, adds and removes rows, yet skips the required not-found retry (S8.4). Two asynchronous parses can also complete newest first and restore the older list. Remove a quoted ticker, refresh successfully, then re-add it offline tomorrow: seven-day retention and the model merge can both restore the expressly forbidden quote (S15.6). Different configs also recompute a shared failure wait using different intervals (§6.5, S13.16).

8. **Serious — Two proposed exceptions change acceptance tests.** Start `print` with an unwritable empty cache and a healthy source: it returns no quotes (S15.5). Start at market open with one series point and accumulate a full day: variable-length JSON can exceed the 10% limit (QR8, S15.3). A bounded representation of the ten displayed sparkline positions is worth testing before declaring the criterion defective.

9. **Minor — Day-one contracts are incomplete.** Reload a valid config containing an unknown key: `configMsg` has no warning field. Switch to the stand-in with a fresh Yahoo cache: unqualified entries can return Yahoo prices under S17.2. A fixed `Now` also leaves actual HTTP deadlines running on real time.

10. **Excess — Seven-day retention.** Delete it. The loss is warm data for an alternate watchlist; retaining removed symbols already breaks the single-watchlist acceptance test.

## Provisional choices

- **X1:** Keep the nonblocking lock for ordinary overlap. A short expiring claim has a stronger case when a stopped holder must not freeze every caller.
- **X2:** Keep one instrument per Yahoo call. A genuinely batch-native second provider would justify revisiting it.
- **X3:** Reject the shared failure domain. A separate pacing record survives replacement of quote data.
- **X4:** Keep direct Bubble Tea messages. A separate step layer earns its cost only if command ordering remains difficult to test.
- **X5:** Keep live quotes through truncation. Define ordering for health, removals and config revisions before accepting the merge.
- **Bubble Tea v2:** Keep it. Existing v1 integration would favor v1; this design identifies none.

## What holds up

With a healthy source, intact cache and one watchlist, ten rounds × 23 entries give **230 requests**, or **253** counting an endpoint round, below QR4’s 345. Under-lock replanning prevents two winners, and fresh two-second `print` calls send zero requests. Silent cold start after truncation, symlink-preserving config reads, and keeping the last valid config after a syntax error also survive the walkthroughs.
