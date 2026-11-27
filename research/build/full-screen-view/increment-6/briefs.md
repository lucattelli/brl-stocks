# Briefs: the full-screen view, increment 6 (the review's answers)

Written on 2026-11-25. The agents of increments 1–5 cannot be resumed, so each role is a fresh agent: its brief is the increment-1 brief of that role (`../increment-1/`), read in full, plus the message below.

## Test writer

Work in the worktree /home/lucattelli/Programming/prototyping/brl-stocks-wt-tests (branch slice2/8-tests), not in the main checkout.

Your brief is research/build/full-screen-view/increment-1/test-brief.md: read it and follow it, with these changes:

- The criteria are increment 6 of research/build/full-screen-view/scope.md: S2.9, S2.10, S9.11, S12.6, S12.7, S12.8, S15.10, S18.5, S18.6 (new), and S9.6, S9.7, whose wording changed (they now scroll by one screen line, a row or a group header alike).
- Write the new tests in a NEW file internal/app/view_increment6_acceptance_test.go. Reuse the helpers of the earlier view test files (view_acceptance_test.go and view_increment2–5_acceptance_test.go); you may read those files.
- You may edit exactly two existing tests, TestS9_6_... and TestS9_7_... in view_increment5_acceptance_test.go, because their criteria changed: update their comments to the new text, and add a case with a group header. Change nothing else in any existing test file.
- S18.5 and S18.6 run without a Terminal: read the `IsTTY` paragraph of "The view's test boundary" in docs/design.md (you may read that section).
- The view is built (increments 1–5): some new tests may pass at once. Every test that passes against the current build needs a reason AND one shown failure of its own on a changed copy outside the repository (one given or one rule changed). List each.
- Commit your test files on your branch in one commit ("Slice 2, increment 6: acceptance tests for the review's answers (N)"); no other git command that changes history.

Return at most 250 words, in the format of your brief, plus the list of passing tests with their shown failures.

## Builder

Your brief is research/build/full-screen-view/increment-1/build-brief.md: read it and follow it, with these changes: the tests are internal/app/view_increment6_acceptance_test.go and the updated TestS9_6/TestS9_7; implement D92–D96 as the spec now states them (7.1, 7.3, S2, S9, S12, S15, S18) and the `IsTTY` paragraph of the view's test boundary in docs/design.md. Work in your own worktree, commit the build there in one commit. Return at most 250 words in the format of your brief.

## Verifier

Your brief is research/build/full-screen-view/increment-1/verify-brief.md: read it and follow it, with these changes: verify increment 6's criteria through the real view (S18.5 and S18.6 as plain commands with output redirected). Update the checks your predecessors wrote whose criteria changed (S9.6, S9.7; S12.2, whose check ran at 200 columns because the warning did not fit at 120; any check of the summary line or the status line at a narrow width), citing D92–D96, and their rows in verification/README.md. Report what you observed outside the criteria. Work in your own worktree, commit there. Return at most 300 words.
