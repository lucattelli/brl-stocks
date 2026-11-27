# Scope: live refresh in the full-screen view (slice 3)

Written 2026-11-25 from the spec's surface tags (176 criteria, 139 tagged `view`) and slice 2's scope (`../full-screen-view/scope.md`), which placed these criteria here. In this slice the view keeps itself current while it is open: the heartbeat and the refresh interval, `r`, waits after failures and blocks while open, and watching the config file. Slice 2 covered everything the view shows when it opens and its one fetch at start.

## Surfaces

- `brl-stocks`, with no command, on a terminal, **left open while time passes** (7.3, 6.1–6.5), with `--config <path>`
- In the view: `r` (7.3), and the config file saved, broken or deleted while the view runs (7.2, D12, D45)

## The criteria

30 criteria, all tagged `view` (some also `print`, already covered there by slice 1): S5.1, S5.2, S5.3, S5.7, S5.8, S5.10; S6.4, S6.5, S6.6; S7.1–S7.10; S8.3, S8.4; S11.3, S11.4, S11.6, S11.10, S11.11, S11.14, S11.19, S11.20; S15.3. S7.10 and S11.20 were added to the spec after the scope check found transitions of 6.3 and 6.4 with no criterion.

Stories with criteria both here and elsewhere:
- **S5** (refresh): S5.4–S5.6 and S5.12 are what the view shows when it opens (slice 2, increment 3).
- **S6** (failures): S6.1–S6.3 and S6.7–S6.11 are shown by the fetch at start (slice 2).
- **S8** (not found): S8.1, S8.2 and S8.5 are shown when the view opens (slice 2).
- **S11** (the config file): the criteria about the file read at start (slice 2, increments 1 and 4).
- **S15** (the cache): all but S15.3 are shown at start or by `print` (slices 1 and 2).

Shown again by this slice but counted in slice 2, because their own text is met at start; the tests of the increment named show them again while the view is open:
- increment 1: S4.4 (a ticker in two groups is fetched once on a heartbeat refresh too), S6.7 (a row goes stale by age alone while the view is open), S5.4 and S5.5 (a market closes while the view is open), S5.12;
- increment 2: S6.1, S6.2, S6.3, S6.8, S6.9, S6.10, S6.11 and S8.2, on a heartbeat refresh that fails;
- increment 4: S15.6, through a config edit while the view is open.

## Waiting

- **S5.9, S5.11:** wait on Q62 (open, close and holidays; the closing quote). Not in an increment.

## Increments

Grouped by D85: each reaches the edge, criteria with the same setup go together, the order follows what each needs, each is one PR.

| # | Increment | Criteria | Needs |
|---|---|---|---|
| 1 | **Time passes with a healthy source.** The heartbeat and the refresh interval; markets opening and closing while the view is open; the not-found row's hourly retry; the cache's size over eight hours | S5.1, S5.2, S5.3, S5.7, S5.8, S5.10, S8.3, S15.3 (8) | PR 0's boundary (time steps) |
| 2 | **Failures over time.** Waits after failures and refusals while the view is open; the block ends; recovery clears stale marks and the error line | S6.4, S6.5, S6.6, S7.5, S7.10 (5) | Increment 1's heartbeat |
| 3 | **`r`.** A forced refresh, after a failure, during a block, on closed markets, twice in a row, on not-found rows, for 20 rows | S7.1, S7.2, S7.3, S7.4, S7.6, S7.7, S7.8, S7.9 (8) | Increment 2's failures and block |
| 4 | **The config file changes while the view is open.** Added and removed tickers, through a link, a broken file, a deleted file, a file that appears; a not-found row retried after a change | S8.4, S11.3, S11.4, S11.6, S11.10, S11.11, S11.14, S11.19, S11.20 (9) | Increment 1's heartbeat |

Notes for the briefs:
- Time passes only through the boundary's time step (design: the view's test boundary, "Time"). No test sleeps.
- A config file saved while the view runs gets its modification time set to the view's clock by the test (design, same section), so "newer than the last attempt" is decided on one clock.
- Open or closed comes from the trading period the stand-in source returns with each quote, and the view's clock, as in slice 2. The next session of a closed market is the same hours on the next weekday; holidays wait on Q62 (S5.9). S5.8 needs only a quote time before the close, not a recognised closing quote.
- Increment 2's build also makes the view take in what other processes fetched (it re-reads the cache, design C4); increment 1 did not need it, and QR4 does.
- The stand-in source reads the same clock as the view (`deps.Clock`) to stamp requests; it never waits on it, and answers at once (S7.9's "within one second" includes at once).
- S15.3 runs eight hours of view time; its size is measured on the cache folder after the first full refresh and at the end.

## Not by an acceptance test

| What | Route |
|---|---|
| QR4, QR5 (several processes, the view open and `print` every 2 seconds) | Verification of the real build, in real time, as for `print` in slice 1 |
| QR6 (no stale mark for 10 minutes with a healthy source) | Verification, with the smallest interval; the acceptance tests of increment 1 cover it in view time |
| QR8 with the view (eight hours) | The LONG verification check, run as `brl-stocks` |
| QR11 during a heartbeat refresh | Verification, as in slice 2, with a refresh running |
| The view left open overnight on a real terminal | Hand check |

## Scope check

Run on 2026-11-25 by a fresh agent with the spec and both scopes. Coverage: all 137 `view` ids of the spec at the time placed once. Applied: increment 2 split in two by setup (failures over time; `r`), now increments 2 and 3; the criteria shown again while the view is open listed by increment; S5.7 and S5.8 kept, with the reading of the next session and the close in the notes (holidays wait on Q62); two transitions with no criterion (6.3 the block ends and the source answers; 6.4 a config file appears while the example is shown) became S7.10 and S11.20 in the spec, from existing decisions; slice 2's count corrected to 104.

## Boundary critique

Run on 2026-11-25 by a fresh agent on the "Time" section and `deps.go`. Seven gaps, all applied: the stand-in could not see the view's clock (now `deps.Clock`, shared); a held request held the clock (a source waits on the clock instead); an Elapse could look settled half-way (counted only at its end); waits made during an Elapse, ties and splitting were not stated; modification times of the first config; the heartbeat period; `Deps.Now` and `Deps.After` unused with a clock. A second pass on the rework found five gaps closed and one new one: with the source waiting on the clock, nothing told the driver when that wait had woken and finished. Closed by narrowing: the source reads the clock and never waits on it; settled means only the view's own waits remain; "newer" means strictly later; the stale `Frame.Idle` comment and the design's code block updated; `Step` marked driver-only. Its four behaviour questions are answered by the criteria's own windows and wording (S5.1's range absorbs the fetch at start; S7.1's "within 2 seconds" is a limit; S15.3's eight hours can be one long session of the stand-in; S8.4 needs a changed file).
