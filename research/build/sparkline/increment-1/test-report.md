# Test report: the sparkline (slice 4), increment 1

The test writer's summary, unedited:

> I wrote 23 tests for slice 4 increment 1 and committed them as 800eaf6 on slice4/1-tests, in internal/app/sparkline_increment1_acceptance_test.go. They cover S10.1–S10.8, S10.10–S10.22, `TestS1_6_Sparkline_…` and `TestS16_3_Sparkline_…`. S10.1, S10.2 and S10.8 also run with 20 tickers.
>
> **Against the current build:** 22 fail on assertions, because no row has a sparkline yet. Many fail on a given check that a sparkline is shown. vet passes, every earlier test still passes, and nothing panics or hangs.
>
> **S10.3 passes now:** no key opens anything. It checks after each 7.3 key that only the summary line, header, rows and status line remain, and that the rows stay. On a changed copy that leaves the help open after `?`, it fails: "after ["?"] the screen holds other lines…". I first tried making the source fail on `r`, but that changed nothing: the press falls within the 5-second guard after the fetch at start.
>
> **How the tests work:** the sparkline is the run of blocks `▁…█` that a row ends with; each series has one point a minute from the session start, all inside the session; S10.1 and S10.2 use prices that land exactly on a level (10–17), so the expected glyphs are exact; S10.15–S10.17 check that the row is all spaces from the sparkline's column on; S10.18 and S10.22 use the view's clock; S10.19 uses an earlier view's cache and a source that holds its answers.
>
> **Added beyond a criterion's text:** S10.5 also accepts SGR 92 for green, and S10.6 accepts 91 for red. S10.20 requires `TODAY` to start at the sparkline's column. In S10.3, `?` is closed with Esc, and `q` must exit with status 0.
>
> **Nothing skipped and no ambiguity in the spec.**

## Coordinator's check

- Rerun: 22 of 23 fail on assertions, S10.3 passes; vet and gofmt clean; the suite takes well under a second.
- S10.3 passes before the build for its stated reason; its shown failure is a copy that leaves the help open.
- Sorting the additions:
  - SGR 92 and 91: looser than the boundary (only 32 and 31), not stricter than S10.5 and S10.6 ("the palette's green/red"). Kept.
  - Esc closes the help: 7.3 says any key closes it. Kept.
  - `TODAY` starts at the sparkline's column: no decision says so (D107 names the header, not its place; the example in 7.3 shows it). Q104, built on the recommendation (as the example).
