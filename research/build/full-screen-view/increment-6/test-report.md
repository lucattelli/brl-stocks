# Test writer's report: the full-screen view, increment 6

Returned 2026-11-25 for the test-writer section of `briefs.md` (a fresh agent; the writer of increments 1–5 could not be resumed). The agent's summary, unedited:

---

I wrote and committed the increment-6 tests: 9 new tests plus S9.6 and S9.7 updated to the new wording. Against the current build 8 fail on their assertions and 3 pass. Nothing is skipped and nothing panics or hangs; the whole app suite takes about 9 s with `-race`, and `go vet` passes.

Commit a54235f on slice2/8-tests, in the worktree /home/lucattelli/Programming/prototyping/brl-stocks-wt-tests:
- internal/app/view_increment6_acceptance_test.go (new)
- internal/app/view_increment5_acceptance_test.go (only the two tests, renamed `...OneLine`; each has a new case with two groups, Brasil and US, that scrolls one press at a time past both headers)

**Failing (8):** S2.9 and S2.10 (the summary line is cut, percent included); S12.6, S12.7, S12.8 and S15.10 (the full home path is shown, the sort stays, the warning is cut); S18.5 (exits 1); S18.6 (stderr is a bubbletea /dev/tty error).

**Passing (3), each shown failing on a changed copy outside the repository:**
- **S9.6:** passes because D93 was built in PR #7. With each press sent twice, the group cases fail: "with Brasil at the top, down put VALE3 at the top, want PETR4".
- **S9.7:** same reason. With each press sent twice: "up put SUZB3 at the top, want US".
- **S9.11:** passes because D92 was built in PR #7. With the down arrow sent in place of left or right: "the screen changed".

**Expectations I added beyond the criteria:**
- S2.9 is tested at the width of the full-width summary line minus each percent and its space, so it assumes the separators the view draws at full width.
- S2.10 requires at least the first entry to be shown, at 43 and 25 columns.
- S12.7 and S12.8 split the status line at "·" and take the sort to be the one part that changes when `s` is pressed. The warning is whatever the no-config line has that a line with a config lacks.
- S18.6 requires exactly one line on stderr, as 7.1 says.

**Ambiguities and gaps:**
- S2.9 does not say whether a view may shrink the gaps between entries before dropping percents.
- The header comment of `view_increment5_acceptance_test.go` still calls Q84 and Q85 open. I was not allowed to change it.
- With the current build, S18.5 and S18.6 make `Run` try to open /dev/tty. In a real terminal that could take over the terminal until the fix lands.
- One grep I ran showed the log's lines D92–D96. They match the spec, and I used only the spec.

---

## Coordinator's check and sorting

- Checked: only the two test files changed; `go vet` clean; rerun with `-race`: the same 8 fail and 3 pass.
- Gaps between summary entries shrinking before percents go: D96 says the percents go first, so the test's reading follows it. No question.
- The added expectations only guard against a vacuous pass (an empty summary line, an unnamed sort, several lines). Kept.
- The stale header comment: the coordinator rewrote that comment only; no assertion changed.
- S18.5 and S18.6 opening the process's terminal against the current build: the build fixes it. CI has no terminal.
