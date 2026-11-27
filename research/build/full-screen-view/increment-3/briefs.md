# Briefs: the full-screen view, increment 3

Written on 2026-11-25. The same agents are resumed with these messages; their increment-1 briefs still hold.

## Test writer

Increment 3 of research/build/full-screen-view/scope.md: S5.4–S5.6, S5.12, S6.1–S6.3, S6.7–S6.11, S8.1, S8.2, S8.5 (15 criteria), through the view surface, in a NEW file internal/app/view_increment3_acceptance_test.go (reuse your helpers; edit no other test file). Read again 6.2, 6.3, 7.3 (status line, stale row, not found, error line, which now counts rows only and says `summary not updated` when only summary entries failed, D81) and the scope's notes (S6.7 and S6.10 start from a cache an earlier run left, with a source that holds its answers). "When the view is drawn" after a failure: the idle frame after the fetch at start. Same rules: one test per criterion, red where the behaviour is missing, skip with a reason where the spec is ambiguous; if a test passes against the current build, show it can fail (say how). Return at most 250 words in the same format.

## Builder

Increment 3: make internal/app/view_increment3_acceptance_test.go pass. Same rules as your increment-1 brief; return at most 250 words in the same format.

## Verifier

Increment 3: the same 15 criteria through the real view on the pseudo-terminal, in a NEW file verification/view_increment3_verify_test.go; QR1 with 20 tickers for the failure checks; feature-map rows in your view section of verification/README.md. Same rules; run all ordinary checks once; return at most 250 words in the same format.
