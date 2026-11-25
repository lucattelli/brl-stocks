# Verification of the one-shot mode, from outside

These checks run the **real binary** as a child process against a local
stand-in for the quote source, and assert only on what a user or a script can
observe: standard output, the error stream, the exit status, the requests the
stand-in received, and files on disk. They were written from `docs/spec.md`
(sections 6.3, 6.5, 7, stories S13 to S15, QR3, QR4, QR5, QR7; later also
the criteria of S3, S4, S11, S12 and S17 that `print` can show) without reading
the implementation.

A failing check is a finding about the tool. Do not adjust a check until it
passes.

## Run

```sh
# ordinary checks, about 1 minute
mise exec -- go test -tags verification ./verification/ -v

# the slow check as well (recovery after a block: a little over 5 minutes)
BRL_VERIFY_SLOW=1 mise exec -- go test -tags verification ./verification/ -v -timeout 15m

# only the slow checks (they run in parallel: about 20 minutes)
BRL_VERIFY_SLOW=1 mise exec -- go test -tags verification ./verification/ -run TestVerifySlow -v -timeout 30m

# the full QR8 run: 8 hours of real time, to be started separately
BRL_VERIFY_LONG=1 mise exec -- go test -tags verification ./verification/ -run TestVerifyLong -v -timeout 9h
# a shorter run of the same check (any Go duration)
BRL_VERIFY_LONG=1 BRL_VERIFY_LONG_DURATION=2h mise exec -- go test -tags verification ./verification/ -run TestVerifyLong -v -timeout 3h
```

`BRL_VERIFY_SLOW=1` turns on the checks of up to about 20 minutes
(`TestVerifySlow_*`); `BRL_VERIFY_LONG=1` turns on the 8-hour check
(`TestVerifyLong_*`). Without them the ordinary run stays about 1 minute.

The build tag `verification` keeps these checks out of the ordinary
`go test ./...`.

## Parts

- `standin/main.go`: the stand-in source (standard library only). It serves
  synthetic quotes (made-up numbers) on
  `GET /v8/finance/chart/<symbol>?interval=5m&range=1d`, with trading periods
  placed relative to the current time. `POST /_control` switches its behaviour
  while it runs (a status for everything, a status for one symbol, a delay,
  closed markets, changed quotes); `GET /_requests` returns every request it
  received, with arrival time, symbol and the status answered.
- `long_verify_test.go`: the long checks (QR4 and QR5 at 10 minutes, the
  doubling of the wait, QR8), behind `BRL_VERIFY_SLOW` and `BRL_VERIFY_LONG`.
  They use the stand-in's `vary_quotes` control, which changes the price on
  every answer.
- `surface_verify_test.go`: increment 2 of the one-shot slice (the criteria
  tagged `print` that the first checks did not cover), QR1 and QR12. Where
  several criteria observe one run, each criterion is a subtest named after
  it (`.../S1_08`) and writes its own `CRITERION` and `RESULT` lines into the
  run's evidence. Added to the stand-in for it, without changing the existing
  controls: the request record also holds method, `Host`, headers and body
  length, and the control `open_end_unix` sets when the open markets' session
  ends.
- `print_verify_test.go`: the checks. `TestMain` builds the tool and the
  stand-in once into a temporary folder. Each check starts a stand-in of its
  own on a free loopback port and runs the tool with a clean environment:
  `HOME`, `XDG_CONFIG_HOME`, `XDG_CACHE_HOME` and `TMPDIR` point at temporary
  folders, `TZ=UTC`, and `BRL_STOCKS_SOURCE_URL` is always the stand-in (the
  helper refuses any address that is not `http://127.0.0.1:...`).
- `evidence/<UTC timestamp>/<check>.txt` (not in git): what each check
  observed: command lines, environment, standard output, error stream, exit
  status, wall time, and the stand-in's request record per run and in full.

## Feature map

"Scaled" means a time in the criterion was shortened: `refresh_seconds = 10`
(the minimum) stands for the default 60, and "12 seconds old" stands for
"older than the interval".

### S3 Two markets, plain tickers, own currency (in `print`)

