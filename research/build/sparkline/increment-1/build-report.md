# Build report: the sparkline (slice 4), increment 1

The builder's summary, unedited:

> The sparkline is built: all 23 tests in `sparkline_increment1_acceptance_test.go` pass, and so do all earlier tests. One ordinary verification check fails, and I think the time of day causes it rather than my change. I did not prove that against the build without my change. Committed as 761e5cb on slice4/1-build in /home/lucattelli/Programming/prototyping/brl-stocks-wt-build (changes in `internal/render/render.go` and `internal/tui/tui.go`).
>
> **What I built:** a `TODAY` column, shown only in the view, so `print` stays as it was (7.4). It is placed like any other column: two spaces after the time column, which includes the stale mark. So `TODAY` starts at the sparkline's column, as Q104's recommendation and the 7.3 example show. It is the first column a narrow screen drops, and its header goes with it.
>
> **Choices the spec left me:**
> - **Price exactly halfway between two levels:** it rounds up.
> - **Out-of-session prices (S10.9):** I already leave out points before the session start or after its end, with the end itself counted as inside. No test of this increment checks it.
> - **Colour:** it follows the day-change value as shown, rounded to 2 decimals, like the change column. A change that shows `+0,00` gets no colour.
>
> **Checks:** with -race the suite took 47.7 s and 47.8 s on two runs. vet and gofmt are clean.
>
> **Verification:** 2 of the ordinary checks failed, both in code I did not change:
> - `TestVerifyView_S5_4_5_USClosed` (S5.4) depends on the time of day. It ran just after midnight UTC, where the view showed the close as `25/11` instead of `23:56`, because the close fell on the previous UTC day. It should pass at other hours; rerunning after 03:00 local would confirm it.
> - `TestVerifyView_S7_2_RDuringWaitAfterFailure` passed when I reran it alone, so it looks timing-sensitive when the whole suite runs.

## Coordinator's check and sorting

- No test file changed since the tests' commit (800eaf6). gofmt and vet clean. `go test ./... -race -count=1` passed twice, 46 s and 47 s.
- Halfway between two levels, rounded up: a choice D105 does not make. **Q105**, built on the recommendation (as built).
- Prices outside the session: left out by the provider since slice 1 (`internal/provider/yahoo`). Checked by verification (S10.9). No question.
- Colour from the change as shown: the rule the change column already follows (D50: no colour when zero). No question.
- The two failing verification checks fail on the branch without the build too (run at 22:57 local, 01:57 UTC), so they are not this build's. Both are faults of the checks, not of the product:
  - S5.4: the checks run the tool with `TZ=UTC`; just after midnight UTC, a close two hours earlier is yesterday's, and 7.3 asks for `DD/MM`. The check expects `HH:MM` at any hour.
  - S7.2: the check takes the time of `r` after writing the key; the view's requests came within the same millisecond, before that time was taken (16 requests; MSFT 1 ms after, the other four 0.1 ms before).
  Both go to the verifier to be fixed to follow the spec, not loosened.
