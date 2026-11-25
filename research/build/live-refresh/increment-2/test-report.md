# Test writer's report: live refresh, increment 2

Returned 2026-11-25 for the test-writer section of `briefs.md` (the increment-1 writer, resumed). Summary of the agent's report:

- 13 tests in `internal/app/refresh_increment2_acceptance_test.go` (commit c779983): S6.4, S6.5, S6.6, S7.5, S7.10, and `ViewOpen` tests for S6.1, S6.2, S6.3, S6.8, S6.9, S6.10, S6.11, S8.2, each failing on the heartbeat refresh at 61 s, not at start; several with 20 tickers too. The package passes with `-race` in 54 s; vet clean.
- All 13 pass on the increment-1 build: its heartbeat already applies the waits after failures, blocks and recovery. Each shown failing on a changed copy of the test file outside the repository, one given changed: S6.4 (PETR4 refused, gaps of 5 min not 1); S6.5, S6.6 (VALE3 keeps failing); S7.5 (unreachable, gaps of 1, 2… min, not 5); S7.10 (stopping at 4 min); S6.1 (new prices); S6.2 (`!6m`, not `!5m`); S6.3 (nothing fails); S6.8 (PETR4 stale too); S6.9 (wrong cause); S6.10 (`!13m`); S6.11 (no error line); S8.2 (the old price).
- Added beyond the criteria: gaps between attempts may be up to 1 s longer (the heartbeat); in S7.5 requests less than 1 s apart count as one attempt; each test first checks that the refresh it relies on ran.
- Seen outside the criteria: when only one ticker is refused while the others answer, the block restarts at 5 minutes every time instead of doubling.

## Coordinator's check and sorting

- Checked: one new file; `go vet` and `gofmt` clean; rerun with `-race`: all 13 pass. Spot check in the product: with the doubling in `FailureWait` removed, S6.4 fails; restored.
- The added expectations follow the heartbeat of the boundary ("Time") or establish givens. Kept.
- A block that restarts at 5 minutes when one ticker is refused after the others answered: 6.3 says the block ends when "the wait ends and the source answers", and a later refusal starts a new block with a first wait of 5 minutes. The build follows that text. Whether "the source answers" means the probe or the whole round is not decided: Q94, asked with this increment, recommended as built.
