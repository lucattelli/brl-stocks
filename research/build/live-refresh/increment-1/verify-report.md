# Verifier's report: live refresh, increment 1

Returned 2026-11-25 for the verifier section of `briefs.md` (a fresh agent). Summary of the agent's report:

- One full ordinary run: 166 top-level checks pass, 0 fail, 12 skipped (SLOW and LONG), 3 min 39 s. New file `verification/refresh_increment1_verify_test.go`: 8 ordinary checks and 5 SLOW or LONG. Commit d26667e. Two stand-in controls added (`opens_at_unix`, `quote_before_close_seconds`); existing ones unchanged.
- The real view on a pseudo-terminal, in real time, at the 10-second minimum interval. Measured: S5.1 (scaled to 100 s) 10 requests per B3 row; S5.2 (scaled to 20 s for 100 s) 5 per row; S5.3 (60 s) no request for AAPL, MSFT or ^GSPC while the B3 control row had 5; S5.7 B3 rows requested 0.4 s after the session started, no key; S5.8 requested again at +10 s and +20 s; S5.10 shortest gap 10.004 s; S8.3 partly, 1 request in 100 s. Shown again while open: S4.4 (PETR4 in three places, 10 requests, as VALE3), S5.4, S5.5.
- QR6 (3 min, 18 samples, 20 tickers): no stale mark; prices changed on screen. QR11: a key changed the screen in 1 to 7 ms while 8 requests of a heartbeat refresh were held. S15.3 scaled (3 min, 23 entries, quotes changing on every answer): 22101 bytes after the first refresh, then −0.3% to +1.0%.
- Observed outside the criteria: with B3 closed 60 s ago and the quote time exactly at the close, the rows are still requested every interval (a quote at the close does not end the period after the close; Q62 open). The heartbeat drifts about 7 ms per interval, each refresh timed from the end of the last. The status line warns `refresh_seconds is below 10; using 10`.
- Not run: the literal 10-minute S5.1, S5.2 and QR6 (`BRL_VERIFY_SLOW=1`); the 3-hour S8.3 and the 8-hour QR8/S15.3 with the view (`BRL_VERIFY_LONG=1 … -run TestVerifyLong -timeout 9h`). Hand check in `verification/README.md`.

## Coordinator's check and sorting

- Rerun of all ordinary checks by the coordinator: pass (241 s). `gofmt` and `go vet -tags verification` clean. Acceptance tests pass with `-race` (39 s).
- No fault.
- A quote at exactly the close does not end the 30 minutes after the close: the design's rule 7 and the recognition of the closing quote (C7) wait on Q62. As designed; Q62 will settle it.
- The drift and the warning follow the design and D50 (the 10-second floor and its warning). No question.

## Hand check

Leave `brl-stocks` open with `refresh_seconds = 10` against the stand-in (commands in `verification/README.md`): the quote times move with no `!` mark; switch B3 closed and back open through the stand-in's `_control`; the requests stop and resume with no key pressed. On the real source, during B3 hours: leave it open for a few minutes and watch the quote times advance.