| Criterion | Check | Note |
|---|---|---|
| S3.1 to S3.5 | `TestVerifyS3_01_05_MarketOfATicker` | One config with all nine tickers; JSON `market` and `currency`, and `R$`/`US$` in the table. Also 7.2: `US:ABCD1` and `B3:XYZ` shown without the prefix. S3.1 also by `TestVerifyS13_17_B3PriceFormat` |
| S3.6 | not verified here | Worded for the view ("on screen"); in `print`, `TestVerifySec7_4_RowAndSummaryFormat` shows the stand-in's prices unconverted |
| S3.7 | `TestVerifyS3_07_LowerCaseTickerShownUpper` | Table and JSON |

### S4 Groups (in `print`)

| Criterion | Check | Note |
|---|---|---|
| S4.4 | `TestVerifyS4_04_TickerInTwoGroupsFetchedOnce` | First refresh on an empty cache and a forced one; also 7.2: shown in both groups. S4.1 to S4.3 are about the view; `print` has S13.8 |

### S11 The watchlist is a file I edit (in `print`)

| Criterion | Check | Note |
|---|---|---|
| S11.1 | `TestVerifyS11_01_DefaultPathUnderHome` | `XDG_CONFIG_HOME` unset, `HOME` temporary |
| S11.2 | `TestVerifyS11_02_ConfigFlag` | The flag before and after `print`; a different watchlist at the default path is not shown |
| S11.5 | `TestVerifyQR7_ConfigNeverTouched` | Config folder and `config.toml` are both symbolic links; the rows show the file's watchlist |
| S11.7, S11.8 | `TestVerifyQR7_ConfigNeverTouched`, and every check | For `print` only: the view is not built yet. Every run of every check compares the config file's checksum |
| S11.9, S11.16 | `TestVerifyS11_09_16_InvalidConfig` | Unparsable, wrong type, unknown summary name, empty ticker (7.2); default path and `--config`. "Names the problem": asserted only as text beside the path; the messages are in the evidence. S11.9 also by `TestVerifyS14_08_UnparsableConfigExit2` |
| S11.12, S11.17 | `TestVerifyS11_12_17_UnknownKey` | Warning on the error stream |
| S11.15 | `TestVerifyS11_15_XDGConfigHome` | A different watchlist at `~/.config` is not shown |
| S11.3, S11.4, S11.6, S11.10, S11.11, S11.13, S11.14, S11.18, S11.19 | not verified | About the view |

### S12 No config yet (in `print`)

| Criterion | Check | Note |
|---|---|---|
| S12.1, S12.2, S12.3 | `TestVerifyS12_01_03_NoConfig` | `XDG_CONFIG_HOME` set and unset; `print`, `print --json`, `print --refresh`. Example rows and default summary per 7.7. S12.3 for `print` only |
| S12.4, S12.5 | `TestVerifyS12_04_05_ConfigFlagMissingFile` | Also 7.6: with `--json`, a document with state `error` |

### S13 One-shot table

