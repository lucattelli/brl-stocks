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
