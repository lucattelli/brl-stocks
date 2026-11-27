# Build report: live refresh (slice 3), increment 3

The builder's summary, unedited:

> `r` is built: 7 of the 8 increment-3 tests pass, and S7.2 fails because the test conflicts with the spec. I stopped on it rather than guess. Committed as 159a84b on slice3/3-build in /home/lucattelli/Programming/prototyping/brl-stocks-wt-build.
>
> **The S7.2 conflict:** The test presses `r` at 15:04:02, one second after PETR4's third failed attempt at 15:04:01. Spec P33 (D50) and the design's `Plan` say a forced refresh skips a row attempted less than 5 seconds ago, so PETR4 is correctly not requested. The brief asks for that guard too. The test's setup needs `r` at least 5 s after the last attempt.
>
> **What `r` does:** it runs a forced refresh through `Plan`. That covers closed markets and rows waiting after failures, but not-found rows keep their own wait and nothing is sent during a block. During a block the status line now always shows `next attempt HH:MM` after the market states, kept whole when the line is shortened, as spec 7.3 lists it. The changes are in `internal/tui/tui.go` and `internal/render/render.go`.
>
> **Choices the spec left me:**
> - **`r` while a refresh is running:** the press is remembered, and a forced refresh starts when the running one ends. Rows the first refresh just tried are then skipped by the 5 s guard. The spec only says a refresh "starts within 2 seconds" (6.3) and does not cover this case.
> - **`r` with the help open:** it only closes the help, since spec 7.3 says any key does.
>
> **Checks:** vet and gofmt are clean, and nothing else fails. With -race the suite takes 43.7 s and 42.5 s on two runs, each failing only S7.2. The ordinary verification passes in 366 s.

## Coordinator's check and sorting

- No test file changed by the build (its diff touches only `internal/tui/tui.go` and `internal/render/render.go`).
- **S7.2:** a test issue, decided by the spec (6.5, D50: a forced fetch skips a row attempted less than 5 seconds ago). The test writer moved the press to 10 s after the third failure (f5ee05e); the fixed test still fails on the increment-2 build. The build was not changed.
- With the fix under the build: gofmt and vet clean (also `-tags verification`); `go test ./... -race -count=1` passes twice, 41 s and 42 s.
- **`r` while a refresh runs** (the press is kept; a forced refresh follows the running one): a choice the spec leaves open. Q95, built on the recommendation, which is the builder's choice.
- **`r` with the help open closes it only:** decided by 7.3 ("any key closes the help"). No question.
- **`next attempt HH:MM` in the status line during a block:** 7.3 and D38/D79 list it; S7.4 tests it. No question.