| Criterion | Check | Note |
|---|---|---|
| S13.1 | `TestVerifyS13_01_TableOneLinePerRow` | |
| S13.2 | `TestVerifyS13_02_FreshCacheNoRequest` | Literal: default interval, cache 12 s old |
| S13.3 | `TestVerifyS13_03_OldCacheIsRequested` | Scaled: interval 10 s, cache 12 s old in place of "5 minutes ago" |
| S13.4 | `TestVerifyS13_04_RefreshForcesFetch` | Literal: default interval, cache 20 s old |
| S13.5 | `TestVerifyS13_05_ClosedMarketNotRequested` | B3 closed 2 h ago; US open as a control; interval 10 s |
| S13.6 | `TestVerifyS13_06_UnreachableShowsStaleWithAge` | Two variants: status 503, no connection. Scaled: interval 10 s |
| S13.7 | `TestVerifyS13_07_PipedNoColour` | |
| S13.8 | `TestVerifyS13_08_GroupNamesOnOwnLines` | |
| S13.9 | `TestVerifyS13_09_SilentSourceExitsWithin11s` | The stand-in holds every answer for 30 s; empty cache and cached quotes |
| S13.10 | `TestVerifyS13_10_BlockedRefreshSendsNothing` | 429, 403 and 401; `--refresh` 6 s after the refusal |
| S13.11 | `TestVerifyS13_11_FreshCacheShowsCachedQuotes` | The source's price changes between the runs |
| S13.12 | `TestVerifyS13_12_ClosedMarketShowsLastClose` | |
| S13.13 | `TestVerifyS13_13_FirstLineStartsWithIBOV` | |
| S13.14 | `TestVerifyS13_14_LastLineMarketStates` | |
| S13.15 | `TestVerifyS13_15_UnreachableOneErrorLine` | Two variants: status 503, no connection |
| S13.16 | `TestVerifyS13_16_FailedRowNotRequestedDuringWait` | Literal: default interval, 30 s after the failure |
| S13.17 | `TestVerifyS13_17_B3PriceFormat` | |
| S13.18 | `TestVerifyS13_18_USPriceFormat` | |
| S13.19 | `TestVerifyS13_19_IbovespaWholePoints` | |
| S13.20 | `TestVerifyS13_20_PricesEndAtSameColumn` | Also `R$ 1.038,42` |
| S13.21 | `TestVerifyS13_21_RateLimitErrorLine` | On the run that meets the refusal and on the next run, during the block |
| S13.22, S13.23 | `TestVerifyS13_22_23_TenPrintsOnEmptyCache` | Table and JSON. S13.22: every process shows a quote in each of the 5 rows. S13.23: exactly 1 request per row (a row is a watchlist line); the summary entries' counts are logged, not asserted (2026-11-24: 1 each). Not parallel |
| S13.24 | `TestVerifyS13_24_NoCacheUnreachableMarketsUnknown` | Two variants: status 503, no connection |

### S14 One-shot JSON

| Criterion | Check | Note |
|---|---|---|
| S14.1 | `TestVerifyS14_01_OneJSONDocument` | |
| S14.2, S14.3, S14.9, S14.10 | `TestVerifyS14_02_03_09_10_RowObject` | |
| S14.4 | `TestVerifyS14_04_StateOK` | |
| S14.5 | `TestVerifyS14_05_StatePartial` | One row's fetch answered 503. Scaled: interval 10 s |
| S14.6, S14.7 | `TestVerifyS14_06_07_NoCacheUnreachable` | Two variants: status 503, no connection |
| S14.8 | `TestVerifyS14_08_UnparsableConfigExit2` | Also 7.6: a valid document with state `error` on standard output |
| S14.11 | `TestVerifyS14_11_MarketStateClosed` | |
| S14.12 | `TestVerifyS14_12_PriceNotRounded` | |

### S15 The last quotes are kept between runs

| Criterion | Check | Note |
|---|---|---|
| S15.1 | `TestVerifyS15_01_QuotesKeptWithoutNetwork` | "No network" = the stand-in is stopped; its port refuses connections |
| S15.2 | `TestVerifyS15_02_FilesUnderHomeCache` | `XDG_CACHE_HOME` unset, `HOME` temporary |
| S15.3 | `TestVerifySlow_QR8_Scaled_CacheSizeTwentyMinutes` (SLOW), `TestVerifyLong_QR8_CacheSizeEightHours` (LONG) | Partial: `print` once per interval in place of the view, which is not built yet. See QR8 |
| S15.4, S15.7 | `TestVerifyS15_04_07_DamagedCacheIsAbsentAndSilent` | Every file in the cache folder emptied, or filled with garbage; compared with a run on no cache |
| S15.5 | not verified | About the full-screen view, which needs a terminal; outside this suite |
| S15.6 | not verified | About the full-screen view |
| S15.8, S15.9 | `TestVerifyS15_08_09_UnwritableCacheFolder` | For `print` only. Three ways of "cannot be written". Skipped when run as root |

### S17 The data source can be replaced

| Criterion | Check | Note |
|---|---|---|
| S17.1 | not verified | Needs the real source |
| S17.2 | `TestVerifySec7_4_RowAndSummaryFormat`, `TestVerifyS13_11_FreshCacheShowsCachedQuotes` | Every check runs against the stand-in with an unchanged config; these two assert the stand-in's own numbers in the rows |
| S17.3 | `TestVerifyS17_03_NothingSelectsASource` | `--help` and `print --help` name no source, URL or the testing variable; `--source` exits 2; a `source` key is an unknown key and the requests still reach the stand-in |

### Quality requirements

