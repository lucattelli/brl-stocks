# Verify report: close, open and holidays (Q62), increment 1

The verifier's summary, unedited (2026-11-27):

> Close, open and holidays (Q62) has no failing check, but S5.17 could not be verified (see below). The full ordinary run took 7 min 33 s: 202 pass, 0 fail, 20 skipped. That is over the earlier 6-minute mark; this new file's ordinary checks take about 2 min 40 s. Committed as 3a22e3c on q62/1-verify.
>
> **Updated to D111** (not weakened):
> - **S5.3:** the US period ended 6 min before every answer (was 2 h). No US row requested in 60 s.
> - **S5.8:** B3 ended 60 min and 99 min ago (was 60 s with a quote before the close). Every B3 row was requested again every 10 s.
> - **S13.5:** B3 ended 101 min and 2 h ago, US ended 6 min ago. None of those rows was requested; the other market was.
>
> **New checks** (`verification/markethours_increment1_verify_test.go`, plus two stand-in controls, `period_from_unix` and `session_seconds`):
> - **S5.9:** on a holiday the status line read `US closed` throughout the usual hours.
> - **S5.11:** with pre-market prices 235.00 and 236.50 in the answer, the view and `print` both show the close, `US$ 229.87`.
> - **S5.13** (scaled to 60 s): no B3 row requested, nor ^BVSP.
> - **S5.14:** the status line showed `B3 closed` throughout, while the B3 rows were still refreshed.
> - **S5.15:** each US row was requested twice, at the open and 4 min 59 s later, then nothing until after the close. I made the session 10 min long so the close comes within the check (SLOW, run once).
> - **S5.16:** the second answer came at +5 m 59 s and `US open` showed on the next sample (SLOW, run once).
> - **S5.17: not verified.** It needs the next weekday's open, which is a day of real time. Hand check: on a real US holiday, leave the view open overnight against the real source and see the US rows requested within a minute of the next day's open.
>
> **Outside the criteria:**
> - With `print` called every minute across a holiday's open, the US rows were requested only by the calls at +30 s and +5 m 30 s.
> - During the settle time the status line says only `B3 closed`. Nothing tells the user the rows are still being refreshed.

## Coordinator's sorting

- **Faults:** none.
- **S5.17 (not verified by a check):**
  - **Covered by acceptance tests:** `TestS5_17_...` and `TestS5_7_Holiday_...` run a whole day of view time on the injected clock.
  - **Hand check:** at the next holiday, in `../hand-checks.md`.
- **`print` called every minute across a holiday's open:** two requests per row, as D112 allows.
- **`B3 closed` during the settle time, while rows still refresh:** D111 accepted this knowingly ("the market is shown as closed from the reported end"). Not a question.
- **Run time:** the ordinary run grew from about 6.5 to 7.5 minutes, within CI's 20-minute timeout.
- **My rerun:** the ordinary verification passes in 438 s.
