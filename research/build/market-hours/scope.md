# Scope: close, open and holidays (Q62)

Written 2026-11-27 from D111–D113 and the spec as updated for them. Q62 was the last open question of the spec. It held back S5.9 and S5.11 since slice 2, and its numbers were provisional in `internal/market/plan.go` (a 30-minute settle for B3; checks every 5 minutes for 30 minutes after an expected open).

## Surfaces

- `brl-stocks` with no command, on a terminal (the view): which rows are requested after the close and at the usual open, and the market states in the status line.
- `brl-stocks print`: which rows are requested (S13.5), and the market states on its last line (S5.9, S5.11).

## The criteria

10 criteria, in one increment.

- **New** (D111, D112): S5.13–S5.17.
- **Reworded** (D111–D113); their tests from slices 2 and 3 change to the new numbers: S5.3 (US, 5 minutes), S5.8 (B3, 100 minutes, every row), S13.5 (the settle time of 6.1).
- **Waiting on Q62 until now:** S5.9 (a holiday shown as closed, view and print) and S5.11 (US before its regular session: the last regular close, view and print).
- **Shown again** with the holiday rule in place: S5.7, the market opening on the next weekday, and S5.5, the status line.

## Increments

| # | Increment | Criteria | Needs |
|---|---|---|---|
| 1 | **Close, open and holidays.** The settle time per market; the two checks at the usual open; a holiday shown as closed | S5.3, S5.8, S5.9, S5.11, S5.13–S5.17, S13.5 (10), plus S5.5 and S5.7 shown again | Slices 2 and 3's view and print; no boundary change |

## Notes for the briefs

- **A holiday comes from the stand-in:** at the usual open it keeps answering with the last session's trading period (F43). A day that opens late after one check comes from the stand-in switching to that day's session between two answers (S5.16).
- **"Usual open"** is the last session's start, at the same time of day on the next weekday, in the exchange's time zone (design, rule 6).
- **Counting requests is done at the stand-in,** as in slices 2 and 3.

## Not by an acceptance test

| What | Route |
|---|---|
| The real B3 closing price within 100 minutes, and a real holiday | The measurements of F43–F46; a hand check on the next B3 holiday (24 Dec) |
| Verification of the real build | The verifier, through `BRL_STOCKS_SOURCE_URL`, with its stand-in's clock-free answers: only what the real clock allows (the settle time, scaled down where the README says) |

## Test boundary

No change.
