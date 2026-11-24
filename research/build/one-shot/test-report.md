# Test writer's report: the one-shot mode, second batch

Returned 2026-11-24 for the brief in `test-brief.md`. The agent's summary, unedited:

---

I wrote 30 tests in one new file, /home/lucattelli/Programming/prototyping/brl-stocks/internal/app/print_shared_acceptance_test.go. 27 pass, 1 fails and 2 are skipped. `go vet` passes. The whole package runs with `-race` in about 0.3 s, with no panic and no hang.

**Failing**
- `TestW6_5_NotFound_TriedLessThanAnHourAgoIsNotFetched` (6.5: "Not found, tried less than an hour ago, config unchanged | No fetch"). One `print` found XXXX9 not found. I moved the clock 30 minutes and ran `print` again with the config unchanged. Expected no request for XXXX9. Observed: `XXXX9, not found 30 minutes ago, requested 1 times, want 0`. The row still printed "not found".

**Skipped**
- S13.22 and S13.23. A call that waits for another call can only wait on `Deps.After`, so the test has to decide when time passes, and every choice I tried is a guess:
  - With no timer firing, the ten calls hang.
  - With 9 s let pass after the first call returns, that call printed quotes. The other nine returned within milliseconds with `—` in every column, although the cache was full by then. A few runs hung instead.
  - With 9 s let pass once every instrument had been requested, the results were mixed: hangs, some failures, some passes.

  So S13.22 may be a real fault, but this boundary can't settle it. You can rerun the tests under the second rule with `BRL_STOCKS_TRY_TOGETHER=1`. The ten calls start together through a goroutine barrier.

**Ambiguities**
- S11.16: the spec fixes no wording for "the problem". The test checks that the line naming the file also says something beyond the path.
- S13.23: "one request per row" doesn't say whether summary entries count. I counted only watchlist rows.

**Expectations beyond the criteria**
- The not-found row has exactly 6 `—`: name, change value, percent, low, high and time, from 7.3 and 7.4.
- "not found" ends at the same column as the prices, which is how I read "in place of the price".
- S17.3:
  - Every flag in `--help` is one of the 7.1 list.
  - `--source` and `--provider` exit with status 2.
  - The keys `source`, `provider` and `source_url` each draw an unknown-key warning.
- S11.15 adds a decoy file at `~/.config` to show that `XDG_CONFIG_HOME` wins, as 7.2 says it should.

**What the boundary lacks**
A way to model several processes waiting on each other in time, such as a clock that moves on its own or a signal when a call is waiting.

---

## Coordinator's sorting

- `TestW6_5_NotFound_TriedLessThanAnHourAgoIsNotFetched`: a fault. "Config unchanged" was decided by comparing the config file's modification time with the last attempt's time, so a file time ahead of the clock counted as a change. Fixed: the row now keeps the config's modification time at its last attempt and compares for equality.
- S13.22, "the other nine returned with `—` although the cache was full": a fault. When the wait's deadline passed, the call printed the snapshot from its last poll without reading the cache again. Fixed.
- S13.22 and S13.23 skipped: a gap in how the tests drive time, not in the boundary. Sent back to the same writer to use `testing/synctest` (standard library in Go 1.27), where fake time advances when every goroutine is blocked.
- S11.16 "the problem": the criterion only asks that the message names the problem; the test's reading (something beyond the path) adds no expectation. No question.
- S13.23 "one request per row": decided by the glossary, where a *Row* is one line of the watchlist (D9); summary entries are not rows. The test's reading is right. No question.
- Expectations beyond the criteria: the dash count and the column of "not found" follow from 7.3 and 7.4. The S17.3 extras (two invented flags exit with status 2, three invented keys warn) follow from 7.1 and S11.12. Kept.

## Follow-up: S13.22 and S13.23 (same writer, resumed)

The agent's summary, unedited:

> S13.22 and S13.23 now pass in all 50 runs each with `-count=50 -race`, and nothing is flaky. Every run sent exactly 1 request per row and per summary entry: PETR4, VALE3, AAPL, IBOV, SP500 and USDBRL. The `BRL_STOCKS_TRY_TOGETHER` switch is removed. Both tests run inside `synctest.Test`, with `Deps.Now = time.Now` and `Deps.After = time.After`, so the bubble's clock decides when waits end. A `time.Sleep` inside the bubble moves it to Monday 2000-01-03 15:00 UTC; the quotes' sessions are built around that moment, so B3, US and USD/BRL are all open. A barrier releases the ten `app.Run` calls at once. The stand-in holds each answer for 300 ms of bubble time; the ten calls took 400–600 ms of bubble time in total. I also ran 20 times with no delay on the source: all passed.

Coordinator's check: the package passes with `-race`; the two tests pass 20 more runs. With the D73 wait switched off in `app.go`, S13.22 fails in 5 of 5 runs, so the test is not vacuous. The wait was switched back on.

## Follow-up: colour on a terminal (S16), same writer, resumed

The agent's summary, unedited:

> I added five colour tests through `print`; one passes and four fail. The product prints no colour at all with IsTTY true, e.g. PETR4 on a terminal: `"PETR4   Petrobras PN    R$ 38,42   +0,31  +0,81%   37,73   38,80  11:45"`. S16.1 fails: `+0,31` and `+0,81%` carry no SGR 32 or 92. S16.2 fails: `-1.12` and `-0.48%` carry no SGR 31 or 91. S16.3 fails on its given: without NO_COLOR the terminal output has no colour either, so its absence with NO_COLOR proves nothing. S16.4 passes, piped and with NO_COLOR. TestW7_3_DayChange_ZeroHasNoColour fails on its given for the same reason. Three readings are mine: "palette green/red" means SGR 32/92 and 31/91; following 7.3, both the value and the percent must be coloured; "colour off" in S16.4 means piped, or NO_COLOR on a terminal.

Coordinator's sorting: a fault (F35); implemented, all five pass. The readings follow 7.3 (the terminal's own palette; value and percent) and S13.7 and S16.3 (when colour is off). No question.
