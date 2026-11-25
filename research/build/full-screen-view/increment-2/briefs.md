# Briefs: the full-screen view, increment 2

Written on 2026-11-25. The same agents as increment 1 are resumed with these messages; their increment-1 briefs still hold (same reading limits, same rules, same return format).

## Test writer

Increment 2 of research/build/full-screen-view/scope.md: S2.1–S2.3, S2.5–S2.8, S4.1–S4.4, S9.1 (12 criteria), through the view surface, in a NEW file internal/app/view_increment2_acceptance_test.go (reuse your helpers from view_acceptance_test.go; do not edit that file or any other test file except to export nothing: put new helpers in the new file). Same rules as your increment-1 brief: one test per criterion named TestS<id>_View_..., the criterion's text as the comment, red against the current build where the behaviour is missing, skip with a reason where the spec is ambiguous. Note: the view already shows a summary line and rows in file order from increment 1, so some of these may pass now; say which and why. Return at most 250 words in the same format.

## Builder

Increment 2: make internal/app/view_increment2_acceptance_test.go pass (summary line per the config's `summary` key, group headers, file order, a ticker in two groups fetched once). Same rules as your increment-1 brief: read the spec, the design and the code; never edit a test; stop and report on any disagreement; all tests under `-race`; return at most 250 words in the same format.

## Verifier

Increment 2: the same 12 criteria through the real view on the pseudo-terminal, in a NEW file verification/view_increment2_verify_test.go, reusing your helpers; QR1 with 20 tickers for the group checks; then the feature-map rows for these criteria in your view section of verification/README.md. Same rules as your increment-1 brief; run all ordinary checks once; return at most 250 words in the same format.
