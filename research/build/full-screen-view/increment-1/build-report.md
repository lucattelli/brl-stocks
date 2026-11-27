# Builder's report: the full-screen view, increment 1

Returned 2026-11-24 for `build-brief.md`. The agent's summary, unedited:

---

Increment 1 of the view is built. All 35 increment-1 tests pass, the print tests still pass, and no test is blocked. Nothing is committed.

**What I built**
- `internal/tui/tui.go` (new): the Bubble Tea 2 model. Opening the view reads the cache as a command and shows it, then runs one `engine.Cycle` (`MayFetchUnpaced: true`, 10 s deadline). Results are merged into memory by protocol rule 7, so a quote is never replaced by nothing. `q` and Ctrl+C quit. The view draws the summary line, a blank line, the column header and rows, then the error line (`render.Cause`) and the status line at the bottom.
- `internal/app/view.go` (new): `runView`. With a `Terminal` it runs the driver described in the design. Without one it runs `tea.NewProgram` with the alternate screen. Lines are cut to the width in `app`.
- `internal/app/app.go`: the no-command path now opens the view.
- `internal/render/render.go`: the table code moved into `render.Lines`, with `SummaryLine` and `MarketLine` exported, so `print` and the view share one formatter. `Table` uses them, unchanged in output.

**Dependencies:** `charm.land/bubbletea/v2` v2.1.2, plus what it pulls in indirectly; `go mod tidy` was run. I did not use Lip Gloss, because the existing render code already formats and colours the rows.

**Tests:** `go vet` is clean. `go test ./... -race` run 5 times: 165 top-level tests and 50 subtests per run, 0 failed, 0 skipped.

**Hand check:** I ran the built binary under `script` against the repo's existing `verification/standin`, built into the scratchpad. It enters and leaves the alternate screen, draws the rows, and exits 0 on both `q` and Ctrl+C.

**Choices I made**
- A command counts as running until its message has been handled, not just until it returns, so the idle frame is the one that shows the fetch's result.
- Waits on `deps.After` are not yet counted as idle. Nothing waits on the timer in this increment; slice 3's heartbeat will need it.
- On quit or a cancelled context, the driver cancels the commands' work and waits for them, so nothing outlives `Run` and touches the test's temp folders. One consequence: a request cut off by quitting is committed as a failure, which is the engine's existing behaviour on cancel. The real program just exits instead.
- The status line is the market states plus "sort: file order" and any config warnings. I left out "? help" until help exists. Rows that don't fit the height are dropped until scrolling arrives in increment 5.
- When the real terminal fails, the view exits with status 1, which the spec doesn't define.
- Width is counted as one cell per character, without an ANSI library.

**Where the code departs from the design:** `tui` imports `config` for the config type, which the design's import list doesn't show.

**Other notes:** `internal/deps/deps.go` (from PR 0) isn't gofmt-clean; I left it alone. `research/build/full-screen-view/increment-1/verify-brief.md` showed up as untracked while I worked; I didn't write it.

---

## Coordinator's check and sorting

Checked: no test file changed; `go vet` clean; `go test ./... -race` passes twice; the ordinary verification of `print` still passes (94.0 s), so moving the table code into `render.Lines` changed nothing `print` shows.

- `tui` imports `config`: the design's import line updated. No question.
- `deps.go` formatting: fixed with `gofmt`.
- A request cut off by quitting is committed as a failure, in the test driver only: a quit during the fetch at start then lengthens that row's wait for the next run. Noted for slice 3, where quitting during a refresh is common; the verifier checks the real program.
- Exit status 1 when the terminal cannot be used (for example `brl-stocks > file`): not in the spec. A question for the user (Q86), asked with this PR.
- One cell per character: right for every character the view draws today (`—`, ASCII, `R$`); names with wide characters are not in the spec. Noted.
- "? help" left out of the status line until increment 5, rows that do not fit dropped until scrolling: both follow the increments' order.
