# Scope: the sparkline (slice 4)

Written 2026-11-25 from the spec's surface tags (183 criteria, 146 tagged `view`) and the scopes of slices 2 and 3, which left S10 to this slice. In this slice each row of the full-screen view ends with a sparkline of the day (7.3, D13, D50). Slices 2 and 3 built the row without it: the renderer leaves the column out, while the engine already keeps ten points of the regular session per row (design C14) and the stand-in source's quotes already carry a series.

## Surfaces

- `brl-stocks`, with no command, on a terminal (7.3): the last column of each row, and the narrow terminal that drops it first
- No other surface shows it: `print` has no sparkline (7.4, D50), and the JSON output has no intraday series (7.5, D43)

## The criteria

10 criteria, all tagged `view`: S10.1–S10.10. S10.4–S10.10 were added to the spec by this scope, from what 7.3 already states with no criterion (D25, D50): the width and direction, the colour of the day change, the first column dropped on a narrow terminal, the regular session only.

Read across the spec for what a sparkline shows, and left where they are:
- **S1.6** (narrow terminal: four columns always remain) is slice 2's, and still holds with the sparkline; S10.8 is the sparkline's part of the order of 7.3.
- **S16.1–S16.4** (colour) are slice 2's and name the day change, not the sparkline; S10.5–S10.7 are the sparkline's. S16.3 (`NO_COLOR`: no colour codes in the frame) already covers the sparkline, since it covers the whole frame; its test in slice 2 is shown again with a series in increment 1.
- **S15.3, QR8** (no history; the cache within 10% after eight hours) are slice 3's; the ten points kept per row (C14) are part of what they measured.
- **S5.11** (US before its regular session: the last regular close) waits on Q62, as in slices 2 and 3. S10.9 does not: it asks only that prices outside the regular session are not drawn.
- **S1.4, S1.5, S6.7, S8.1, S15.1** (cached rows at start, no-data, stale and not-found rows) are slice 2's; what such a row shows in the sparkline column is Q101.
- **S5.4, S5.6, S5.8** (closed markets, the close) are slices 2 and 3's; which day the sparkline shows after the close and before the open is S10.2, and on a holiday waits on Q62.
- **S9.2–S9.7, S2.4, S4.4, S11.13** (sort, scroll, a ticker in two groups, more than 20 rows) move rows that carry a sparkline; their tests in slice 2 are not repeated, and QR1 holds: increment 1's tests run with 20 tickers as well.
- **The summary line** has no sparkline: 7.3 lists what an entry shows (value and percent).

## Waiting

- **S5.9, S5.11:** wait on Q62, as before. Not in this slice.
- What Q99–Q102 decide becomes criteria of S10 once answered (through `log-to-spec`), and joins increment 1.

## Increments

One increment: every criterion has the same setup (the view opened on a stand-in source whose quotes carry a series) and the whole slice reads in one sitting.

| # | Increment | Criteria | Needs |
|---|---|---|---|
| 1 | **The sparkline.** Drawn at full width, for an open and a closed market; 10 characters, earliest on the left; its colour; dropped first on a narrow terminal; no extra screens on any key | S10.1–S10.8, S10.10 (9), and the criteria from Q99–Q102 when answered | Slices 2 and 3's view, no boundary change |

Shown again by increment 1 with a series, counted in slice 2: S1.6 (the four columns that always remain), S16.3 (no colour codes with `NO_COLOR`).

Notes for the briefs:
- The stand-in source returns the series with each quote (`market.Quote.Series`, oldest first). The provider behind the boundary keeps only the regular session; the stand-in is in its place, so S10.9 is checked by verification (below).
- A closed market (S10.2) comes from the trading period the stand-in returns and the view's clock, as in slices 2 and 3.
- S10.3 passes before the build (no key opens anything): it needs a shown failure of its own. It is a whole-screen check, close to S9.9; it stays here as S10's own criterion.
- S10.1, S10.2, S10.4 and S10.10 need the glyphs and the scale (Q100) to say what to expect; until answered, they assert only what does not depend on them (a sparkline is there, its width, lowest left and highest right with block characters in their Unicode order).
- S10.2: the stand-in gives a closed market's last trading day as its series, with a clock after the close; it is S10.1 with a closed clock.
- The width of the full row is read from a frame at a wide terminal, then narrowed one cell (S10.8).

## Not by an acceptance test

| What | Route |
|---|---|
| S10.9 (regular session only) | Verification of the real build, through `BRL_STOCKS_SOURCE_URL` (D70): the source's own answer with a price far below the session before the open and one far above after the close; with the scale of Q100, either would change the line |
| The look of the glyphs in the user's terminal and font | Hand check |

## Test boundary

Unchanged. The stand-in source's `Quote` carries `Series` (`internal/market/types.go`); frames already allow block characters as one cell each and the green and red SGR codes (design: the view's test boundary, "Frames"). No critic is needed, since nothing in the boundary changes.

## Scope check

Run on 2026-11-25 by a fresh agent with the spec and this file. Applied: S10.4 split (width; the direction became S10.10); S10.8 reworded to one result with a measured width ("one cell narrower than the widest row with its sparkline"); S10.9 also covers prices after the close, as 7.3's "regular session" does; S10.2 gained "at full width"; the rows at start, no-data rows and the close listed above; QR1 noted. Its nineteen undecided points went to Q99–Q102, except: the summary line (7.3 already says what an entry shows), colour on a stale row (7.3: the colour of the day change, whatever the row's state), holidays (Q62), and whether a block character is one cell in the user's font (a hand check). S10.3 kept in S10, as the spec has it.
