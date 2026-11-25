# Build notes: slice 3

What was awkward or new while building slice 3 with brl-factory 0.6.1 (`plan-slice`, `write-tests`, `build-increment`, `create-verification`), for the factory. Short entries.

- **Resuming agents across increments worked.** The test writer, builder and verifier were each resumed with a short message per increment, kept in `increment-<n>/briefs.md`. Each message was about 150 words, and the agents kept their helpers and their reading of the spec.
- **A subagent cannot always make its own worktree.** Twice the permission check refused `git worktree add` from a resumed agent (S7.2's fix, the wording change in increment 4). The coordinator made it, as it does for every first run. `build-increment` rule 7 could say plainly that the coordinator makes every worktree, including those for fixes.
- **Three test givens were tighter than the spec,** each caught by the builder or the coordinator rather than by a run against the product:
  - S5.8's one-second window;
  - S7.2's press inside the 5-second guard;
  - the full path where the spec says "names the file".

  The builder's "stop on any disagreement" did its job. A test-writer brief could ask for each given to be checked against the spec's state tables (6.2–6.5) before writing.
- **The builder's report caught a rule of the spec that no criterion tests:** the error line's order (7.3), where the builder had joined two problems. Prose in the spec's behaviour sections that has no criterion is easy to miss. The coordinator sends such rules to the verifier as extra checks, and could also ask the test writer.
- **Verification time grows with every slice.** The ordinary run went from about 4 to about 7 minutes, and CI's limit was raised from 10 to 20 minutes on #13. A slice with real-time checks should plan which of them go behind SLOW.
- **Real-time verification found the one number nobody had measured:** the 5-second request limit (Q96) sits exactly on QR11's 5-second stand-in. A quality requirement's measuring setup should be checked against the design's limits when the design is written.
- **Building on the recommendation needs a small extra round when the recommendation differs from what was built.** The invalid-config wording was reordered before the PR (rule 6), so a "yes" needs no rework.
