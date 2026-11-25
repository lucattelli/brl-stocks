# Briefs: the full-screen view, increment 5

Written on 2026-11-25. The same agents are resumed with these messages; their increment-1 briefs still hold.

## Test writer

Increment 5 of research/build/full-screen-view/scope.md: S1.6, S2.4, S9.2–S9.10 (11 criteria), plus QR11 (a key changes the screen within 100 ms while a fetch is running), through the view surface, in a NEW file internal/app/view_increment5_acceptance_test.go (reuse your helpers; edit no other test file). Read again 7.3: "Narrow terminal" (the order in which columns are dropped; the view follows a resize), the keys table, and the sort rules under it. Two points are open questions for the user and must not be guessed: Q84, whether the left and right arrows scroll, and Q85, whether scrolling by one row counts group header lines. Where a test needs either, test only what does not depend on it (for example, scroll with a config that has no groups), or skip with the reason "waits on Q84" or "waits on Q85". QR11: the frame after a key, while the source holds its answers, compared on the fake clock or with a real-time guard; say which. Same rules: one test per criterion, red where the behaviour is missing; show that a test passing against the current build can fail. Return at most 250 words in the same format.

## Builder

Increment 5: make internal/app/view_increment5_acceptance_test.go pass (narrow terminal, resize, sort, scroll, help, other keys). Same rules as your increment-1 brief; where a test is skipped as waiting on Q84 or Q85, build the recommended answer of PR #2 (left and right do nothing; one screen line per press) and say so. Return at most 250 words in the same format.

## Verifier

Increment 5: the same criteria and QR11 through the real view on the pseudo-terminal, with real resizes (TIOCSWINSZ and SIGWINCH) and real key bytes (arrow escape sequences, `j`, `k`, `s`, `?`), in a NEW file verification/view_increment5_verify_test.go; feature-map rows in your view section of verification/README.md. Same rules; run all ordinary checks once; return at most 250 words in the same format.
