# Test writer's report: the full-screen view, increment 1

Returned 2026-11-24 for `test-brief.md`. The agent's summaries, unedited.

---

I wrote 35 view tests, one per increment-1 criterion, in internal/app/view_acceptance_test.go. It is standard library only and I touched no other file. Against the skeleton, `go vet` passes; `go test ./internal/app/ -race` gives 30 failures, 5 passes and 0 skips in about 2 s, with no panic, race or hang. Every view test fails on its assertion because `Run` returns status 70 before drawing a frame.

**Naming:** the package already has `TestS1_2_...`, `TestS3_1_...` and others, so every name carries `_View_` (`TestS1_1_View_...`).

**The 5 that pass, and why:** S17.4, S18.1, S18.2, S18.3 and S18.4. They pass because slice 1 already parses the command line without `print`. `--version` prints `brl-stocks dev` and exits 0. `--help` prints the usage to stdout and exits 0, and the usage does not name `BRL_STOCKS_SOURCE_URL`. An unknown flag exits 2 with the usage on the error stream.

**Ambiguities:**
- **S18.1:** the version text is not fixed. I assert only that stdout is not empty.
- **S18.2:** the spec does not say which stream the usage goes to. I assert stdout or stderr is not empty.
- **S18.4:** what "a usage message" contains is not fixed. I assert stderr is not empty.
- **S1.5:** it is unclear whether the empty sparkline counts as a data column. I assert every field after the ticker is `—`.
- **S1.4:** the spec does not say whether the view fetches at start when the cache is fresh. I used a cache 90 s old: due for a fetch, not yet stale.
- **S16.1/S16.2:** I accepted SGR 32/92 and 31/91, as the print tests do. The boundary allows only 32 and 31.

**Added beyond the criteria's text:**
- QR7: every helper checks the config is byte-identical when the test ends.
- S1.5 also checks the price appears once the source answers.
- S3.6 also checks low and high, and that neither currency symbol appears in the other row.
- S3.7 checks no lowercase `petr4` appears anywhere.
- S15.4 compares whole frames with a no-cache run, before and after the answers.
- S15.7 requires stderr to equal the no-cache run's.
- QR1: S1.1, S1.2, S1.4, S1.5 and S15.1 also run with 20 tickers.

**What the boundary lacks:** `Idle` is also true while requests wait on the source. To detect "the fetch at start has settled", the helper requires `Idle`, the frame's input count, zero stand-in requests in flight when the frame is drawn, and a 30 ms quiet period. A frame field for requests in flight would make that exact.

---

## Coordinator's sorting

- The 5 passing tests: the command line without `print` already works (slice 1, D77). Kept.
- "What the boundary lacks": a gap in the boundary. Fixed in the design (pending approval with PR 0's boundary): `Idle` now excludes requests in flight. The writer was resumed to drop the quiet period.
- S18.1, S18.2, S18.4 wording and streams: the spec fixes no wording; 7.1 says only "print usage". The tests assert presence. No question.
- S1.5 and the sparkline: the sparkline is slice 4; in this slice the view shows no sparkline, so every field after the ticker is `—`. No question.
- S1.4 and a fresh cache: the view's fetch at start follows the same rules as any refresh (6.5's order, design `Plan`); a cache 90 s old is due. No question.
- S16 accepting 92 and 91: looser than the boundary, not stricter. Kept.
- The added expectations: each follows from 7.3, 6.2 or QR1, QR7, or guards a vacuous pass. Kept.

## Follow-up (same writer, resumed)

> The settle helper now relies on `Idle` and `Inputs` only. The 30 ms quiet period and the in-flight check are gone, and the 10 s real-time guard stays. `go vet` passes. Against the skeleton, `go test ./internal/app/ -race` still gives 30 fail, 5 pass (S17.4, S18.1–S18.4) and 0 skipped, with no panic, race or hang.
>
> One case still needs more than `Idle` and `Inputs`. S1.4, S1.5, S15.4 and S15.7 need a frame from before any answer, while the source holds its requests. Such a frame is never idle now. A new helper, `beforeAnswer`, waits until a held request is in flight, then takes the latest frame that shows the rows.

Coordinator: a frame from before any answer is found by what it shows while the source holds its answers, which is deterministic. Kept.
