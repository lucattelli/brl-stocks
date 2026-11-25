# Test report: live refresh (slice 3), increment 3

The test writer's summary, unedited:

> I wrote 8 tests for increment 3 (S7.1, S7.2, S7.3, S7.4, S7.6, S7.7, S7.8, S7.9), one per criterion. Committed as 5fae9ea on slice3/3-tests: internal/app/refresh_increment3_acceptance_test.go. vet passes, nothing hangs, and every earlier test still passes.
>
> **Against this branch:** 7 fail on their own assertions or givens and S7.3 passes. S7.1 and S7.7 also run with 20 tickers.
> - S7.1, S7.2, S7.6, S7.9: nothing is requested after `r`.
> - S7.7 and S7.8: fail on their given check that `r` refreshed the rows.
> - S7.4: the status line during a block lacks the next-attempt time.
>
> **S7.3 passes because:** `r` is not built, and a block sends nothing until its 5-minute wait ends. On a changed copy outside the repository, the test watches 5 minutes after `r` instead of 1. It then fails: "1 requests were sent after r during a block".
>
> **How the givens are set up:**
> - S7.2: `r` is pressed 1 s after PETR4's third failure, so the next attempt is 239 s away.
> - S7.3/S7.4: the block starts on the heartbeat refresh, and `r` comes 30 s later.
> - S7.6: US has been closed since Friday, with no request in the minute before `r`.
> - S7.7: there are 2 s of view time between the presses.
> - S7.8: XXXX9 was tried exactly once, 10 minutes earlier.
>
> **Added beyond a criterion's text:**
> - S7.9 uses `refresh_seconds = 600`, presses `r` at 10 s, and checks requests before the automatic refresh at 600 s. That is 10 s stricter than "one interval". Any one-interval window contains an automatic refresh, so without this the test could not fail.
> - S7.8 checks that XXXX9 is not requested for 1 minute after `r`.
> - S7.1 and S7.2 count requests in [press, press+2 s].
>
> **Ambiguity:** I read "the first B3 rows" as at least one B3 row.

## Coordinator's check

- One new file, no other test file changed; gofmt and vet clean.
- Against the increment-2 build: 7 fail on assertions, S7.3 passes. Its shown failure was rerun by me on the changed copy: "1 requests were sent after r during a block (PETR4 at 2026-11-23 15:00:00)".
- Sorting: S7.9's window ending at the automatic refresh only guards against a vacuous pass (kept). S7.8's one minute after `r` follows from the criterion (no request for that ticker). "The first B3 rows" read as at least one, within 2 s: the criterion's own limit; no question.
