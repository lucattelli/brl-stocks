# Scope: the one-shot mode

Written 2026-11-24, **after** the slice was built. The slice predates the scope stage (story-to-tests 0.5.0), so this file was rebuilt from the log's Build status row, the two test briefs and the verification feature map. Checked by a fresh agent on 2026-11-24 (stage 0, step 3); its findings and their sorting are in the last section.

## Entry points

- `brl-stocks print`, as a table (7.4)
- `brl-stocks print --json` (7.5)
- `brl-stocks print --refresh`, with either output
- `--config <path>` and `--help` on any of them (7.1)

## Criteria in the slice

| Story | In this slice | How it reached the slice |
|---|---|---|
| S13 One-shot table | S13.1–S13.24 (all) | Story of this mode |
| S14 One-shot JSON | S14 (all) | Story of this mode |
| S15 Kept between runs | S15.1, .2, .4, .7, .8, .9 | Shared; the parts `print` shows |
| S16 Colour | S16.1–S16.4, on a terminal and piped | Shared; added after the build (F35) |
| S3 Two markets | S3.1–S3.5, S3.7 | Shared; any mode |
| S4 Groups | S4.4 | Shared; any refresh |
| S11 Config file | S11.1, .2, .5, .7, .8, .9, .12, .15, .16, .17 | Shared; "when the tool starts" or "in any mode" |
| S12 No config yet | S12.1–S12.5 (all) | Shared; any mode |
| S17 Replaceable source | S17.1–S17.3 (all) | Shared; any mode |
| Workflows | The not-found row (6.5, 7.3) | No numbered criterion; tested as TestW6_5_NotFound_* |
| Quality | QR3, QR4, QR5, QR7, QR8 | QR4 without the view; QR8 through repeated `print` |

## Excluded, from stories that are partly in

| Criterion | Why it is out | Where it goes |
|---|---|---|
| S3.6 | "On screen": the view's rows | Slice 2 |
| S4.1–S4.3 | Group headers "when the view is drawn"; `print`'s headers are S13.8 | Slice 2 |
| S11.3, .4, .6, .10, .11, .14, .19 | Need the view open while the file changes | Slice 3 |
| S11.13, S11.18 | 25 rows and the warning "in the status line" | Slice 2 |
| S15.3 | "The view has run for eight hours"; QR8 measures the same through `print` | Slice 3 |
| S15.5 | "When the view runs"; `print`'s form is S15.8 and S15.9 | Slice 2 |
| S15.6 | "When the view is drawn" | Slice 2 |

## Increments (proposed, not yet in the skill)

Backfilled: the slice was built as one increment, in three batches.

| # | Criteria | Tests | Code | Verified |
|---|---|---|---|---|
| 1 | All of the above | `print_acceptance_test.go` (S13.1–S13.21, part of S14, S15), `print_shared_acceptance_test.go` (the rest, then S16) | 35fba13, f842376, 9a184e1, eb38ce5, 5e24254 | `verification/` (F32, F34); 8-hour QR8 run pending |

## Scope check (fresh agent, 2026-11-24)

The agent read only the spec and this file (without the coordinator's own notes) and went through every criterion. Summary of its report, with the coordinator's sorting.

**Not in scope, but `print` can show them** (the spec words them for the view; 7.4 says the table is formatted "as in the view", and 6.3 and 6.5 apply the same rules to one-shot calls):

- Row formatting: S1.5, S1.7, S1.8, S1.10–S1.15. Summary line: S2.1–S2.3, S2.6–S2.8. Currency: S3.6. Groups: S4.2, S4.3 (S4.1 is S13.8). Rows and warning: S9.1, S11.13, S11.18.
- Stale and failure marks: S5.6, S5.10, S5.12, S6.5, S6.6, S6.8–S6.10.
- `--refresh` forms: S7.2, S7.6–S7.8; S7.5 unclear. Not-found rows: S8.1–S8.5 (in scope today only as the unnumbered workflow row).
- No data after a ticker returns: S15.6. Traffic record: QR12.
- On Q62 (open, close, holidays): S5.8, S5.11.

Sorting: **Q71**. Whether a view-worded criterion with a `print` form belongs to this slice is what the spec does not say; surface tags would say it. Until answered, these stay out, and the Excluded table's reasons for S3.6, S4.2, S4.3, S11.13, S11.18 and S15.6 are marked weak.

**In scope, but only partly shown:** S11.7, S11.8, S12.3 and QR7 ("any mode"); S16.3 ("view or one-shot"); QR4 and QR5 (measured with the view open). Sorting: expected; the view half is slice 2 or 3 under Q71. S17.1: needs the real source; already a hand check. S13.5, S13.12, S13.14: on Q62, already open.

**No place in any slice:** QR1 (20 tickers across every criterion), QR9 (review before publishing), QR10 (the same check as S17.1). Section 7.1 has no criterion for `--version`, an unknown flag (status 2, usage on stderr), or the source address being absent from `--help`. Sorting: **Q72** and **Q73**.

**Doubled:** S15.3 is out while QR8, the same measurement, is in. Sorting: no question; S15.3 is the view form and stays in slice 3. S16 is in slice 1 and slice 2: part of Q71.
