# Verifier's report: the full-screen view, increment 6

Returned 2026-11-25 for the verifier section of `briefs.md` (a fresh agent; increments 1–5's verifier could not be resumed). Summary of the agent's report:

- All ordinary checks once at the end: 158 top-level checks pass, 0 fail, 7 skipped (SLOW and LONG), about 2 min 15 s. 7 new checks in `verification/view_increment6_verify_test.go`; all 11 criteria of increment 6 pass. Commit e1cc4c3.
- Existing checks changed: S12.2 (D95) runs at 120 columns with the default config path and expects `~/.config/brl-stocks/config.toml`, and stays at 200 columns with the full path when the config folder is outside the home folder; S9.6 and S9.7 (D93) say "screen line"; the Q85 observation became the asserting check `TestVerifyView_S9_6_7_ScrollByScreenLine`, over two group headers. README rows for S12.x, S9.6/S9.7, Q84/Q85 and S1.6 updated; a section for increment 6 added.
- Observed outside the criteria: the full summary line is 71 wide; below it all percents go at once; at 11 columns there is no summary line and rows are cut (`PETR4     R`). At 94 columns (one less than the line without the sort) the warning is cut with no ellipsis. With stdin from `/dev/null` and the output on a terminal, the view opens and `q` exits 0. Without a terminal, an invalid or missing `--config` gives the no-terminal line and exit 2, not the config message. Without a terminal, in four ways of redirecting, the tool exits 2, sends no request, writes nothing to standard output and one line: ``brl-stocks: the full-screen view needs a terminal; for redirected output use `brl-stocks print` ``.
- Not verified: the look on a real terminal. Hand check below.

## Coordinator's check and sorting

- Rerun of the ordinary checks by the coordinator: pass (136 s). `gofmt` flagged the new check file and the older `print_verify_test.go`: formatted, no change in behaviour.
- No fault. The observations follow D94–D96: all percents at once (D96), the warning cut once the sort is gone (D95), the terminal check before the config (the boundary's "without opening anything"; both are status 2).
- At 11 columns nothing of the summary line fits, so it goes (D96); rows cut there follow 7.3's narrow-terminal rule, which keeps four columns that do not fit in 11. Below the width of those four columns the spec says nothing; noted, not asked: no terminal is that narrow in use.

## Hand checks

1. Without a config file, run `brl-stocks` in a window about 100 columns wide, then narrow it below 95, 71 and 50: the sort goes first, then the warning is cut; the percents go below 71, then whole entries from the right.
2. With groups in the config, press `j` and `k` past a group header (one line per press), then the left and right arrows (nothing changes).
3. `brl-stocks > /tmp/out.txt; echo $?`: prints `2`, with one line on the terminal naming `brl-stocks print`.
