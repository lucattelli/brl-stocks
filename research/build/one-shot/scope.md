# Scope: the one-shot mode

Rewritten 2026-11-24 from the spec's surface tags (D78). The slice is every criterion tagged `print`: 128 of 164 (37 `print` only, 91 `view, print`). Increment 1 was built before the tags existed and scoped by story; increment 2 is what the tags added. The first version of this file, rebuilt by hand from the log, and its scope check led to the tags; see the log (D75–D78).

## Entry points

- `brl-stocks print`, as a table (7.4)
- `brl-stocks print --json` (7.5)
- `brl-stocks print --refresh`, with either output
- `--config <path>`, `--help` and `--version` with `print`, and an unknown flag with `print` (7.1, D77)

View-worded criteria read on `print` as section 8's reading table says.

## Increment 1: done (F32–F35)

62 criteria, each with a test named after it.

| Test file | Criteria |
|---|---|
| `internal/app/print_acceptance_test.go` | S13.1–S13.21; S14.1, S14.4–S14.7, S14.12; S15.1, S15.4, S15.7, S15.9 |
| `internal/app/print_shared_acceptance_test.go` | S3.1–S3.5, S3.7; S4.4; S11.1, .2, .5, .7, .8, .9, .12, .15, .16, .17; S12.1–S12.5; S13.22–S13.24; S16.1–S16.4; S17.2, S17.3 |

Also: the not-found row (TestW6_5_NotFound_*), now numbered as S8.1, S8.2 and S8.5; zero change has no colour (TestW7_3_*).

## Increment 2: done (F36, F37)

63 criteria with no test, to be written now; two more wait on Q62 (below). Where an increment-1 test already shows the same thing through another criterion, it is named: the new test is still written, one per criterion, so a failure names the requirement.

| Group | Criteria | Already shown in substance by |
|---|---|---|
| Row columns and number formats | S1.2, S1.5, S1.7 (D75), S1.8–S1.15 | — (S13.17 and S13.18 show two prices with no thousands separator) |
| Summary line | S2.1, S2.2, S2.3, S2.5–S2.8 | Partly: S13.13 (S2.1), S13.19 (S2.5) |
| Currency | S3.6 | S13.17, S13.18 |
| Groups and order | S4.1, S4.2, S4.3, S9.1 | S13.8 (S4.1) |
| Closed markets and stale marks | S5.4, S5.5, S5.6, S5.12 | S13.12 (S5.4), S13.14 (S5.5) |
| Interval floor | S5.10 | — |
| Failed fetches | S6.1, S6.2, S6.3, S6.5, S6.6, S6.8, S6.9 (D76, D81), S6.10, S6.11 (D81, added after verification) | S13.6 (S6.1, S6.2), S13.15 (S6.3) |
| Forced fetch (`--refresh`) | S7.1, S7.2, S7.3, S7.4 (D76), S7.6, S7.7, S7.8 | Partly S13.4 (S7.1, without its 2 seconds); S13.10 (S7.3) |
| Not-found rows | S8.1, S8.2, S8.4, S8.5 | TestW6_5_NotFound_* (S8.1, S8.2, S8.5; not S8.4) |
| More than 20 tickers | S11.13, S11.18 | — |
| JSON | S14.2, S14.3, S14.8, S14.9, S14.10, S14.11 | — |
| Kept files | S15.2, S15.6, S15.8 | — |
| Help text and command line | S17.4, S18.1–S18.4 (D77) | — |

Notes for the brief, from the scope check:
- S5.10 on `print`: `brl-stocks print` called repeatedly (every 2 seconds) with `refresh_seconds = 3`; no row is requested twice less than 10 seconds apart. The warning for a value under 10 belongs to 7.2, not to this criterion.
- S6.5, S6.6 and S8.4 take two calls: the first sets the given (a failure, a not-found row), the second is the "when". On `print`, S6.6's "the error line is gone" is no cause line on the error stream of the second call.
- S8.4: the config change is a change of content, not a save of the same bytes.

### Waiting on Q62

| Criterion | Why |
|---|---|
| S5.9 (holiday shown as closed) | Rests on F14, unverified (spec section 12). A stand-in can only send a closed trading period, which tests nothing about holidays |
| S5.11 (pre-market price not shown) | What the source sends before the open is F28, one sample, and Q62 is open; the stand-in's answer would be a guess |

## Not by an acceptance test

| Criterion | Route |
|---|---|
| S17.1 (a fresh machine, no key, shows quotes) | Needs the real source: a hand check, `brl-stocks print` on a clean `HOME` with only a config file (already in `verification/README.md`) |

## Quality requirements tagged `print`

QR1, QR3, QR4, QR5, QR7, QR8, QR10, QR12. Increment 1 covers QR3, QR7 and QR8 (F32, F34), and QR4 and QR5 only in part: they are measured with the view open, and only `print` calls were run. Open: QR1 (every `print` test with 20 tickers), QR10 (by S17.1), QR12 (the traffic of a `print` run): for verification in increment 2.

## Scope check

Run on 2026-11-24 by a fresh agent with the spec, the entry points and this file. Coverage: all 128 ids once, none view-only. Its other findings are applied above: S5.9 and S5.11 moved to "Waiting on Q62"; the readings of S5.10, S6.5, S6.6 and S8.4 written as notes for the brief; four wrong "already shown" claims corrected; QR4 and QR5 marked partial.
