# Build report: close, open and holidays (Q62), increment 1

The builder's summary, unedited (2026-11-27):

> Close, open and holidays are built, and all 14 tests of this increment pass, as does everything else. Committed as 4498bc6 on q62/1-build. The only file changed is `internal/market/plan.go`.
>
> **What changed:**
> - **Settle time:** after the end the source reports, B3 rows keep being refreshed for 100 minutes, other markets for 5 (D111, D113).
> - **Usual open (rule 7):** a row is checked once at the usual open and once more 5 minutes later. It is then not checked again until the next weekday's usual open, so a holiday costs at most two requests per row (D112).
> - **Shown as open or closed (rule 6, D112):** a market counts as open from its expected next session only until a row fetched since that session started still carries the last session. A holiday is then shown as closed.
>
> **Choices the spec or design left me:**
> - **Inclusive times:** "since the open" includes the open itself, and the second check falls due from exactly 5 minutes after the open.
> - **One row is enough:** it marks the whole market closed for the holiday, while each row still makes its own two checks.
>
> **Checks:** vet and gofmt are clean. With -race the app package took 154.8 s and 148.5 s on two runs, about the same as before. The ordinary verification passes in 397 s.
>
> **Speed:** my change does not make it slower. S5.17 takes 39 s with -race and 9.5 s without. About 70% of its time is drawing the screen once per second of view time: 86,400 draws for 24 hours. I found no cheap product fix. Skipping the draw after a heartbeat breaks the boundary's rule of a frame after every update. Keeping the rendered screen until something changes depends on the clock (ages, market states, dates), so it is not small or safe.

## Coordinator's check and sorting

- **No test file changed:** the only file the commit touches is `internal/market/plan.go`.
- **My rerun:** `go test ./... -race -count=1` passes, the app package in 134 s; gofmt and vet are clean.
- **The diff, read by me:**
  - The provisional constants are gone: `reopenWindow` and `reopenEvery` are replaced by one `holidayRecheck` of 5 minutes, and `settleB3` is now 100 minutes.
  - `MarketOpen` stops counting the expected session once a row fetched successfully since that session started still carries the last session. A failed fetch does not count, because it leaves `FetchedAt` unchanged.
- **One real output:** `brl-stocks print` against the real source on 2026-11-27 at 21:18 UTC.
  - US rows show `15:00`, the early close (F44).
  - B3 rows show `17:54`, with `B3 closed`: the reported end has passed while the closing price has not arrived yet (F46). This is what D111 accepted.
- **The builder's choices:**
  - Inclusive times: they follow D112's wording ("at the usual open", "5 minutes later"). No question.
  - One row enough to mark the market closed: D112 reads "the source's answer", so one answer is enough. No question.
- **Speed:** the suite takes the same time as before. The cost of S5.17 is drawing the screen, which the boundary requires. Noted for the factory; nothing to do in this increment.
