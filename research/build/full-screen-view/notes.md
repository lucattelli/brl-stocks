# Build notes: slice 2

Notes for extracting a `build-increment` skill after this slice: where a decision was needed that no skill gave, what went into each PR, and what was awkward. Short entries, in order.

## 2026-11-24, night: setting up

- The user asked for the whole slice as a stack of PRs to review the next morning. Decisions that are the user's (the boundary change to the design, the emulator library of D86, spec questions) cannot wait for answers, so each PR builds on the recommended answer, marked "pending your approval", and the PRs above it are reworked if one is rejected. Spec questions are not recorded as decided; a test that needs one is skipped with its reason. A skill needs a rule for this: **work ahead on recommendations, never record them.**
- Stacked PRs: each branch starts from the one below and its PR targets it, so each PR shows only its own change. When the bottom one merges, the next is retargeted to `main`.
- The increments were planned in `scope.md` with D85's rules and checked by the same fresh agent as the scope. Planning took one pass; the count per increment (29, 28, 27, 11) came from grouping by setup, not by story.
- The test boundary had to change before any test: the view's edge did not exist. `story-to-tests` stage 1 says a boundary change needs approval but not how to make one; done by hand plus one fresh critic, not with `design-alternatives` (too large for one edge).
- Implementation is done by a fresh builder agent per increment, which reads the spec, the design and the tests and must not edit the tests. No skill covers this yet; it keeps the coordinator's context for sorting findings.
- The boundary critic found eight real gaps in a first draft that looked complete: no way to know the view had settled (so "nothing changes" could not be tested without sleeping), unstated concurrency of the test driver, colour not deterministic under tests, key names that were the library's, no lifecycle for the input channel, the frame's shape. A boundary for an interactive surface needs a **settling signal** in its output; a skill should ask for it.
- Two critic findings were spec questions, not design (left/right arrows; header lines when scrolling). They go to the user as Q84, Q85 in PR 0; tests that need them are skipped.