| Requirement | Check | Note |
|---|---|---|
| QR3 | `TestVerifyQR3_FreshCacheZeroRequestsWithin100ms` | As specified: 20 tickers, 3 summary entries, median of 10 runs. Runs alone (not parallel) |
| QR4 | `TestVerifySlow_QR4_Full_PrintEveryTwoSecondsTenMinutes` (SLOW); also `TestVerifyQR4_Scaled_PrintEveryTwoSeconds`, `TestVerifyS13_22_23_TenPrintsOnEmptyCache` | Full size without the view (not built yet): `print` every 2 s for 10 min, open market, interval 60 s, 23 entries; asserts at most 345 in total. Run of 2026-11-24: 300 calls, 230 requests, 10 per entry for every entry |
| QR5 | `TestVerifySlow_QR5_Full_And_W6_3_WaitDoubles` (SLOW); also `TestVerifyQR5_BlockUnderLoad`, `TestVerifyQR5_TenPrintsAgainstRefusingSource` | Full size without the view (not built yet): source refuses everything from the first request, `print --refresh` every 2 s for 16.5 min, 23 entries; any 10-minute window from the first refused answer. Run of 2026-11-24: 8 requests by the first call (all in flight at the first refusal), then one at +300.2 s and one at +901.9 s; at most 2 in a window counted after the first refusal |
| QR8 | `TestVerifySlow_QR8_Scaled_CacheSizeTwentyMinutes` (SLOW), `TestVerifyLong_QR8_CacheSizeEightHours` (LONG) | Quotes change on every answer (`vary_quotes`), open market, 23 entries, `print` every interval + 1 s so each call fetches; cache size (sum of the regular files) after the first full refresh and after every call, each within 10%. SLOW is scaled: 20 min at interval 10 s (2026-11-24: 110 samples, first 20979 bytes, min 20866, max 21273). The 8-hour run at interval 60 s is started separately (`BRL_VERIFY_LONG=1`): 2026-11-24 to 25: 8 h 0 min of open market, 472 samples, first 22150 bytes, min 21964 (−0.8%), max 22432 (+1.3%), 10856 requests after the first refresh (one per entry per call), PASS; evidence `verification/evidence/20261124T172137Z/` |
| QR7 | `TestVerifyQR7_ConfigNeverTouched` | Config folder and `config.toml` are both symbolic links. In addition every check compares the config file's checksum around every run |

### From the workflows, not numbered criteria

| Source | Check | Note |
|---|---|---|
| 6.5 row 3 | `TestVerifyW6_5_RefreshWithinFiveSecondsDoesNotFetch` | A forced fetch does not fetch a row attempted less than 5 s ago |
| 6.3 row 1 | `TestVerifyW6_3_RefusedRowsAreStale` | "the rows whose fetch was refused are stale", judged by the stand-in's record of which requests it refused |
| 6.5 (D73), QR4 | `TestVerifyS13_22_23_TenPrintsOnEmptyCache` | See S13.22, S13.23 |
| 6.5 row 1, 6.2 Not found, 7.3 Not found, 7.6 | `TestVerifyW6_5_NotFoundRow` | The stand-in answers 404 for one ticker: `not found` and `—` in the 6 other data columns, other rows unaffected, no error line, JSON `not_found` and state `partial`; not requested again after one interval nor by `--refresh`; requested again after the config changes. The end of the hourly wait is not verified |
| 6.3 row 6 | `TestVerifySlow_QR5_Full_And_W6_3_WaitDoubles` | SLOW. The wait doubles after a refused probe: first probe 300.2 s after the first refusal, next 601.7 s after the refused probe's answer; nothing in between. The cap of 30 minutes is not reached |
| 6.3 rows 4 and 5 | `TestVerifySlow_W6_3_RecoveryAfterBlock` | SLOW. Nothing sent 20 s, 150 s and 285 s after the refusal; quotes again after 5 min 8 s; the record shows how the tool resumed |
| 7.3, 7.4 | `TestVerifySec7_4_RowAndSummaryFormat` | The fields of a row and of the summary line, as in the spec's example |

### Not verified from outside

- Everything about the full-screen view: no terminal is attached; every run
  here is piped.
