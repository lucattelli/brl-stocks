# Verify report: live refresh (slice 3), increment 3

The verifier's summary, unedited:

> Increment 3 (`r`) has no failing check: all 8 criteria pass on the real view in real time. One ordinary run of all files took 6 min 15 s: 179 top-level checks pass, 0 fail, 18 skipped (SLOW and LONG). This increment's file runs in about 40 s. Committed as 2660196 on slice3/3-verify; the new checks are in `verification/refresh_increment3_verify_test.go`, and `verification/README.md` has a new section. `r` is always pressed more than 5 s after the rows' last attempt (the guard in 6.5).
>
> **Criteria:**
> - **S7.1:** B3 rows were requested 3 to 7 ms after `r`, at the default interval.
> - **S7.2** (scaled: one failure puts the next attempt 1 min away, not 4): every failed row was requested 3 to 4 ms after `r`.
> - **S7.3 and S7.4** (429 and 403): no request after `r`. The status line read `B3 unknown · US unknown · next attempt 18:50 · sort: file order`, 5 min after the refusal.
> - **S7.6:** with US closed 2 h ago, AAPL and MSFT were requested 1 ms after `r`.
> - **S7.7:** `r` pressed twice 2 s apart gave one request per row. Ten presses 50 ms apart and a held key (30 presses 30 ms apart) also gave one each.
> - **S7.8** (scaled to 7 s, not 10 min): the not-found XXXX9 was not requested after `r`; the other rows were.
> - **S7.9:** with 20 B3 rows answering after 900 ms, the last row was requested 1.8 s after `r`, well inside the 60 s interval.
>
> **Outside the criteria:**
> - **5-second answers fail in the view.** In all three runs the view gave up every heartbeat request at 5.0 s, with or without `r`. The rows went stale (`!16s`) under `source unreachable: 5 rows not updated`, then recovered on the next attempt one interval later. Answers after 4 s were taken. The spec sets no time limit for the view, but QR11 measures with a 5-second source, which the view treats as a failure. Someone should decide whether that is intended.
> - **`r` during a running heartbeat refresh** sends nothing and changes nothing on screen; no ticker was requested twice.
> - **The requests after `r` are spread out:** for the 20 rows they arrived over 1.8 s, not all at once.
>
> **Not verified:**
> - S7.2's real 4-minute wait (it needs three failures at the default interval) and S7.8's 10 minutes.
> - How `r` looks on a real terminal. Hand check: press `r` in the README's view setup and watch the stand-in's PETR4 request count go up by one. Press `r` again at once and it stays the same. During a 429 block, the count stays the same and the status line shows the next attempt.

## Coordinator's sorting

- **No fault.**
- **5-second answers fail:** not a fault. Design C16 sets 5 seconds per request (`requestTimeout`), marked as a guess and a risk accepted; a timeout is a network error (P29, D50). QR11 still holds (keys answer), but its 5-second stand-in sits exactly on that limit. Q96.
- **`r` during a running heartbeat refresh sends nothing at once:** as built, the press is kept and a forced refresh follows the running one; the rows just tried are skipped by the 5-second guard, so nothing new is sent. Q95.
- **Requests spread over 1.8 s:** the 8 requests in parallel of C16, with answers after 900 ms. Within S7.9.
- **Not verified, routed:** S7.2's four minutes and S7.8's ten minutes are covered at full length in view time by TestS7_2 and TestS7_8 (acceptance); `r` on a real terminal is a hand check (above), gathered for the slice's closing PR.
