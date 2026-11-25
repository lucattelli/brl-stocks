# Test writer's report: live refresh, increment 1

Returned 2026-11-25 for the test-writer section of `briefs.md` (a fresh agent). Summary of the agent's report:

- 13 tests in `internal/app/refresh_increment1_acceptance_test.go` (commit 5007e76): one per criterion (S5.1, S5.2, S5.3, S5.7, S5.8, S5.10, S8.3, S15.3) and five `TestS<id>_ViewOpen_` (S4.4, S6.7, S5.4, S5.5, S5.12). S5.1, S5.2, S5.3 and S5.10 also with 20 tickers. All 13 fail on their assertions in about 0.5 s; `go vet` and `gofmt` clean; every earlier test passes with `-race`.
- S5.3, S5.10 and S15.3 fail now on the given that time passed; their own assertions cannot be shown failing until the build moves time.
- Readings: S6.7, a row can go stale by age only when its market's next session starts while the view is open, so the source holds its answers through that Elapse and the test reads a frame drawn during it (`Frame.Now` at or after the session start). S5.5 needs a market to close while the view is open; the test uses Friday 2026-11-27, when the stand-in's US session ends at 18:00 UTC. S15.3 uses one long session reported by the stand-in. S5.7's next session is the same hours on the next weekday (scope notes). S5.1 counts the fetch at start.
- Added beyond the criteria: S5.3 checks `Frame.Now` shows the full 10 minutes; S5.10 and S15.3 require each row requested at least twice; S4.4 counts requests over 90 s; S5.4, S5.5, S5.7, S5.8, S5.12 and S6.7 check the market states in the status line as their given.
- Boundary: enough for every test.

## Coordinator's check and sorting

- Checked: one new file; `go vet` and `gofmt` clean; rerun with `-race`: the same 13 fail.
- The readings follow 6.1, 6.2 and the scope's notes. No question.
- The added expectations establish givens (time passed, the market's state) or guard a vacuous pass. Kept.
- S5.3, S5.10 and S15.3: once built, the coordinator shows each failing on its own assertion by breaking the rule it depends on.
