# Briefs: the full-screen view, increment 4

Written on 2026-11-25. The same agents are resumed with these messages; their increment-1 briefs still hold.

## Test writer

Increment 4 of research/build/full-screen-view/scope.md: S11.1, S11.5, S11.7–S11.9, S11.12, S11.13, S11.15–S11.18, S12.1–S12.5, S15.2, S15.5, S15.6, S15.8, S17.3 (21 criteria), through the view surface, in a NEW file internal/app/view_increment4_acceptance_test.go (reuse your helpers; edit no other test file). Read again 6.4, 7.2 (warnings in the view go to the status line), 7.7 (no config: the example watchlist, and the path named in the status line of the view; the cache folder; an unwritable cache: the view goes on fetching and warns once, D60). A config error at start ends the tool before the view opens (6.4, S11.9, S12.4): say how you observe that through the boundary. S15.6 is two runs with the config changed between them. Same rules: one test per criterion, red where the behaviour is missing, skip with a reason where the spec is ambiguous; show that a test passing against the current build can fail. Return at most 250 words in the same format.

## Builder

Increment 4: make internal/app/view_increment4_acceptance_test.go pass. Same rules as your increment-1 brief; return at most 250 words in the same format.

## Verifier

Increment 4: the same 21 criteria through the real view on the pseudo-terminal, in a NEW file verification/view_increment4_verify_test.go; feature-map rows in your view section of verification/README.md. Same rules; run all ordinary checks once; return at most 250 words in the same format.