- Colour on a terminal (S16.1, S16.2, and S16.3 and S16.4 on a terminal): not
  checked here, for the same reason. Covered instead by the acceptance tests
  `TestS16_1_*` to `TestS16_4_*` and `TestW7_3_DayChange_ZeroHasNoColour`
  (`internal/app/print_shared_acceptance_test.go`), and by a hand check: run
  `script -qc "brl-stocks print" /dev/null | cat -v` against the stand-in and
  look for `^[[32m` on a gain and `^[[31m` on a loss, then the same with
  `NO_COLOR=1` and none. Done on 2026-11-24: as expected.
- "Closed for more than 30 minutes" (S13.5) is tested with 2 hours. The period
  right after the close (6.1) is not explored.
- The end of the hourly wait of a not-found row (6.5 row 1) and waits longer than one
  interval after repeated failures (6.2): they need more real time.

## Feature map, increment 2 (`surface_verify_test.go`)

The 63 criteria of increment 2 in `research/build/one-shot/scope.md`, each
read on `print` as section 8's reading table says, plus QR1, QR10 and QR12.
These rows take precedence over the earlier rows that called S3.6, S11.13 and
S11.18 "not verified". With these checks the ordinary run takes about 1.5
minutes. Run of 2026-11-25: 40 ordinary checks, 39 pass; of the 63 criteria,
62 pass and S6.9 fails (below).

"QR1" in a note means the check runs twice: with its own config and with the
config padded to 20 tickers and the default 3 summary entries
(`.../own_rows/...` and `.../20_tickers_QR1/...`).

### S1 Row columns and number formats

| Criterion | Check | Note |
|---|---|---|
| S1.2 | `TestVerifyS1_02_08_15_RowFormats/*/S1_2` | Header columns, and PETR4 and AAPL rows field by field; quote time `HH:MM`. QR1 |
| S1.5 | `TestVerifyS1_05_NoCacheNoQuoteDashes` | "Until its first quote arrives" read as "no quote arrives during the call": no cache, status 503 and no connection. Every column after the ticker is `—` (7 of them) |
| S1.7 | `TestVerifyS1_07_EarlierDayQuoteShowsDate` | US closed 2 days 1 hour ago; `DD/MM` of that day (child has `TZ=UTC`); B3 open as control (`HH:MM`) |
| S1.8, S1.9 | `TestVerifyS1_02_08_15_RowFormats/*/S1_8`, `/S1_9` | Stand-in prices 1038.42 (BBDC4) and 1229.87 (NVDA). QR1 |
| S1.10 to S1.13 | `TestVerifyS1_02_08_15_RowFormats/*/S1_10` ... `/S1_13` | PETR4 +0.31 and +0.81 %, AAPL -0.48 %, VALE3 zero. QR1 |
| S1.14 | `TestVerifyS1_02_08_15_RowFormats/*/S1_14` | Stand-in price 38.425 (ABEV3). QR1 |
| S1.15 | `TestVerifyS1_02_08_15_RowFormats/*/S1_15` | The low column and one currency symbol per row. QR1 |

### S2 Summary line

| Criterion | Check | Note |
|---|---|---|
| S2.1, S2.5 to S2.8 | `TestVerifyS2_01_05_08_DefaultSummaryLine/S2_1` ... `/S2_8` | One run without a `summary` key; the first line, above the header line |
| S2.2 | `TestVerifyS2_02_SummaryOnlyUSDBRL` | |
| S2.3 | `TestVerifyS2_03_EmptySummaryNoLine` | The header line is the first line with text |

### S3.6, S4 and S9.1

| Criterion | Check | Note |
|---|---|---|
| S3.6 | `TestVerifyS3_06_NoConversion` | Table (`R$ 38,42`, `US$ 229.87`) and JSON `currency` and `price` equal the stand-in's |
| S4.1 | `TestVerifyS4_01_03_S9_01_GroupsAndOrder/*/S4_1`, `TestVerifyS4_01_GroupOrderFollowsFile` | Brasil then US, and US then Brasil, as in the file. QR1 |
| S4.2 | `TestVerifyS4_02_NoGroupNoHeader` | Every line between the header line and the last line is a row. QR1 |
| S4.3 | `TestVerifyS4_01_03_S9_01_GroupsAndOrder/*/S4_3` | QR1 |
| S9.1 | `TestVerifyS4_01_03_S9_01_GroupsAndOrder/*/S9_1` | File order chosen to differ from both other sort orders. QR1 |

