# Test writer's report: the one-shot mode, increment 2

Returned 2026-11-24 for the brief in `test-brief.md`. The agent's summary, unedited:

---

I wrote 63 tests, one per criterion of increment 2, in `internal/app/print_surface_acceptance_test.go`. **62 pass, 1 fails, 0 are skipped.** `go vet` passes, and `go test ./internal/app/ -race` finishes in about 1 s with no panic or hang. The tests in the two earlier files still pass. I created or changed no other file.

**Failing test**
- **S15.8** (`TestS15_8_UnwritableCacheWarnsOnce`). Spec 7.7 says that when the cache folder cannot be written, "the tool still runs and says so once in a warning". The test makes the folder read-only and runs `print` in two cases:
  - The cache is 5 minutes old, so a fetch would be due: one warning appears (passes).
  - The cache is fresh, so no fetch is due: the error stream is empty. Observed: `0 warnings on the error stream with a cache folder that cannot be written, want 1: []`.

  At first I only had the fresh-cache case. When it failed I added the 5-minute case; it passed, and I kept both. Either the product warns only when it would write, or the spec should say the warning depends on that.

**Fixed in my own test**
- **S7.2:** my first version ran `--refresh` 1 s after the third failed fetch. 6.5 skips a forced fetch for a row attempted less than 5 s ago, so the setup was wrong. It now waits 10 s.

**Ambiguities**
- **S1.5, S15.6:** I took "the data columns" on `print` to be 7 (name, price, change value, change percent, low, high, time) and required `—` in each, with no stale mark. 6.2 says a "No data" row keeps its placeholders, but the glossary's "stale" covers "last fetch failed".
- **S6.9, S7.4:** the spec gives no format for "the time of the next attempt" on `print`. I required local `HH:MM`, as in the view example.
- **S11.18, S15.8, S18.1, S18.2, S18.4:** the wording of the warning, version and usage is not fixed. I only checked that they are present: "20" appears on the error stream, the output is not empty, and the table is not printed.

**Expectations I added beyond the criterion text**
- Day change is the last price minus the previous close (S14.2, S1.10–12).
- **S4.2/S4.3:** from 7.4's list, the only lines that are not rows are the summary line, a header row and the last line.
- **S8.5:** "no error line" read as an empty error stream, since nothing else should warn.
- **S5.10, S7.2, S6.5/6:** each requires that the refreshes happened, so they cannot pass against a product that does nothing.

**Missing from the boundary:** nothing. Request times are recorded by a wrapper around the stand-in source, and S7.1/S7.2 use `testing/synctest` for "within 2 seconds".

---

## Coordinator's sorting

Checked: `go vet` passes; the package passes with `-race` except S15.8; three tests read (S1.8 output, S7.7 "no request" with its given established, S6.9 failure line).

- S15.8: a fault. The warning came only when a write was attempted; with nothing due, `print` writes nothing and never found out. Fixed: `print` checks that the cache folder can be written when the cycle has not already found that it cannot. All tests pass with `-race`; the ordinary verification run passes (55.8 s).
- S1.5, S15.6, `—` in seven columns and no stale mark: decided by 7.3 ("No data yet. `—` in every data column"; the not-found example shows `—` in the name) and 6.2 (a No data row that fails stays No data). No question.
- S6.9, S7.4, the next attempt as local `HH:MM`: decided by nobody; the 7.3 example is illustrative. Proposal P56, asked as Q77.
- S11.18, S15.8, S18.x, presence only: the spec fixes no wording; the tests assert no more than it says. No question.
- Day change as price minus previous close: the boundary's quote type carries the previous close, and the domain's day change is against it. Kept.
- S4.2, S4.3 (7.4's list of non-row lines), S8.5 (empty error stream, given "no other problem"), and the given checks: follow from the spec or guard against a vacuous pass. Kept.

## Follow-up: D81 (S6.9 all-fail case, S6.11), same writer, resumed

The agent's summary, unedited:

> The new S6.11 test and both S6.9 cases pass. The whole internal/app package now passes, with 64 tests in my file, and go vet is clean.
>
> - S6.9: now has two subtests. In "two rows fail" the summary answers and the line must give 2. In "every fetch fails" all 3 rows and all 3 summary entries fail, and the line must give 3. In both, the line must also name `source unreachable` and the next attempt as `HH:MM`. To catch the fault, the test fails on any number in the line other than the row count, so a count of 6 would fail.
> - S6.11 (TestS6_11_OnlySummaryFailedSaysSummaryNotUpdated): only the Ibovespa fails, and the test first checks that every row updated. Exactly one line on the error stream must give a cause, and it must contain `summary not updated`.
> - S15.8: the case that failed before, a fresh cache with no fetch due, now passes too.
>
> Ambiguity: S6.11 doesn't say whether a cause phrase also appears on the line, so I only counted lines that give a cause. D79 now fixes the next-attempt time as `HH:MM`, which settles that earlier question.

Coordinator's check: against the error-line code before the D81 fix, S6.9's "every fetch fails" case and S6.11 both fail; with the fix, the package passes with `-race`. The S6.11 ambiguity: D81's example starts with the cause phrase, so counting the lines that give a cause is right. No question.
