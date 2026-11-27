# Briefs: close, open and holidays (Q62), increment 1

Written on 2026-11-27. The agents of slice 4 are resumed with these messages. Their first briefs still hold: slice 2's, slice 3's in `../../live-refresh/increment-1/briefs.md`, and slice 4's in `../../sparkline/increment-1/briefs.md`. In a new session, start fresh agents from those briefs plus this file.

## Test writer

Worktree /home/lucattelli/Programming/prototyping/brl-stocks-wt-tests (branch q62/1-tests). A new increment, by research/build/market-hours/scope.md: close, open and holidays (D111–D113). Reread spec 6.1, section 5's constraints, and S5 and S13.5: they changed today.
- **New criteria:** S5.13–S5.17.
- **Waiting until now:** S5.9 (view and print) and S5.11 (view and print).
- **Reworded:** S5.3 (US, 5 minutes), S5.8 (B3, 100 minutes, every B3 row), S13.5 (the settle time of 6.1). Change their existing tests (internal/app/refresh_increment1_acceptance_test.go, internal/app/print_acceptance_test.go) to the new wording. These are the only edits allowed in existing test files, and each needs its own shown failure against the new number, e.g. passing with 30 minutes where 100 are required.
- **Holidays:** at the usual open the stand-in keeps answering with the last session's trading period (F43). The usual open is the last session's start, at the same local time on the next weekday.
- **Also:** S5.7 and S5.5 shown again with a holiday in the way, named `TestS5_7_Holiday_...` and `TestS5_5_Holiday_...`.
- **File:** a NEW file, internal/app/markethours_increment1_acceptance_test.go.
- **Same rules as before:** never sleep; never guess (skip with the reason and report it); every test that passes before the build needs a reason and one shown failure on a changed copy outside the repository.
- Commit in one commit ("Q62, increment 1: acceptance tests for close, open and holidays (N)"). Return at most 250 words.

## Builder

Worktree /home/lucattelli/Programming/prototyping/brl-stocks-wt-build (branch q62/1-build). Make the new and changed tests pass: D111–D113, spec 6.1, and design rules 6 and 7 of `Plan` and `MarketState` (docs/design.md, as changed today). The provisional numbers are in internal/market/plan.go.
- **Same rules as before:** never edit a test; stop on any disagreement; list every choice the spec or design left you.
- **Checks:** gofmt, vet, `go test ./... -race -count=1` twice, and the ordinary verification (`-tags verification`), which still passes; say the suite's time.
- Commit in one commit ("Q62, increment 1: build close, open and holidays"). Return at most 250 words.

## Verifier

Worktree /home/lucattelli/Programming/prototyping/brl-stocks-wt-verify (branch q62/1-verify). Through the real build and its stand-in:
- **The criteria:** S5.3, S5.8, S5.9, S5.11, S5.13–S5.17 and S13.5, as reworded today.
- **Real time:** the real build runs on the real clock, so a stand-in answer whose trading period ended a chosen time ago gives the settle cases. The holiday cases come from an answer with yesterday's period at an expected open you place minutes ahead.
- **Changed checks:** update your existing S5.3, S5.8 and S13.5 checks to the new wording (a spec change, D111). Don't weaken them.
- **Outside the criteria:** `print` called every minute across an expected open on a holiday, with the requests counted; the status line during the settle time.
- **Ordinary checks:** run them all once at the end. Commit in one commit ("Q62, increment 1: verify close, open and holidays"). Return at most 300 words.
