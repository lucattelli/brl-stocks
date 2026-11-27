# Scope: the full-screen view, static (slice 2)

Written 2026-11-24 from the spec's surface tags (D78, D80, D81): the criteria tagged `view`, 128 of 165, split by slice and, for this slice, into increments (D82, D85). The view in this slice fetches once when it opens and then shows what it has; refreshing on its own, `r`, and watching the config file are slice 3.

## Entry points

- `brl-stocks`, with no command, on a terminal: the full-screen view (7.3), with `--config <path>`
- In the view: the keys of 7.3 (`q`, Ctrl+C, `s`, arrows, `j`/`k`, `?`, any other key), and a resize of the terminal
- `brl-stocks --help`, `--version` and an unknown flag without `print` (D77)

## The view's criteria, by slice

| Slice | Criteria | Count |
|---|---|---|
| 2, this one | The five increments below, and S17.1 by hand | 95 |
| 3, live refresh | S5.1–S5.3, S5.7, S5.8, S5.10; S6.4, S6.5, S6.6; S7.1–S7.9; S8.3, S8.4; S11.3, S11.4, S11.6, S11.10, S11.11, S11.14, S11.19; S15.3 | 28 |
| 4, sparkline | S10.1–S10.3 | 3 |
| Waiting on Q62 | S5.9, S5.11 | 2 |

Why the slice 3 ones are not here: each needs time to pass with the view open (a refresh interval, a wait after a failure, a key that forces a fetch, a file saved while the view runs). S5.10 is about how often rows are requested while the view runs. S6.5 and S6.6 need an error line on screen before a refresh clears it, which the spec only gives while the view runs.

## Increments

All five built and verified on 2026-11-25 (F38): PRs #3 to #7.

Grouped by D85: each reaches the edge (something on the screen changes), criteria with the same setup go together, the order follows what each needs from the one before, and each is one PR.

| # | Increment | Criteria | Needs |
|---|---|---|---|
| 1 | **Opens, draws the rows, quits.** One fetch at start, after the cached rows; which config, with `--config`; a stand-in source; every column of a row; number formats; currency; colour; `q` and Ctrl+C exit 0; the command line without `print` | S1.1, S1.2, S1.3, S1.4, S1.5, S1.7, S1.8, S1.9, S1.10, S1.11, S1.12, S1.13, S1.14, S1.15, S3.1, S3.2, S3.3, S3.4, S3.5, S3.6, S3.7, S11.2, S15.1, S15.4, S15.7, S16.1, S16.2, S16.3, S16.4, S17.2, S17.4, S18.1, S18.2, S18.3, S18.4 (35) | PR 0's boundary |
| 2 | **Summary line, groups and order.** | S2.1, S2.2, S2.3, S2.5, S2.6, S2.7, S2.8, S4.1, S4.2, S4.3, S4.4, S9.1 (12) | Increment 1's rows |
| 3 | **Market states, stale rows, the error line, not-found rows.** The status line's market states; stale marks from the cache and from a failed fetch at start; the error line with its cause, count and next attempt | S5.4, S5.5, S5.6, S5.12, S6.1, S6.2, S6.3, S6.7, S6.8, S6.9, S6.10, S6.11, S8.1, S8.2, S8.5 (15) | Increments 1 and 2 |
| 4 | **The rest of config, cache and source at start.** The default and XDG paths, through a link, never changed; errors at start; warnings and the example watchlist in the status line; the cache folder and an unwritable cache; the source not selectable | S11.1, S11.5, S11.7, S11.8, S11.9, S11.12, S11.13, S11.15, S11.16, S11.17, S11.18, S12.1, S12.2, S12.3, S12.4, S12.5, S15.2, S15.5, S15.6, S15.8, S17.3 (21) | Increment 3's status line |
| 5 | **Narrow terminal and keys.** Columns dropped as the width shrinks; following a resize; sort, scroll, help; other keys do nothing; the summary line stays while scrolling | S1.6, S2.4, S9.2, S9.3, S9.4, S9.5, S9.6, S9.7, S9.8, S9.9, S9.10 (11) | Increments 1–3 |

Notes for the briefs:
- Open or closed comes from the trading period the stand-in source returns with each quote, as for `print`; S5.4–S5.6 and S5.12 do not wait on Q62.
- S6.7 and S6.10 start from a cache left by an earlier run (a fetch 12 minutes ago) and a source that holds its answers, so the view shows the cached rows before its fetch at start. S15.6 is two runs with the config changed between them.
- S1.4 "before any answer from the source has arrived": a source that holds its answers.
- S9.x and S2.4 need more rows than the screen has.

## Not by an acceptance test

| Criterion | Route |
|---|---|
| S17.1 | Needs the real source: the hand check in `verification/README.md` (QR10), run with `brl-stocks` instead of `print` |

## Quality requirements tagged `view`

In this slice, from increment 1 on: QR1 (every test with 20 tickers as well), QR7 (config untouched after every test), QR2 (rows from the cache within 300 ms of launch, with the cache read) and QR12 (the traffic of a view run, recorded by verification). Increment 5: QR11 (a key answers within 100 ms while the fetch at start runs). QR10 by S17.1, by hand. Slice 3: QR4, QR5 and QR6 with the view open, and QR8 with the view.

## Scope check

Run on 2026-11-24 by a fresh agent with the spec, the entry points and this file. Coverage: all 128 ids once. Applied: S6.7 brought into the slice (a cached row starts stale, 6.2); S6.5 and S6.6 moved to slice 3; the cache read, `--config` and the stand-in source moved into increment 1, which every later test needs; the old increment 2 split in two by setup; QR1, QR2, QR7 and QR12 placed; a note that open or closed comes from the stand-in's trading period.