### S5 Closed markets, stale marks, interval floor

| Criterion | Check | Note |
|---|---|---|
| S5.4, S5.5 | `TestVerifyS5_04_05_USClosed` | US closed 2 h ago; the last line of the table |
| S5.6 | `TestVerifyS5_06_ClosedMarketNoStaleByAge` | Scaled: the US quote is two days old, but its last successful fetch was 25 s ago (more than two intervals of 10 s), not two days ago. Two days cannot pass in a check, and the cache was not edited. Table and JSON `status` |
| S5.10 | `TestVerifyS5_10_IntervalFloor` | As scope.md says: `refresh_seconds = 3`, `print` every 2 s for 32 s; the smallest gap between two requests for a symbol must be at least 10 s. 2026-11-25: 3 requests per row, 12 s apart |
| S5.12 | `TestVerifyS5_12_FailedBeforeCloseStaysStale` | Stand-in control `open_end_unix`: the session ends 22 s after the first call; 503 from +12 s; at +27 s the last line reads `B3 closed · US closed` and the rows are stale (JSON `stale`, `closed`) |
| S5.9, S5.11 | not in this increment | Wait on Q62 (scope.md) |

### S6 Failed fetches

| Criterion | Check | Note |
|---|---|---|
| S6.1, S6.2, S6.3 | `TestVerifyS6_01_02_03_09_FailedRefresh/*/*/S6_1`, `/S6_2`, `/S6_3` | Scaled: interval 10 s, cache 12 s old. Status 503 and no connection. S6.3 counts the error-stream lines that start with a cause phrase. QR1 |
| S6.5, S6.6 | `TestVerifyS6_05_06_RecoveryClearsMarks` | Two calls (scope.md): a failing one, then 12 s later (past the wait of one interval) one that fetches every row with success |
| S6.8 | `TestVerifyS6_08_OnlyFailedRowStale` | VALE3 answered 503, the rest 200. QR1 |
| S6.9 | `TestVerifyS6_01_02_03_09_FailedRefresh/*/*/S6_9` (FAILS), `TestVerifyS6_09_OneRowNotUpdated` (passes) | With every row failing, the line counts the summary entries as rows: `source unreachable: 8 rows not updated · next attempt 00:25` for 5 rows (23 for 20). With one row failing it says `1 row`. Cause and `HH:MM` are right |
| S6.10 | `TestVerifyS6_10_StaleMarkAge`; literal: `TestVerifySlow_S6_10_TwelveMinutes` (SLOW) | Scaled: 12 s, mark `!12s`. The 12-minute check (`BRL_VERIFY_SLOW=1 ... -run TestVerifySlow_S6_10`, about 13 min) has not been run yet. By hand: as the slow check does, `print`, then make the source fail, wait 12 minutes, `print` again and look for `!12m` |
| S6.11 | `TestVerifyS6_11_SummaryEntryFails` | Added with D81. Empty cache, every ticker answers, `^GSPC` answered 503; one cause line starting `source unreachable` that holds `summary not updated` and no number (only the `HH:MM`) |

### S7 Forced fetch (`print --refresh` for `r`)

| Criterion | Check | Note |
|---|---|---|
| S7.1 | `TestVerifyS7_01_RefreshRequestsB3Within2s` | Default interval, cache 6 s old; measured from just before the process starts to the stand-in's arrival time. 2026-11-25: 8 ms |
| S7.2 | `TestVerifyS7_02_RefreshDuringWait` | Scaled: one failure puts the next attempt 60 s away, not 4 min; a plain `print` in between sent nothing (control) |
| S7.3, S7.4 | `TestVerifyS7_03_04_RefreshDuringBlock/{429,403}` | S7.4: the cause line's `HH:MM` is the first refused answer + 5 min (or the next minute) |
| S7.6 | `TestVerifyS7_06_RefreshRequestsClosedMarket` | B3 closed 2 h ago |
| S7.7 | `TestVerifyS7_07_SecondRefreshWithinTwoSeconds` | |
| S7.8 | `TestVerifyS7_08_RefreshSkipsNotFound` | Scaled: tried 12 s ago, not 10 min |

### S8 Not-found rows

