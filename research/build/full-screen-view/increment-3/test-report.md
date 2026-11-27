# Test writer's report: the full-screen view, increment 3

Returned 2026-11-25. Summary of the agent's report (its full text is in the session): 15 tests, one per criterion, in `internal/app/view_increment3_acceptance_test.go`; all 15 pass against the current build. The writer showed 14 of them failing on changed copies outside the repo (one given changed at a time: a failing AAPL for S5.4, an open US market for S5.5, a successful fetch before the close for S5.12, new source prices for S6.1, a 3-minute cache for S6.2, no failing row for S6.3, a 90 s cache for S6.7, a second failing row for S6.8, a 120 s wait for S6.9, an 11-minute cache for S6.10, no failing summary entry for S6.11, a known XXXX9 for S8.1, a failing PETR4 for S8.2, a failing S&P 500 for S8.5). QR1: S6.1, S6.3, S6.8, S6.9 also with 20 tickers. Readings: S5.4 "the last close" is the price reported with a closed session; S6.9 next attempt = failure time + one interval (6.2); S6.3 one line naming a cause; S5.6 requires that the view does not fetch the closed market's rows again (6.1). Added: S8.1 checks "not found" ends where prices end. Noticed: with a cache younger than one interval the view sends no request at start.

## Coordinator's check and sorting

- S5.6, the one not shown failing by the writer: the coordinator changed the stale rule so a closed market's rows go stale by age; S5.6 fails. All 15 shown able to fail.
- The readings follow 6.1, 6.2 and 7.3. No question.
- No request at start with a young cache: the view's fetch at start follows the same rules as any refresh (6.5, the design's `Plan`). As designed.
- No build needed: increment 1's view and the shared formatter already do all of it.
