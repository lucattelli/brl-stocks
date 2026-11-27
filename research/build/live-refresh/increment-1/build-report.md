# Builder's report: live refresh, increment 1

Returned 2026-11-25 for the builder section of `briefs.md` (a fresh agent). The agent's summary, unedited:

---

12 of the 13 new tests pass. One, S5.8, fails because it conflicts with the design and with an older test. I stopped on that point and did not guess. The work is committed as b982401 in /home/lucattelli/Programming/prototyping/brl-stocks-wt-build (branch slice3/1-build).

**Tests:** `go test ./... -race`: 246 top-level tests pass, 1 fails, none skipped. Every earlier test passes, `go vet` is clean, gofmt was run and `go mod tidy` changed nothing. The ordinary verification run (`-tags verification`) passes, so `print` still checks out. No dependencies were added.

**The S5.8 conflict:** The view's fetch at start answers at 20:10:00 on a clock that is not moving. S5.8 wants a request again within (20:10:00, 20:11:00]. The design says a row is due when its last success is "more than one interval old", so the next request comes on the 20:11:01 heartbeat. I tried changing that rule to "at least one interval" (and reverted it): S5.8 passed, but `TestS13_2_FreshCacheSendsNoRequest` failed (it expects no request for a cache fetched exactly 60 s ago). The spec, the design or one of the two tests has to decide which reading holds.

**What I built:**
- `internal/app/view.go`: when `Terminal.Clock` is set, the view, the engine and the cache lock use only that clock. The driver keeps the view's own waits itself rather than in running commands. On an Elapse it moves the clock from wait to wait in order and lets each one settle. The Elapse counts as an input only at its end, and every frame carries `Now`. The real program's heartbeat waits through `deps.After`.
- `internal/tui/tui.go`: a one-second heartbeat. Each beat asks `Plan`, using the rows the view holds in memory, whether anything is due, and starts a cycle only if so and none is running.
- `internal/market/plan.go`: S6.7 needs this change. `MarketOpen` now also counts a market as open during the expected next session (same hours on a later weekday), as spec 6.4 says ("the next session starts → Open"). This affects `print` too, and every print test still passes.

**Choices I made:**
- With no clock, the test driver runs no heartbeat, so slice 2 tests are unchanged. Their fake timer would otherwise fire heartbeats endlessly.
- Watching the cache file for changes from other processes is not built yet. QR4 needs it.
- A busy cache lock under the test clock would hang an Elapse, because the lock's retry waits on that clock. It cannot happen with only one process.

A real-time run of the built binary against the stand-in, under `script`, with `refresh_seconds = 10`, refreshed every 10 s with no key pressed.

---

## Coordinator's check and sorting

- **S5.8:** decided elsewhere. Spec 6.5 says a quote at most one interval old is not fetched, and the design's rule 6 follows it, so "when a refresh interval passes" is read as "by the first heartbeat after the interval". The same test writer widened the window to (t0+60s, t0+61s], citing 6.5 (commit 514390e). No change to the build, the design or the spec. No question.
- **A market open from its expected next session:** realises 6.1 (Closed → the next session starts → Open) and the design's "rows past their predicted open are due". Recorded in the design's rule 6 on this branch. Holidays still wait on Q62.
- **No heartbeat without a clock in the test driver:** keeps slice 2's tests unchanged, as "Time" says. Follows the design.
- **The view does not yet re-read the cache for other processes' fetches (C4):** no criterion of increment 1 needs it; QR4 (several processes) does. Noted in the scope for increment 2's build.
- **A busy lock under the test clock would hang an Elapse:** only with two processes, which acceptance tests do not run. Noted.
- **Checked:** no test file changed in the build commit; `go vet`, `gofmt` clean; `go test ./... -race -count=1` passes twice with S5.8's fix.
- **Found by the coordinator:** the app suite went from 9 s to 164 s with `-race`. A CPU profile showed the cost in the product: every draw reloaded time-zone files from disk per row (`time.LoadLocation` in `MarketOpen`) and rebuilt the wanted rows per row. The real view draws every second. Sent back to the same builder.
