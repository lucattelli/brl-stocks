# Design exploration — synthesis

Written 2026-11-23, after three designs, three critiques and three rebuttals (all in this folder). This file records where the designs ended up agreeing, where they still disagree, and what the critiques raised that no design had covered. The disagreements go to the user as questions Q38 onward; the answers are recorded in `../grilling-log.md`.

Run: three Claude designers (stances: simplest, most robust, easiest to change and test), three fresh Claude critics (one per design), one rebuttal each. No other model family took part. None of the designers looked facts up; the critics did.

## 1. Outcome of the critiques

No design had a fatal finding. Serious findings: simplest 3, robust 5, extensible 6. Every serious finding was conceded by its author with a fix; none was rebutted. Authors contested only "excess" ratings.

## 2. Converged after rebuttal

All three authors now hold these, or dropped their objection:

| # | Point | Notes |
|---|---|---|
| C1 | **No scheduler.** A short heartbeat tick reads the wall clock and asks a pure function which entries are due. | Simplest and robust tick at 1 s, extensible at 5 s. Survives sleep and wake without special detection (robust deleted its wake detection). |
| C2 | **One refresh path** shared by the view and one-shot: load cache, fetch what is due, save, return. The view runs it inside a Bubble Tea command; `Update` does no I/O. | |
| C3 | **Cache is JSON written by temp file and rename; readers never lock.** Missing, corrupt or other-version cache is treated as empty. | Robust wants the user told when the cache was corrupt. |
| C4 | **The view re-reads the cache when the file changes**, so it picks up quotes fetched by a one-shot call. | Fix for the simplest design's S1; the other two had it. |
| C5 | **Per-entry failure count and last-attempt stamp** in the cache, giving back-off that all processes share. | |
| C6 | **A forced refresh skips entries attempted less than 5 seconds ago.** | Protects against `print --refresh` in a loop. |
| C7 | **After the close, an entry stays due until the closing quote has arrived**, bounded by a settle time. | Needed because B3 is 15 minutes delayed. Settle: 20 min (robust) or 30 min (extensible) for B3. |
| C8 | **A ticker the source does not know waits one hour between tries**, reset when the config changes. | |
| C9 | **Typed provider errors**, per ticker and source-wide; the provider checks that the response is for the symbol requested. | |
| C10 | **No router, no auth, no provider registry yet.** They wait for a second provider. The interface keeps a provider name and an "unsupported" result so a one-market provider can be added without changing callers. | |
| C11 | **Open or closed comes from the session data in the last quote**, with a re-probe at the predicted open (every 5 min for 30 min) when the source still reports an old session. | Rests on F14, unverified for holidays. Next check: US Thanksgiving, 2026-11-26. |
| C12 | **Small items:** `refresh_seconds` has a floor of 10; a configurable User-Agent; temp-file cleanup; supports Linux and macOS only. | The platform limit is a question for the user (it follows from file locking and rename). |

## 3. Still disagreed after rebuttal

| # | Point | Simplest | Robust | Extensible |
|---|---|---|---|---|
| X1 | Cache coordination | No lock: rename plus an "attempting now" stamp | Lock held across the fetch | Lock held across the fetch, tried without waiting |
| X2 | Provider call | One ticker per call; the core runs the workers | Batch; the provider owns concurrency | Batch |
| X3 | Source-wide block state | None: per-entry counters, and a source-wide error cancels the round | A persisted source-health record, only 401/403/429 mean blocked | Health keyed by source name, in the same cache file |
| X4 | A step layer between Bubble Tea and the logic | No: `Update` is already that | No separate layer; pure plan and classify functions | Yes: `Step(State, Event) (State, []Effect)` |
| X5 | Merging memory with disk on each cycle | No | Yes, plus a 7-day prune | Yes, plus a prune |

Strongest surviving argument on each side:

