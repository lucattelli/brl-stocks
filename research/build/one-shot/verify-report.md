# Verifier's report: long checks for the one-shot mode

Returned 2026-11-24 for the brief in `verify-brief.md`. The agent's summary, unedited:

---

All 47 checks pass. Nothing failed. The ordinary run (42 checks with the slow ones skipped) passed in 51 s. The 5 SLOW checks passed in one parallel run of 20 min 22 s: 4 new ones plus the earlier recovery-after-block check. The 8-hour QR8 check was written but not run, as the brief says.

**What I changed:**
- **New check file:** `/home/lucattelli/Programming/prototyping/brl-stocks/verification/long_verify_test.go`.
- **New stand-in control:** in `/home/lucattelli/Programming/prototyping/brl-stocks/verification/standin/main.go` I added a `vary_quotes` control, which moves the price on every answer. It is off by default, so existing controls behave as before.
- **README:** `/home/lucattelli/Programming/prototyping/brl-stocks/verification/README.md` has the run commands for both switches. Its feature-map rows for QR4, QR5 and S15.3 are updated, and there are new rows for QR8 and the doubling (6.3 row 6).
- **Evidence:** `verification/evidence/20261124T170013Z/`.

**QR4** (`print` every 2 s for 10 min, interval 60 s, 23 entries, no view): 300 calls led to 230 requests, under the limit of 345. Every entry got exactly 10, so the largest per entry is 10.

**QR5 and the doubling** (source refuses everything, `print --refresh` every 2 s, 23 entries). Arrival times relative to the first refused answer:
- 8 requests at +0.000 to +0.001 s, all from the first call and all in flight together.
- One request (`^BVSP`) at +300.2 s.
- One request (`^BVSP`) at +901.9 s, which is 601.7 s after the refused probe's answer.

That is the 5-minute wait, then the doubled 10-minute wait. At most 2 requests fell in any 10-minute window counted after the first refusal; even counting all 8 first-call requests, a window holds at most 9.

**QR8, scaled** (20 min, interval 10 s, quotes changing on every answer): 110 samples. The cache was 20979 bytes after the first full refresh, and every later sample stayed between 20866 and 21273 bytes (within −0.5% to +1.4%). There were 2530 requests after the first refresh.

**Not verified:**
- Anything about the full-screen view, so QR4 and QR5 were measured without it, and S15.3 is covered only through `print`.
- The 30-minute cap on the wait.
- The 8-hour QR8 run, which has to be started separately with `BRL_VERIFY_LONG=1`.

**Ambiguous in the spec:**
- **"From the first refused answer" (QR5):** it does not say whether requests already in flight at that moment count.
- **"One probe" (6.3):** it is not defined. In practice a probe was a single request.
- **"Once per interval" (QR8):** a cache exactly one interval old is not fetched (6.5), so I called `print` every interval + 1 s.
- **"Cache size":** apparent bytes or disk blocks is unstated. I used the sum of the file sizes.

`git diff` also shows changes to `internal/engine/engine.go` and `internal/market/plan.go`. I did not make them; they are presumably the concurrent work the brief mentions. The ordinary run may have built against them.

---

## Coordinator's sorting

- No failing check, so no fault.
- The changes to `engine.go` and `plan.go` are the coordinator's two fixes from the test writer's findings (`test-report.md`). The ordinary checks were rerun by the coordinator on the fixed build: they pass (48.7 s).
- "From the first refused answer": requests sent before that answer are not "from" it; even counting them, a window holds 9, within the limit of 10. No question.
- "One probe": the word came from the coordinator's brief, not the spec. The spec (6.3) says only what happens when the wait ends; how many requests that takes is bounded by QR5. No question.
- "Once per interval" and "cache size": the verifier's readings (interval + 1 s; the sum of file sizes) follow from 6.5 and the plain meaning. No question.
- The 8-hour run was started by the coordinator on 2026-11-24, on the fixed build. Result, 2026-11-24 to 25: 8 h 0 min of open market, 472 samples, first 22150 bytes, min 21964 (−0.8%), max 22432 (+1.3%), 10856 requests after the first refresh (one per entry per call), PASS; evidence `verification/evidence/20261124T172137Z/`. QR8 holds for `print`; S15.3 itself needs the view.

## Follow-up: S13.22–S13.24 and the criteria shared with other stories (same verifier, resumed)

Summary of the agent's report: 55 top-level checks in the ordinary run, 0 failing, 5 skipped (the SLOW and LONG checks). The concurrency check became `TestVerifyS13_22_23_TenPrintsOnEmptyCache`: all ten processes printed a quote for each of the 5 rows, and the source got exactly 1 request per row (the summary entries also 1 each, logged). New: `TestVerifyS13_24_NoCacheUnreachableMarketsUnknown` (`B3 unknown · US unknown` with 503 and with no connection), `TestVerifyW6_5_NotFoundRow` (row, JSON `not_found` and state `partial`, no error line, not requested again after one interval or with `--refresh`, requested again after a config change), and checks for S3.1–S3.5, S3.7, S4.4, S11.1, S11.2, S11.9/16, S11.12/17, S11.15, S12.1–S12.5 and S17.3. S11.5, S11.7, S11.8 and S17.2 are mapped to existing checks. Ambiguities it raised: summary entries outside S13.23's "per row"; "names the problem" (S11.16) not measurable; "under the config path" (S12.3) read as the `brl-stocks` folder; "the other data columns" of a not-found row read from the 7.3 example; S17.3 "listed" read as help text and config keys; S4.4 applied to `print`.

Coordinator's check and sorting: the ordinary checks pass on a rerun (57.8 s), `go vet` is clean. Every ambiguity is settled by the glossary (*Row*), 7.2 (config paths), 7.1 and 7.3, or is a reading that adds no expectation. No question.
