# Builder's report: the full-screen view, increment 6

Returned 2026-11-25 for the builder section of `briefs.md` (a fresh agent). The agent's summary, unedited:

---

I built increment 6 and committed it as e9603d5 on slice2/8-build. All 234 top-level tests pass with `go test ./... -race -count=1`, none fail or are skipped, and `go vet` is clean. No test file was edited. S9.6/S9.7 (group-header case) and S9.11 already passed before I changed anything, so D92 and D93 needed no code.

**Files**
- `internal/render/render.go` (D96): when the view's width is set and the summary line doesn't fit, it drops every entry's percent, then whole entries from the right. `print` has no width, so its output is unchanged and its tests pass.
- `internal/tui/tui.go`: the summary line keeps its place even when no entry fits. The status line drops `sort: …` before a warning is cut.
- `internal/app/app.go`, `view.go` (D95): warnings write the home folder as `~`. The view's no-config warning is now `no config: example watchlist; put yours at ~/…`; `print` keeps its old note. With no Terminal and `IsTTY` false, the view writes one line naming `brl-stocks print` and exits 2 (D94).
- `cmd/brl-stocks/main.go`, `go.mod`: terminal detection uses `x/term`, which was already an indirect dependency; it is now direct, with no new module. The old check counted `/dev/null` as a terminal.

**Choices I made**
1. Percents are dropped all at once, not one at a time, and stay off when fewer entries are shown.
2. The sort is dropped only when there is a warning. A line without one is just cut.
3. The no-terminal check runs before the config is read ("without opening anything").
4. I used the spec's example wording for the no-config warning as is.

**Gaps and checks**
- If stdout is a terminal but Bubble Tea still can't open the TTY, it still exits 1 with the library's error. Bubble Tea gives that case no error value to test for.
- By hand: output to a file, to `/dev/null` and to a pipe each exit 2 with the one line. Under `script` the view opens.

Nothing in the design turned out wrong.

---

## Coordinator's check and sorting

Checked: no test file changed; `go vet` clean; `go test ./... -race -count=1` passes twice; the built binary with its output redirected writes `brl-stocks: the full-screen view needs a terminal; for redirected output use brl-stocks print` and exits 2. The ordinary verification of `print` runs in stage 3 (the summary line's formatter changed).

- `x/term` used directly: a dependency the design did not name, though already in the module graph. Added to the design's dependency table, pending the user's approval in PR #10 (rule 6); the alternative is a system call per platform by hand.
- The old terminal check counted `/dev/null` as a terminal: a fault from increment 1, fixed here. It also stops `print > /dev/null` from writing colour codes.
- Percents dropped all at once (choice 1): D96 says "each entry's percent first"; Q88's option read the same. Follows D96. No question.
- The sort dropped only for a warning (choice 2): D95 speaks of a warning that does not fit. Follows D95.
- The check before the config is read (choice 3): follows the boundary's "without opening anything". A bad `--config` with redirected output now exits 2 for the terminal, not the config; both are status 2. Noted.
- Exit 1 when standard output is a terminal but the view still cannot open it: outside D94, which speaks of no terminal. Noted for the PR; rare.