- **X1, for no lock:** the critic could not widen the race beyond milliseconds; the cost is one duplicated round or one interval of an old quote. A lock is platform-specific. **For the lock:** the stamp "rebuilds a lock from weaker parts", writes the file twice per round, and a lost write also erases failure counters. With a try-lock that never waits, the hung-holder problem (the extensible design's S1) is gone.
- **X2, for one ticker:** no free source in the research can batch, and a batch signature moves concurrency and partial failure into every provider. **For batch:** rate limits and the right reaction to a 429 are knowledge about the source, so they belong in the provider.
- **X3, for none:** thresholds are guesses and a captive portal could silence every process. **For a record:** in ten minutes of 429 the simplest design sends about 35 requests against 6 to 8, and a newly added ticker is fetched straight into a block.
- **X4, for the layer:** effects as data can be compared in a table test, and the Bubble Tea version question is confined to one file. **Against:** it duplicates what `Update` already is, for features the log defers.

## 4. Raised by the critiques, covered by no design

These are behaviour, not construction, so they belong in the spec:

| # | Gap | Positions after rebuttal |
|---|---|---|
| B1 | What `r` does while the source is blocking | Robust: one probe at most every 60 s. Extensible: `r` overrides a wait after a network error, not a rate limit or block, and the status line says when the next attempt is. |
| B2 | What a row shows for a ticker the source does not know | All: an error row; retried hourly. |
| B3 | When a row counts as stale | Simplest: a failed fetch, or an open market and a last success older than two intervals. |
| B4 | Whether the user is told about a corrupt cache | Robust: yes. Others: silently treated as empty. |

## 5. Facts established

- Bubble Tea: version 1 last released v1.3.10 on 2025-09-17; version 2 is at v2.1.2, released 2026-11-18 (Go module proxy, queried 2026-11-23). Only the simplest design named a version (1).
- `syscall.Flock` takes no context and cannot be cancelled; two opens of the same lock file in one process block each other (critique of the extensible design, with sources).
- Go's monotonic clock reading must be stripped (`Round(0)`) from every time compared across sleep (critique of the robust design, with sources).

## 6. What this run did not test

- All nine agents were the same model. Shared assumptions were not challenged from outside.
- No critic saw more than one design, so no critique compares them; the comparison above is the synthesiser's.
- The converged design as a whole has not been attacked, only its three ancestors.

## 7. Check against the approved spec (added 2026-11-23, on resuming the run)

The run was made against a hand-written draft that has since been replaced by `docs/spec.md`, approved as D52. What the spec changes here:

- **The behaviour gaps are closed.** B1 is D38, B2 is D39, B3 is D40, B4 is D48 (a corrupt cache is treated as absent, silently, which also settles the robust design's wish in C3).
- **C7:** the settle time is 30 minutes for every market (D50, P31).
- **C5 and the back-off ladders:** the waits are now behaviour. Per row, interval × 2^(n−1) up to 30 minutes; after a refusal, 5 minutes doubling to 30; both shared by the view and one-shot calls (D50, P28). The designs' own ladders (5, 10, 20 s and so on) no longer apply.
- **X3 is narrowed by D49.** At most 10 requests in any 10 minutes of blocking, all instances together (QR5), cannot be met by per-entry counters alone (about 35 requests by the simplest design's own figure). Some shared record of the block is needed; what stays open is its form and what sets it. Only a refusal is a block (D50, P29).
- **X1 is not settled by the spec.** QR4 allows 15 requests per entry in 10 minutes, which leaves room for an occasional duplicated round.
- **X5 meets a new rule:** the cache drops tickers that left the config (D50, P45), where the designs pruned by age so that two configs could share a cache.
- **New points no design covered:** how a one-shot call knows that the config changed (D39), what happens with an unwritable cache folder in one-shot (D50, P42 against QR4), how the status line derives a market's state (D47), and how a test replaces the source (S17.2).

`converged.md` is the design that follows from C1–C12, these changes, and the coordinator's provisional side of X1–X5. It is what stage 7 attacks.

## 8. Stage 7, round 1: Codex attacks the converged design (2026-11-23)

Critic: Codex, `gpt-6.2-astra`, high reasoning, read-only; one agent, 66,759 tokens. It followed the format (ratings, scenarios, a fact check with sources). File: `critique-converged-codex.md`. It reviewed revision 1 of `converged.md`.

Outcome: 2 fatal, 7 serious, 1 minor, 2 excess, 2 wrong facts. This is the first review by another model family, and it found faults that nine Claude agents had not.

| # | Rating | Finding | Coordinator's handling in revision 2 |
|---|---|---|---|
| 1 | fatal | Four workers and a 15 s deadline cannot fetch 23 rows on `r` when answers take 5 s; S7.1 fails | Fixed in part (8 workers, 5 s per request, cut-off rows stay due). The criterion's wording is a question for the user (B8) |
| 2 | fatal | `app` and `tui` import each other; a struct map key with only `MarshalText` cannot be decoded, so every restart loses the cache silently (D48 hides it) | Fixed: a `deps` package, `UnmarshalText`, a round-trip test |
| 3 | serious | One stamp stood for "queued", "sent" and "committed": a killed holder leaves 19 rows marked attempted that never were; a refusal can be lost before the final write; a stopped holder blocks everyone | Fixed for the first two (round stamp, stamp on send, refusal written at once). The stopped holder is the strongest surviving objection to X1 |
| 4 | serious | The two-connection cutoff invents failures on rows never tried (against spec 6.3); a probe that times out or hits a not-found ticker never resolves the block | Fixed: cutoff deleted; probe outcomes defined |
| 5 | serious | Without a writable cache nothing paces `print`: 6,900 requests in ten minutes; D49 has no exemption | Behaviour gap for the user (B5) |
| 6 | serious | Two configs on one cache erase each other's quotes (P45); the config hash is not scoped to a config | Hash replaced by the config's modification time; pruning is a behaviour gap for the user (B6) |
| 7 | serious | After sleep a row with yesterday's session can read fresh while the header says open; the closing quote's time stamp never reaches the session end | Fixed: `Classify` uses the market's state; a settle window replaces the time-stamp match |
| 8 | serious | One quote per ticker does not bound the cache to 10% growth: the intraday series grows all day | A flaw in the criterion (QR8, S15.3), for the user (B7) |
| 9 | serious | `Stat` inside `Update`; a late load can overwrite a newer result; a redirect to another host breaks QR12 | Fixed: stat in a command; order-independent merge; no cross-host redirects |

Wrong facts: "two equal stamps prove a save finished" (dropped); "flock limits the tool to Linux and macOS" (it is any Unix-like system).

What held up, by the critic's own count: with a writable cache and one config, the view plus 300 `print` calls stay near 230 to 253 requests in ten minutes (QR4 allows 345); under continuous refusals, 5 requests in ten minutes (QR5 allows 10).

Its view of the provisional choices: X1 conditional (a short lock around a claim is stronger when a holder stalls); X2 keep; X3 conditional (a separate block file survives the cache being replaced); X4 keep; X5 reject as first written (now narrowed); Bubble Tea version 2 keep.

### Behaviour gaps raised by this round

| # | Gap | Provisional answer in `converged.md` |
|---|---|---|
| B5 | What `print` does when the cache folder cannot be written (D50's P42 against D49) | It does not fetch; it prints what the cache holds and warns. The view still fetches |
| B6 | Whether the cache drops tickers that left the config (D50's P45) when two configs can share one cache | Prune by age (7 days) instead; amends P45 and S15.6 |
| B7 | How "no history" is measured (QR8, S15.3), since one day's series grows | One quote and at most one trading day of series per ticker; reword the criterion |
| B8 | Whether `r` must send every request within 2 seconds (S7.1) or start within 2 seconds (P33) | Start within 2 seconds |

A second round was started on revision 2, because round 1 produced fatal and serious findings that changed the design.

## 9. Stage 7, round 2: Codex attacks revision 2 (2026-11-23)

Critic: a fresh Codex session, same model and settings; one agent, 80,876 tokens. It followed the format. File: `critique-converged-codex-2.md`.

Outcome: 2 fatal, 6 serious, 1 minor, 1 excess. The fixes for round 1's findings 2, 4 (cutoff), 7 (stale after sleep, in part) and 9 held. What it found:

| # | Rating | Finding | Where it goes |
|---|---|---|---|
| 1 | fatal | With 4-second answers, 12 of 20 rows miss S7.1's two seconds. The round stamp is the wrong grain: it lets a late-wave row be requested again within 5 seconds and suppresses rows the round never touched (spec 6.5 is per row) | The criterion's wording: Q61. The grain: per-row claims, option C of Q39 |
| 2 | fatal | QR5 holds with intact state (9 requests in ten minutes). It fails if the cache file is emptied and then replaced by a copy from before the block: 16 requests | Q41: the block record in a file of its own |
| 3 | serious | A busy lock drops work: `print` with another watchlist loses the lock and prints five-minute-old quotes without fetching (S13.3). A stopped holder freezes every caller | Q39 |
| 4 | serious | The merge orders entries, not the source's health: an old read can erase a block's countdown. A closed-market cycle with nothing due does not restore an emptied file | Q41, and point C17 of Q38 |
| 5 | serious | Close and open are guessed: a late US correction is missed, B3 is polled for 30 minutes after a close that arrived in 2, seven holiday probes per entry are 161 requests where spec 6.1 says none, a delayed opening stops probing for the day | Waits on research (F14 and the closing-quote measurement): Q62 |
| 6 | serious | Probe and display rules miss cases: no eligible probe when every wanted ticker is not found; a probe answered with a server error has no rule; `Classify` needs an order of precedence (no data before stale, not found first) | Detail for the design document; recorded |
| 7 | serious | A config replaced with an older modification time is reloaded but not-found tickers are not retried; two parses can finish out of order; a 7-day retention restores a quote that S15.6 forbids | Q60; the rest is detail for the design document |
| 8 | serious | The two proposed exceptions change acceptance tests (S15.5, QR8). Keeping only the ten displayed sparkline points would hold QR8 as written | Q59; point C14 of Q38 |
| 9 | minor | No warning field on a config reload; a stand-in source can be served Yahoo's cached prices; real HTTP deadlines under a fixed test clock | Detail for the design document |
| 10 | excess | The 7-day retention | Q60 |

Wrong fact: a `flock` is released on unlock or when the last descriptor closes, not "on death only".

What held up: with a healthy source, an intact cache and one watchlist, 230 to 253 requests in ten minutes (QR4 allows 345); re-planning under the lock prevents two winners; fresh `print` calls send nothing; a silent cold start after truncation; reading the config through the symlink; keeping the last valid config.

Its view of the provisional choices: X1 keep the non-blocking lock for ordinary overlap, with a short expiring claim as the stronger alternative when a holder stalls; X2 keep; X3 reject (a separate pacing record survives the quote data being replaced); X4 keep; X5 keep, once ordering is defined; Bubble Tea version 2 keep.

### Why the attack stops here

Two rounds were run; the limit is three. A third was not started, because what remains is no longer about the design's shape:

- findings 1 to 4 land on the questions the user has to decide (Q39, Q41) or on wording in the spec (Q59 to Q61);
- finding 5 rests on facts that are still unverified (F14, the closing-quote delay), and one of them can only be checked on 2026-11-26;
- findings 6, 7 and 9 are rules of detail that the design document must state and tests must pin down. Another round would attack the coordinator's patches, not a decision.

### Converged points after the attack

C1–C12 stand, as changed in section 7. Added or changed by the two rounds:

| # | Point |
|---|---|
| C2 | `Update` does no I/O at all, including `stat`: every file access is a command |
| C3 | The cache's keys are encoded and decoded by a matching pair of text methods, with a round-trip test for every market |
| C4 | A snapshot that arrives is merged into the view's, never assigned; the merge does not depend on arrival order |
| C6 | The 5-second guard on a forced refresh is per row |
| C8 | A not-found ticker is retried hourly, or when the config file is newer than its last attempt |
| C11 | A market is open when the newest session among its entries contains now; a row's stale state uses the market's state |
| C13 | A refusal is saved the moment it is seen; after the wait, one probe request; a probe's outcomes are all defined |
| C14 | The cache keeps the sparkline as the ten points shown, so its size does not grow during the day (QR8 as written) |
| C15 | Tests inject the source, the clock and the environment; the heartbeat is a message; the whole tool runs through one function |
| C16 | 8 parallel requests, 5 seconds each; a round ends after 10 seconds for `print`, 20 for the view (guesses) |
| C17 | The file is the authority. The view's memory only keeps the screen alive when the file is emptied, and the view rewrites the file at its next write |

### Still disagreed, as they go to the user

- **X1 (Q39):** A, a non-blocking lock held across the fetch; B, no lock, rename and a claim stamp; C, a short lock around each write, with per-row claims and the fetch outside the lock. C is the coordinator's recommendation after the attack. It is the simplest design's claim plus the lock that stops lost writes; as a combination it was not attacked.
- **X2 (Q40):** one instrument per call (recommended; both Codex rounds agree) against a batch (two of three authors).
- **X3 (Q41):** the block record in the cache file, or in a file of its own (recommended after round 2).
- **X4 (Q42):** no step layer (recommended; two of three authors and both Codex rounds) against a step layer.
- **X5:** folded into C17.

### What this run did not test (update of section 6)

- The critics from another family were both the same model, in two sessions.
- Option C of X1 and the separate block file were recommended after the last round and were not attacked.
- Nothing was built or run; every threshold is a guess.
- The session, holiday and closing rules rest on unverified facts.

### Cost of stage 7

No Claude subagents. Codex: one test prompt (3,834 tokens) and two critics (66,759 and 80,876 tokens).

## 10. Stage 7, round 3: Codex attacks the approved design (2026-11-23)

Run at the user's request after `docs/design.md` was approved (D63), because the protocol chosen in D54 and D56 had been recommended after round 2 and never attacked. Critic: a fresh Codex session, same model and settings, read-only; one agent, 45,406 tokens. Scope: the refresh protocol and its shared state; the close, open and holiday rule was excluded (Q62). Files: `critic-brief-design.md`, `critique-design-codex-3.md`. It followed the format.

Outcome: 3 fatal, 6 serious, 1 minor, 1 excess.

| # | Rating | Finding | Handling |
|---|---|---|---|
| 1 | fatal | Rows claimed before a refusal are still requested after it: two processes with eight claims each send 16 requests after another process's refusal (QR5 allows 10) | Patched: a worker reads the source file before each request |
| 2 | fatal | After a claim expires, the old holder's commit overwrites a newer quote, or brings back a removed row (S15.6) | Patched: a claim id; a commit applies only to rows it still owns |
| 3 | fatal | A process stopped inside the lock makes every other process wait without limit (S13.9) | Patched: the lock is tried for at most one second |
| 4 | serious | A missing or replaced source file forgets the block; an empty read erases the view's own block | Patched: the refusal is also on the rows; the later record wins |
| 5 | serious | The probe has no lifecycle: a dead probe claimer, outcomes other than success or refusal, which ticker to probe, a late success clearing a newer refusal | Patched: all defined |
| 6 | serious | Two watchlists on one cache: 3,300 requests in ten minutes from two alternating `print` loops | A question for the user (Q66): D61 accepted the extra traffic before it was counted |
| 7 | serious | Claims outlive finished or abandoned work, so rows cut off by the deadline wait 20 seconds (against D62) | Patched: a commit releases all claims of its round |
| 8 | serious | "Never replace a quote with nothing" does not define a merge: a later failure must show the old price as stale, aged from its success | Patched: a field-by-field merge rule |
| 9 | serious | A clock stepped back keeps a claim alive for an hour; a config replaced with an older modification time does not retry not-found rows | Patched in part: future stamps are void; the view retries on reload. `print` keeps the modification-time rule |
| 10 | minor | At exactly one interval the design fetched where the spec prints from the cache | Patched |

What held up: with one watchlist and ordinary timing, 230 to 253 requests in ten minutes (QR4 allows 345); with a saved block and a working probe, 9 (QR5 allows 10); emptying only the quote cache keeps the block; `Classify`; the config is only read; ten sparkline points bound the cache.

This was the third and last round. The patches went into `docs/design.md` (section 4, "The refresh protocol in detail"), which is a draft again. They were not attacked; the design document lists the tests that would check them.

Cost of round 3: no Claude subagents; Codex 45,406 tokens.