| Criterion | Check | Note |
|---|---|---|
| S8.1, S8.2, S8.5 | `TestVerifyS8_01_02_05_NotFound/*/S8_1`, `/S8_2`, `/S8_5` | Stand-in answers 404 for XXXX9. QR1 |
| S8.4 | `TestVerifyS8_04_ConfigChangeRetriesNotFound` | Two calls with a change of content in between (AAPL added); a call on the unchanged config in between is the control |

### S11, S14, S15, S17, S18

| Criterion | Check | Note |
|---|---|---|
| S11.13, S11.18 | `TestVerifyS11_13_18_TwentyFiveTickers` | Warning observed: `25 tickers; the tool is designed for 20` |
| S14.2, S14.9, S14.10 | `TestVerifyS14_02_09_10_RowObject` | S14.9 also the summary entries; S14.10 every time in the document |
| S14.3 | `TestVerifyS14_03_SchemaVersion` | States `ok` and `error`, and a config error (exit 2) |
| S14.8 | `TestVerifyS14_08_UnparsableConfigJSONExit2` | |
| S14.11 | `TestVerifyS14_11_ClosedMarketState` | B3 open as control |
| S15.2 | `TestVerifyS15_02_KeptFilesUnderHomeCache` | `XDG_CACHE_HOME` unset; every file left in the check's folders. Observed: `cache.v1.json` and `lock` |
| S15.6 | `TestVerifyS15_06_RemovedTickerForgotten` | "No network": the stand-in stopped |
| S15.8 | `TestVerifyS15_08_UnwritableCacheOneWarning` | `print` and `print --json`, folder mode 0555 |
| S17.4 | `TestVerifyS17_04_HelpHidesSourceVariable` | `print --help` and `--help` |
| S18.1, S18.2 | `TestVerifyS18_01_PrintVersion`, `TestVerifyS18_02_PrintHelp` | With `print` (D77) |
| S18.3, S18.4 | `TestVerifyS18_03_04_PrintUnknownFlag` | `print --frobnicate`, `print -z` |

### Quality requirements

| Requirement | Check | Note |
|---|---|---|
| QR1 | the checks marked QR1 above | Run with 20 tickers and 3 summary entries: row formats (S1.2, S1.8 to S1.15), groups and order (S4.1 to S4.3, S9.1), failed fetches (S6.1 to S6.3, S6.8, S6.9), not-found rows (S8.1, S8.2, S8.5). Earlier: QR3, QR5. Not every criterion: the rest run with 2 to 6 tickers |
| QR10 | not verified | Measured by S17.1, which needs the real source. By hand, on a machine with network (this calls Yahoo): `mise exec -- go build -o /tmp/brl-check/brl-stocks ./cmd/brl-stocks`, `mkdir -p /tmp/brl-check/home/.config/brl-stocks`, `printf 'tickers = ["PETR4", "AAPL"]\n' > /tmp/brl-check/home/.config/brl-stocks/config.toml`, then `env -i PATH=/usr/bin:/bin HOME=/tmp/brl-check/home /tmp/brl-check/brl-stocks print; echo "exit $?"` and the same with `print --json`. Expected: both rows with prices, no key or account asked for, exit 0 |
| QR12 | `TestVerifyQR12_PrivacyOnTheWire` | `print`, `print --json`, `print --refresh` and `--config <other path> print`, with a not-found row; `USER`/`LOGNAME` set to a marker; `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY` (both cases) point at a local trap that records and refuses every connection. Asserted: only `GET /v8/finance/chart/<requested symbol>`, query only `interval` and `range`, no body, `Host` the source, no cookie, authorization, referer or forwarding headers, and nothing carrying the markers, the verifier's user and host name, the check's folders or the config's group name; the trap received no connection. 2026-11-25: headers `Accept-Encoding: gzip` and `User-Agent: Mozilla/5.0 (compatible; brl-stocks)` only. Does NOT prove: that the tool honours proxy settings at all (a source address that is not loopback would be needed to show it, and the checks refuse one), traffic that ignores proxy settings, name lookups (none are needed for `127.0.0.1`), or what is sent to the real source over TLS. By hand: run `brl-stocks print` against the real source while recording, e.g. `sudo tcpdump -i any -n 'not host 127.0.0.1'` (all destinations and DNS) or `strace -f -e trace=connect brl-stocks print` (neither tool is installed here) |
