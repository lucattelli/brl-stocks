# Verifier's report: live refresh, increment 2

Returned 2026-11-25 for the verifier section of `briefs.md` (the increment-1 verifier, resumed). Summary of the agent's report:

- One ordinary run of all files: 171 top-level checks pass, 0 fail, 18 skipped (SLOW and LONG), 5 min 51 s. New file `verification/refresh_increment2_verify_test.go`; a feature-map section in `verification/README.md`; the stand-in unchanged. Commit 2c6f04e.
- Through the real view on a pseudo-terminal in real time: S6.4 (scaled to the 10 s interval) a row failing with 503 retried after 10.01, 20.01, 40.03 and 80.05 s while PETR4 refreshed every 10 s; S6.5 and S6.6 stale marks and the error line gone 10.0 s after the source answered again; S7.5 (not scaled) the first attempt 5 min 0.2 s after the first refusal, the error line `source is rate-limiting: 5 rows not updated · next attempt 16:00`, the next attempt 10 minutes later after a second refusal; S7.10 every row requested again at +5 min 0.19 s with no key, then every 10 s, no error line or stale mark afterwards. Shown again on a failing heartbeat refresh (S6.1–S6.3, S6.8–S6.11, S8.2): pass; for example `source unreachable: 5 rows not updated · next attempt 15:55`, `!11s` at an age of 11.3 s, `summary not updated`.
- QR4 (scaled to 2 min at 10 s; the view open and `print` every 2 s): 276 requests, 12 per entry; limits 18 per entry and 414 in total. QR5 (scaled to 5 min 20 s): 6 requests after the first refusal, 5 from the refresh at +0 s and 1 at +5 min.
- Outside the criteria: with the source still refusing, one request when the wait ended; when it answered, all 8 entries within 10 ms. While the view was open, `print` never fetched (it read the cache). A view started on a refusing source with no cache shows `B3 unknown · US unknown`. Stale marks round down (`!2m` at 2 min 47 s).
- Not run: SLOW (S6.4 up to 320 s, S7.5 at 5 and 10 min, QR4 for 10 min, QR5 for 16.5 min) and LONG (S6.4 at 60 s up to the 1,800 s cap, S7.5's 5, 10, 20, 30 min). Hand check below.

## Coordinator's check and sorting

- Rerun of all ordinary checks by the coordinator: pass (378 s). `gofmt` and `go vet -tags verification` clean.
- No fault. QR4 shows the view sharing the work with `print` (the view's cache read of this increment). The single request when a wait ends is the probe of D98 and C13. `unknown` with no quote follows D74. Rounding the age down: 7.3 gives the units, not the rounding; the mark never claims a row is older than it is. Noted, not asked.
- The ordinary run is now close to 6 minutes, and CI with it; the SLOW and LONG switches keep the rest out.

## Hand check

In the README's view setup, switch the stand-in to refusing with `curl -X POST -d '{"status":429}' …/_control`, watch the error line show `source is rate-limiting` and a next attempt 5 minutes ahead, then switch it back with `'{}'`: the error line goes at the time it showed, with no key pressed.
