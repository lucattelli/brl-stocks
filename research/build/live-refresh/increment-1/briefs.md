# Briefs: live refresh (slice 3), increment 1

Written on 2026-11-25. Each role is a fresh agent. Its brief is the increment-1 brief of the same role in slice 2 (`../../full-screen-view/increment-1/`), read in full, with the changes below; where they differ, this file wins. Later increments of this slice resume the same agents with a short message, or, in a new session, start fresh agents from this file.

## Test writer

Work only in the worktree /home/lucattelli/Programming/prototyping/brl-stocks-wt-tests (branch slice3/1-tests). Commit there.

Your brief is research/build/full-screen-view/increment-1/test-brief.md: read it and follow its reading limits, rules and return format, with these changes:

- **The criteria:** increment 1 of research/build/live-refresh/scope.md: S5.1, S5.2, S5.3, S5.7, S5.8, S5.10, S8.3, S15.3 (8), through the view left open while time passes. Also, one test each, named `TestS<id>_ViewOpen_...`, for the criteria that scope lists as shown again in increment 1 (S4.4, S6.7, S5.4, S5.5, S5.12), each showing the criterion's result after time has passed with the view open, not at start.
- **Read also:** research/build/live-refresh/scope.md (its notes for the briefs); in docs/design.md, the whole section "The view's test boundary", especially "Time"; internal/deps/deps.go (`Terminal.Clock`, `deps.Clock`, `Input.Elapse`, `Frame.Now`). You may read the earlier view test files (internal/app/view_acceptance_test.go, view_increment2–6_acceptance_test.go) and reuse their helpers.
- **Write:** one NEW file internal/app/refresh_increment1_acceptance_test.go. Change no other test file.
- **Time:** make a `deps.Clock`, give it to the view through `Terminal.Clock` and to your stand-in source, and let time pass only with `Input{Elapse: d}`. The source stamps each request with the clock's `Now` and answers at once (it never waits on the clock). Never sleep. Count requests per instrument and their times from the source's own record. Every file you write while the clock is in use, the first config included, gets its modification time set to the clock's `Now`.
- **Against the current build** time does not move yet (the driver counts an Elapse but does not step the clock), so tests that need time to pass must fail on their assertions, not hang or panic. A test that passes now needs a reason and one shown failure on a changed copy outside the repository.
- **Commit** your file in one commit ("Slice 3, increment 1: acceptance tests for time passing (N)").

Return at most 250 words in your brief's format, plus every passing test with its shown failure.

## Builder

Work only in the worktree /home/lucattelli/Programming/prototyping/brl-stocks-wt-build (branch slice3/1-build). Commit there.

Your brief is research/build/full-screen-view/increment-1/build-brief.md: follow it, with these changes:

- **Read also:** research/build/live-refresh/scope.md; in docs/design.md, "The view's test boundary" ("Time" in full) and everything about the heartbeat, `Plan` and the refresh (sections 3 and 4).
- **Make pass:** internal/app/refresh_increment1_acceptance_test.go. Build:
  - the view's clock: with `Terminal.Clock` set, the view, the engine and the cache use only that clock, and the driver moves it on `Input{Elapse: d}` exactly as "Time" says (waits fire in order, each settles before the next, the Elapse is counted only at its end, `Frame.Now`);
  - the heartbeat and the refresh while the view is open (6.1, 6.2, 6.5), following `Plan`.
  - Leave `r` and watching the config file to later increments, unless a test of this increment needs them.
- Every earlier test must still pass; `print` must keep its output and its verification.
- **Commit** in one commit ("Slice 3, increment 1: build the view's clock and the heartbeat refresh").

Return at most 300 words in your brief's format.

## Verifier

Work only in the worktree /home/lucattelli/Programming/prototyping/brl-stocks-wt-verify (branch slice3/1-verify). Commit there.

Your brief is research/build/full-screen-view/increment-1/verify-brief.md: follow its reading limits, rules and return format, with these changes:

- **Criteria:** increment 1 of research/build/live-refresh/scope.md, through the real view on a pseudo-terminal in real time. Use the smallest refresh interval and the stand-in's session hours to keep runs short, and say in the feature map where a criterion's times were scaled down.
- **QR6:** sample the screen every 10 seconds for at least 3 minutes with a healthy stand-in and the smallest interval: no stale mark.
- **QR11:** a key changes the screen within 100 ms while a heartbeat refresh is running (a stand-in that answers after 5 seconds).
- **QR8 with the view:** add a LONG check (behind the existing switch, not run by you) for eight hours of the view open; and S15.3 in scaled-down form in the ordinary run.
- **Put** new checks in a NEW file verification/refresh_increment1_verify_test.go. Report what you observed outside the criteria.
- **Commit** in one commit ("Slice 3, increment 1: verify time passing").

Return at most 300 words.
