# Verifier's report: the one-shot mode, increment 2

Returned 2026-11-24 for the brief in `verify-brief.md`. The agent's summary, unedited:

---

## Result
I added 41 checks in `verification/surface_verify_test.go`: 40 ordinary ones and 1 slow one, which skipped. I ran the full ordinary suite once and it took 94 s. All the earlier checks still pass. Of my 40, 39 pass and 1 fails. Each of the 63 criteria and QR12 has its own named subtest, and some run in several variants, giving 100 subtests: 96 pass and 4 fail. By criterion, 62 of the 63 pass and S6.9 fails.

## Failing check
**S6.9** (`TestVerifyS6_01_02_03_09_FailedRefresh/*/*/S6_9`). The error line should give the number of rows not updated. The glossary defines a row as one line of the watchlist, so when every fetch fails I expected 5. The tool counts the 3 summary entries as rows:
`source unreachable: 8 rows not updated · next attempt 00:25`
With 20 tickers it says 23. This happens in both failure modes (status 503 and no connection), at both sizes. When one row fails it correctly says `1 row`. A block gives the same count (`source is rate-limiting: 8 rows not updated`). The cause phrase and the `HH:MM` were right.

## QR1 and QR12
- **QR1:** these checks also ran with 20 tickers and 3 summary entries, and all pass except S6.9 as above: row formats (S1.2, S1.8–S1.15), groups and order (S4.1–S4.3, S9.1), failed fetches (S6.1–S6.3, S6.8, S6.9) and not-found rows (S8.1, S8.2, S8.5).
- **QR12 passes.**
  - Every request was a `GET /v8/finance/chart/<requested symbol>?interval=5m&range=1d` with no body.
  - The only headers were `Accept-Encoding: gzip` and `User-Agent: Mozilla/5.0 (compatible; brl-stocks)`.
  - No marker reached the source: not the user or host name, the folders, or the config's group name.
  - The proxy trap received no connections.
  - It does not prove that the tool honours proxy settings at all, catch traffic that ignores them or name lookups, or show what goes to the real source over TLS.

## Not verified
- **QR10 / S17.1:** not run, because it needs the real source. I wrote the hand-check commands in the README, since the existing README had none.
- **S6.10 at 12 minutes:** only the scaled version ran (12 s, mark `!12s`). The literal slow check exists, but the permission system blocked running it in the background.
- **Scaled down:** S5.6 (25 s, not two days), S6.1–S6.3, S7.2 (a 60 s wait, not 4 min) and S7.8 (12 s, not 10 min).

## Ambiguities
- Whether summary entries count as "rows" in S6.9 and S7.4.
- S5.6 cannot be set up literally on `print` without editing the cache or a fake clock.
- S1.5: the header shows 6 columns but a row holds 7 `—` cells. I accepted 6 or more.

## Other changes
- `verification/standin/main.go`: the request record now also holds method, Host, headers and body length, and a new `open_end_unix` control sets when the open market's session ends (used for S5.12). Existing controls are unchanged.
- `verification/README.md`: one new line under "Parts" and a new increment-2 feature map. No existing rows were changed, but the new section says it takes precedence over the old "not verified" rows for S3.6, S11.13 and S11.18.

Evidence is in `verification/evidence/20261125T002539Z/`.

---

## Coordinator's sorting

Rerun by the coordinator: the ordinary checks fail only on S6.9 (92.8 s).

- S6.9, summary entries counted as rows: a fault, by the glossary's *Row* (D9), as already settled for S13.23. Fixing it left one case undecided: only a summary entry fails. Asked as Q78; answered (b), D81: the line says `summary not updated`. Fixed in the product; S6.11 added to the spec; the test writer and the verifier were resumed for S6.11 and the all-fail case of S6.9.
- "Summary entries as rows" in S7.4: the same reading, settled by D81.
- S5.6 not set up literally: scaled (25 s), with the two days covered by the acceptance test through the fake clock. Route: acceptance test TestS5_6_*.
- S1.5, six header columns and seven `—` cells: the name column sits under the header NAME with the ticker, which the 7.3 example shows. No question.
- Not verified, with routes:
  - QR10 / S17.1: hand check, commands in `verification/README.md`.
  - S6.10 at 12 minutes: acceptance test TestS6_10_* (fake clock); the slow check can be run by hand with BRL_VERIFY_SLOW=1.
  - Scaled times (S5.6, S6.1–S6.3, S7.2, S7.8): acceptance tests with the fake clock, which use the criteria's own times.
  - QR12's limits (proxy-ignoring traffic, name lookups, TLS to the real source): a hand check with a packet capture during a real run, on the hand-check list.

## Follow-up: D81, same verifier, resumed

The agent's summary, unedited:

> I ran the ordinary checks once, across all files. Everything passes: 96 checks pass, 0 fail and 6 are skipped (the slow and long ones), in about 95 s.
>
> Checks that changed:
> - **S6.9 now passes** (`TestVerifyS6_01_02_03_09_FailedRefresh`, check unchanged). With all 5 rows failing the line reads `source unreachable: 5 rows not updated · next attempt 00:52`. With 20 tickers it says `20 rows`, and with one row failing it says `1 row`.
> - **S6.11 is new and passes** (`TestVerifyS6_11_SummaryEntryFails`). On an empty cache every ticker answers and `^GSPC` gets 503. The one cause line reads `source unreachable: summary not updated · next attempt 00:50`. I needed no new stand-in control.
> - The S6.11 row is now in the increment-2 feature map in `verification/README.md`.
>
> My first attempt hung before any test started, on a stray `python3 -` left in my command; I stopped it. The QR8 background run was not touched.
>
> Ambiguity: the new wording does not say what the line reads when both rows and summary entries fail. I assumed rows are counted and the summary is not mentioned; S6.9 checks only the row count.

Coordinator's rerun: the ordinary checks pass (94.0 s). The ambiguity is settled by D81: the line counts rows, and names the summary only when no row failed. No question.
