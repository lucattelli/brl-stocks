# Briefs: slice 4's review answers (D108–D110)

Written on 2026-11-26. The same agents as increment 1, resumed.

## Test writer

Worktree /home/lucattelli/Programming/prototyping/brl-stocks-wt-tests (branch slice4/3-tests). The spec changed for D108–D110: S10.20 reworded (`TODAY` starts where the sparklines start, as your test already asserts), S10.23 new (a price exactly halfway between two levels takes the higher one), QR8 and S15.3 reworded (the cache is compared with its size after the first refresh in which every row has its full sparkline). Add S10.23's test to internal/app/sparkline_increment1_acceptance_test.go; update the comment of S10.20's test to the new wording. Read S15.3's test (slice 3, increment 1) and say whether its baseline already meets the new wording (every row has ten points at the first refresh); change it only if it does not. S10.23 passes against the build (built in PR #19): show it failing on a changed copy outside the repository. Commit in one commit ("Slice 4 review: S10.23, S10.20 and S15.3 follow D108-D110"). Return at most 150 words.

## Verifier

Worktree /home/lucattelli/Programming/prototyping/brl-stocks-wt-verify (branch slice4/3-verify). The spec changed for D108–D110 (see above). In verification/sparkline_increment1_verify_test.go, assert S10.23 (the halfway price you recorded) instead of only recording it; S10.20 now names `TODAY`'s start. Check the QR8 LONG check and any S15.3 check against the new wording (the baseline is the first refresh in which every row has its full sparkline); change them only if they measure from an earlier refresh with short series. Update the README's feature map. gofmt; run the new file and the ordinary checks once. Commit in one commit ("Slice 4 review: verify S10.23; QR8 baseline per D108"). Return at most 150 words.
