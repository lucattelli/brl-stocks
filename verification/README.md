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
| S13.5 | `TestVerifyS13_05_ClosedMarketNotRequested/{b3_ended_101min_ago,b3_ended_2h_ago,us_ended_6min_ago}` | Reworded by D111 (2026-11-27): past the settle time, 100 min for B3, 5 for the US. The period ends that long before every answer; the other market open as a control; interval 10 s. Before: B3 closed 2 h ago |
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
- S13.5 (since D111) is tested just past the settle times (101 min for B3,
  6 min for the US) and at 2 h; the settle time itself is in the view's checks
  (S5.8, S5.14).
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

## The full-screen view, slice 2 increment 1 (`view_verify_test.go`)

The 35 criteria of increment 1 in `research/build/full-screen-view/scope.md`,
plus QR1, QR2, QR7 and QR12, checked on the real view. Written from
`docs/spec.md` (6, 7, 8's opening, the criteria, QR1, QR2, QR7, QR12), the
scope and this folder, without reading the implementation. These rows take
precedence over the earlier line "Everything about the full-screen view: no
terminal is attached" for the criteria below. Run of 2026-11-25: the ordinary
run of all files takes about 1 min 45 s; 117 top-level checks pass, 0 fail, 6
skipped (SLOW and LONG); the 21 view checks pass, and so do the 35 criteria.

```sh
# only the view's checks (about 30 s)
mise exec -- go test -tags verification ./verification/ -run TestVerifyView -v
```

### How the view is driven

- `pty_linux_test.go` opens a pseudo-terminal with the standard library only:
  `/dev/ptmx`, unlock (`TIOCSPTLCK`), the slave's number (`TIOCGPTN`), the size
  with `TIOCSWINSZ` (120 x 40), and the mode read with `TCGETS` into
  `syscall.Termios`. The slave is the child's stdin, stdout and stderr, and the
  child is the leader of a new session with the slave as its controlling
  terminal (`Setsid`, `Setctty`). The child's environment is that of the
  one-shot checks (`newEnv`) plus `TERM=xterm-256color`. Linux only (build tags
  `verification && linux`).
- Everything the child writes is read from the master and fed into a terminal
  emulator, `github.com/charmbracelet/x/vt` v0.1.0 (the only library added, used
  only here), of the same size. What the emulator answers to the tool's queries
  is written back to the master, as a real terminal does. The checks read the
  emulator's cells: text, and each cell's foreground and background colour.
- Keys are bytes written to the master: `q`, and `0x03` for Ctrl+C.
- `waitFor` polls the emulated screen (on every write, and every 20 ms) until a
  condition holds, with a real-time deadline that fails the check and puts the
  last screen into the evidence; `waitStable` also waits until the screen has
  not changed for 300 or 500 ms. No fixed sleep is the only wait.
- After the key, `finish` waits at most 5 s for the exit, then until the
  master has been quiet for 150 ms, and records: the exit status, the termios
  before and after, whether the output entered and left the alternate screen
  (the escape sequences, and the emulator's state), whether the cursor is
  shown, any terminal mode the tool switched on and left on, the screen after
  the exit, and the stand-in's record for the run. Every view run compares the
  config's checksum before and after (QR7).
- Evidence: `evidence/<run>/TestVerifyView_*.txt`, with the screen before the
  key (numbered lines), the colours of the change cells, all of the above, and
  each criterion's `CRITERION` / `RESULT` lines.
- The terminal checks were shown to be able to fail: a throwaway check that
  killed the view with SIGKILL in place of `q` saw the alternate screen still
  on, the cursor hidden and the termios changed (not kept in the suite).

### Feature map

"QR1" in a note means the check runs twice: with its own config and with the
config padded to 20 tickers and the default 3 summary entries.

| Criterion | Check | Note |
|---|---|---|
| S1.1 | `TestVerifyView_S1_1_OneRowPerTicker` | `PETR4`, `AAPL`; one line each with a price. The view uses the alternate screen |
| S1.2 | `TestVerifyView_S1_2_S1_8_15_S16_1_2_RowFormatsAndColour/*/S1_2` | 120 columns read as "full width". Header and PETR4, AAPL field by field; `HH:MM`. No sparkline column yet (slice 4). QR1 |
| S1.3 | `TestVerifyView_S1_3_QuitLeavesTerminal/{q,ctrl_c}` | Exit 0; alternate screen left; cursor shown; termios identical to before the run (`iflag`, `oflag`, `cflag`, `lflag`, speeds, `cc`). 2026-11-25: all hold for both keys, exit about 340 ms after start, no mode left on |
| S1.4 | `TestVerifyView_S1_4_CachedRowsBeforeAnswer` | Cache left by `print`, 12 s old (interval 10 s); the stand-in then holds every answer 20 s and would answer other prices. Cached prices on screen at +29 ms with 8 requests sent and none answered |
| S1.5 | `TestVerifyView_S1_5_NoCacheDashes` | No cache, answers held 20 s: every column after the ticker is `—` (7 of them); status line `B3 unknown · US unknown` |
| S1.7 | `TestVerifyView_S1_7_EarlierDayDate` | US closed 2 days 1 hour ago: `DD/MM`; B3 `HH:MM` as control (`TZ=UTC`) |
| S1.8 to S1.15 | `TestVerifyView_S1_2_S1_8_15_S16_1_2_RowFormatsAndColour/*/S1_8` ... `/S1_15` | Stand-in prices as in the one-shot checks (BBDC4 1038.42, NVDA 1229.87, ABEV3 38.425 ...). QR1 |
| S3.1 to S3.5 | `TestVerifyView_S3_1_5_MarketOfATicker/S3_1` ... `/S3_5` | Nine tickers in one config. "Treated as B3/US" is read on the view as the currency of the price (`R$`/`US$`); the symbols asked of the stand-in are in the evidence (`PETR4F.SA`, `ABCD1`, `XYZ.SA` ...). Rows show the code without the prefix |
| S3.6 | `TestVerifyView_S3_6_NoConversion` | PETR4 and AAPL price, low and high equal the stand-in's |
| S3.7 | `TestVerifyView_S3_7_LowerCaseShownUpper` | `petr4` drawn as `PETR4`; `petr4` nowhere on screen |
| S11.2 | `TestVerifyView_S11_2_ConfigFlag` | `--config <other>`; the watchlist at the default path is not shown |
| S15.1 | `TestVerifyView_S15_1_QuotesKeptWithoutNetwork` | Both runs are the view; "no network" = the stand-in stopped |
| S15.4, S15.7 | `TestVerifyView_S15_4_7_DamagedCache/{emptied,garbage}` | The cache left by a view run is damaged; with answers held, the screen equals that of a run on no cache; the word "cache" is not on the screen before or after the exit (the view's error stream is the terminal) |
| S16.1, S16.2 | `TestVerifyView_S1_2_S1_8_15_S16_1_2_RowFormatsAndColour/*/S16_1`, `/S16_2` | Every cell of the change value and percent: PETR4 palette 2 (green), AAPL palette 1 (red); palette 10 and 9 (bright) would also pass. A zero change (VALE3) has no colour (7.3, observed). QR1 |
| S16.3, S16.4 | `TestVerifyView_S16_3_4_NoColor/*/S16_3`, `/S16_4` | `NO_COLOR=1`: no SGR colour parameter (30-38, 40-48, 90-97, 100-107) anywhere in the output, no cell with a colour; changes start with `+`/`-`. QR1 |
| S17.2 | `TestVerifyView_S17_2_StandInQuotes` | Distinctive stand-in prices (PETR4 12.34, MSFT 98.76) in the rows of the unchanged `baseConfig` |
| S17.4 | `TestVerifyView_S17_4_HelpHidesSourceVariable` | `brl-stocks --help`, a plain command (no terminal) |
| S18.1, S18.2 | `TestVerifyView_S18_1_Version`, `TestVerifyView_S18_2_Help` | Without `print` (D77), plain commands; no rows drawn, no request |
| S18.3, S18.4 | `TestVerifyView_S18_3_4_UnknownFlag` | `--frobnicate` and `-z` without `print` |

| Requirement | Check | Note |
|---|---|---|
| QR1 | the checks marked QR1 above | Row formats, colour and `NO_COLOR` with 20 tickers. The other view checks run with 1 to 9 tickers |
| QR2 | `TestVerifyView_QR2_CachedRowsWithin300ms` | As specified: 20 tickers, cache left by `print`, answers then held so every price comes from the cache; from just before the process starts to the moment all 20 rows show a price on the emulated screen; median of 10 runs. Not parallel. 2026-11-25: median 28 ms (27 to 30 ms). Measures until the bytes reach the emulator, not a real terminal's paint |
| QR7 | `TestVerifyView_QR7_ConfigNeverTouched`, and every view run | Config folder and file both symbolic links; three view runs (`q`, Ctrl+C, `--config` naming the link); links, target, checksum, mtime and folder entries unchanged |
| QR12 | `TestVerifyView_QR12_PrivacyOnTheWire` | Same recording, markers and proxy trap as `TestVerifyQR12_PrivacyOnTheWire`, for two view runs (default path, `--config` elsewhere) with a not-found ticker. 2026-11-25: 14 requests, headers `Accept-Encoding: gzip` and `User-Agent: Mozilla/5.0 (compatible; brl-stocks)` only; no connection to the trap. Does not prove what that check does not prove (proxy honoured at all, traffic outside proxies, DNS, TLS to the real source) |

### Not verified from outside, and the hand checks

- **The look on a real terminal** (layout, alignment, colours in the user's
  palette, that the screen comes back as it was): the emulator is one terminal,
  not the user's. "The terminal palette's green" is verified as palette index 2;
  the hue is the terminal's. By hand, on a real terminal:

  ```sh
  mkdir -p /tmp/brl-view/cfg/brl-stocks /tmp/brl-view/cache
  mise exec -- go build -o /tmp/brl-view/brl-stocks ./cmd/brl-stocks
  mise exec -- go build -o /tmp/brl-view/standin ./verification/standin
  /tmp/brl-view/standin -addr 127.0.0.1:18765 &
  printf 'tickers = ["PETR4", "AAPL"]\n[[group]]\nname = "Brasil"\ntickers = ["VALE3", "ITUB4"]\n' > /tmp/brl-view/cfg/brl-stocks/config.toml
  stty -a > /tmp/brl-view/stty.before
  BRL_STOCKS_SOURCE_URL=http://127.0.0.1:18765 XDG_CONFIG_HOME=/tmp/brl-view/cfg XDG_CACHE_HOME=/tmp/brl-view/cache /tmp/brl-view/brl-stocks; echo "exit $?"
  stty -a | diff /tmp/brl-view/stty.before - && echo "terminal mode unchanged"
  ```

  Expected: PETR4 `+0,31 +0,81%` in green, AAPL `-1.12 -0.48%` in red, VALE3
  `+0,00` uncoloured; `q` (and, on a second run, Ctrl+C) gives `exit 0`, the
  shell's earlier output back, a visible cursor, typing echoes, and no `stty`
  difference. With `NO_COLOR=1` before the command: no colour. Kill the
  stand-in afterwards (`kill %1`).
- **S1.3 on other terminals** than the emulator: the termios and escape checks
  hold for any terminal, but a terminal that reacts differently to the tool's
  queries is not covered. Hand check above, in the terminals the user has.
- **S15.1 with a real network outage**: "no network" is a stopped stand-in.
- **S17.1 / QR10** (the real source) for the view: as the QR10 hand check
  above, with `brl-stocks` in place of `print`, on a terminal.

### Increment 2: summary line, groups and order (`view_increment2_verify_test.go`)

The 12 criteria of increment 2 in `research/build/full-screen-view/scope.md`,
on the real view with the helpers above (`openSettled` opens the view, waits
for every row with a price and a settled screen, and quits with `q`). Run of
2026-11-25: the ordinary run of all files, about 1 min 45 s; 124 top-level
checks pass, 0 fail, 6 skipped; the 7 new checks and the 12 criteria pass.
"QR1" as above.

| Criterion | Check | Note |
|---|---|---|
| S2.1 | `TestVerifyView_S2_1_5_8_DefaultSummaryLine/S2_1` | No `summary` key; the first line with text is above the column header and holds `IBOV`, `S&P 500` and `USD/BRL`, each with a value and a percent |
| S2.5 to S2.8 | `TestVerifyView_S2_1_5_8_DefaultSummaryLine/S2_5` ... `/S2_8` | Stand-in: ^BVSP 158432.10 (+0.32 %), ^GSPC 6812.40, BRL=X 5.4321. Observed `IBOV 158.432 +0,32%    S&P 500 6,812.40 -0.11%    USD/BRL 5,4321 +0,08%` |
| S2.2 | `TestVerifyView_S2_2_SummaryOnlyUSDBRL` | The first line is the USD/BRL entry alone; `IBOV` and `S&P 500` nowhere on the screen |
| S2.3 | `TestVerifyView_S2_3_EmptySummaryNoLine` | None of the three labels on the screen; the column header is the first line with text |
| S4.1 | `TestVerifyView_S4_1_3_S9_1_GroupsAndOrder/*/S4_1`, `TestVerifyView_S4_1_GroupOrderFollowsFile` | Brasil then US, and US then Brasil, as in the file; each row between its header and the next. QR1 (fillers go into the last group) |
| S4.2 | `TestVerifyView_S4_2_NoGroupNoHeader` | Every line between the column header and the status line is a row. QR1 |
| S4.3 | `TestVerifyView_S4_1_3_S9_1_GroupsAndOrder/*/S4_3` | The top-level rows are the first lines below the column header. QR1 |
| S4.4 | `TestVerifyView_S4_4_TickerInTwoGroupsFetchedOnce` | "A refresh" in this slice is the view's fetch at start (empty cache): exactly 1 request for `PETR4.SA`; PETR4 shown in both groups (7.2). The refreshes of slice 3 are not covered. QR1 |
| S9.1 | `TestVerifyView_S4_1_3_S9_1_GroupsAndOrder/*/S9_1` | File order chosen to differ from both other sort orders; status line `sort: file order`. QR1 |

Not verified from outside, for these: how the summary line and group headers
look on a real terminal; the hand check above shows them (its config has a
group).

### Increment 3: market states, stale rows, the error line, not-found rows (`view_increment3_verify_test.go`)

The 15 criteria of increment 3 in `research/build/full-screen-view/scope.md`,
on the real view. In this slice the view fetches once when it opens, so "a
refresh fails" is that fetch at start failing; the cache is left by `print`
before it. The error line is a screen line that starts with one of the four
cause phrases of 7.3; the status line is the last line with text. Run of
2026-11-25: the ordinary run of all files, about 2 min; 131 top-level checks
pass, 0 fail, 7 skipped (SLOW and LONG); the 8 new ordinary checks and the 15
criteria pass. "Scaled" and "QR1" as above.

| Criterion | Check | Note |
|---|---|---|
| S5.4, S5.5 | `TestVerifyView_S5_4_5_USClosed` | US closed 2 h ago: AAPL and MSFT show the close and its time, no stale mark; status line `B3 open · US closed · sort: file order` |
| S5.6 | `TestVerifyView_S5_6_S6_7_S6_10_CachedAges/S5_6` | Scaled: the last successful fetch of the closed US rows was 25 s ago (more than two intervals of 10 s), not two days; the source holds its answers. No stale mark on the US rows |
| S5.12 | `TestVerifyView_S5_12_FailedBeforeCloseStaysStale` | `open_end_unix` ends the session at +22 s; a view run at +12 s meets 503 (error line); a view run at +27 s with answers held reads `B3 closed · US closed` and every row is stale |
| S6.1, S6.2, S6.3, S6.9 | `TestVerifyView_S6_1_2_3_9_FailedFetch/*/*/S6_1` ... `/S6_9` | Scaled: interval 10 s, cache 12 s old; status 503 and no connection. Last prices kept; every row `!12s` (± 2 s of the real age); one error line, e.g. `source unreachable: 5 rows not updated · next attempt 03:22` (20 with QR1); the count is the rows, not the summary entries. QR1 |
| S6.7 | `TestVerifyView_S5_6_S6_7_S6_10_CachedAges/S6_7` | Scaled: last success 25 s ago, no failure since, answers held (no answer arrived during the run): the open B3 rows are stale. "Woke from sleep" is not simulated |
| S6.8 | `TestVerifyView_S6_8_OnlyFailedRowStale` | Cache 12 s old; VALE3 answered 503: only VALE3 stale; error line `source unreachable: 1 row not updated · ...`. QR1 |
| S6.10 | `TestVerifyView_S5_6_S6_7_S6_10_CachedAges/S6_10`; literal: `TestVerifySlow_View_S6_10_TwelveMinutes` (SLOW) | Scaled: 25 s, mark `!25s`. The 12-minute check (`BRL_VERIFY_SLOW=1 ... -run TestVerifySlow_View_S6_10`, about 13 min) has not been run yet |
| S6.11 | `TestVerifyView_S6_11_SummaryEntryFails` | Empty cache, `^GSPC` 503: `source unreachable: summary not updated · next attempt 03:22`, no count |
| S8.1, S8.2, S8.5 | `TestVerifyView_S8_1_2_5_NotFound/*/S8_1`, `/S8_2`, `/S8_5` | Stand-in 404 for XXXX9: `not found` and `—` in the other columns; the other rows fresh; no error line. QR1 |

Not verified from outside, for these: the later refreshes while the view
stays open (slice 3); how the marks and the error line look on a real
terminal (the hand check above, with the stand-in switched to
`curl -X POST -d '{"status":503}' http://127.0.0.1:18765/_control` after a
first run and before a second one).

### Increment 4: config, cache and source at start (`view_increment4_verify_test.go`)

The 21 criteria of increment 4 in `research/build/full-screen-view/scope.md`,
on the real view. On the view the error stream is the terminal: a message at
start is read from everything the tool wrote (escape sequences removed), and
the screen after the exit is in the evidence. Warnings are read from the
status line (7.2). `openViewSized` opens the view on another size (S12 uses
200 columns, so that the long temporary paths fit). Run of 2026-11-25: the
ordinary run of all files, about 2 min; 144 top-level checks pass, 0 fail, 7
skipped (SLOW and LONG); the 13 new checks and the 21 criteria pass, and so do
the one-shot checks of S15.8 and S15.9 after the change to the engine.

| Criterion | Check | Note |
|---|---|---|
| S11.1 | `TestVerifyView_S11_1_DefaultPathUnderHome` | `XDG_CONFIG_HOME` unset; a different watchlist in the folder XDG would name is not shown |
| S11.5, S11.7, S11.8 | `TestVerifyView_S11_5_7_8_ConfigThroughLink` | `~/.config/brl-stocks` a link to a folder elsewhere; three view runs (`q`, Ctrl+C, `--config` through the link); after each: still a link to the same target, the file byte-identical. For `print`: `TestVerifyQR7_ConfigNeverTouched` |
| S11.9, S11.16 | `TestVerifyView_S11_9_16_InvalidConfig/*` | Unparsable, wrong type, unknown summary name, empty ticker; default path and `--config`. Exit 2 without a key, no request; the message names the file and the problem, e.g. `brl-stocks: invalid config <path>: unknown summary entry "DAX" (allowed: IBOV, SP500, USDBRL)`, and stays on the main screen after the exit |
| S11.12, S11.17 | `TestVerifyView_S11_12_17_UnknownKey` | Status line `... · unknown config key "colour_scheme"`; every row shown |
| S11.13, S11.18 | `TestVerifyView_S11_13_18_TwentyFiveTickers` | 25 rows on a 40-line screen; status line `... · 25 tickers; the tool is designed for 20` |
| S11.15 | `TestVerifyView_S11_15_XDGConfigHome` | A different watchlist at `~/.config` is not shown |
| S12.1, S12.2, S12.3 | `TestVerifyView_S12_1_3_NoConfig/{xdg_set,xdg_unset}` | The example rows in the order of 7.7 and the default summary line; nothing created under the config folder. Changed in increment 6 for D95: `xdg_unset` (path under the home folder) runs at 120 columns and wants `~/.config/brl-stocks/config.toml`; `xdg_set` (a folder outside the home folder) runs at 200 columns and wants the full path. Status line since D95: `... · no config: example watchlist; put yours at <path>` |
| S12.2 at 120 columns | `TestVerifyView_S12_2_PathAt120Columns` | Observation only, with the path outside the home folder (not shortened by D95). Before increment 6 the warning was cut at the screen's width; how it is shortened and cut is now decided (D95) and checked by S12.6 to S12.8 below |
| S12.4, S12.5 | `TestVerifyView_S12_4_5_ConfigFlagMissingFile` | Exit 2, no request; `brl-stocks: cannot read the config <path>: open <path>: no such file or directory` |
| S15.2 | `TestVerifyView_S15_2_FilesUnderHomeCache` | `XDG_CACHE_HOME` unset: the new files are `~/.cache/brl-stocks/cache.v1.json` and `lock` |
| S15.5, S15.8 | `TestVerifyView_S15_5_8_UnwritableCache/*` | Empty folder 0555; parent 0555 and folder missing; quotes kept, then folder and files read-only (the stand-in's price changed, so the new price shows the view fetched). Quotes shown, requests sent; exactly one screen line about the cache: the status line `... · quotes are not kept: <folder>` (cut at 120 columns, as above). Skipped as root |
| S15.6 | `TestVerifyView_S15_6_RemovedTickerForgotten` | Three view runs: PETR4 and AAPL; AAPL alone 12 s later (it fetched AAPL); both again with the stand-in stopped: PETR4 shows `—` in the 7 data columns |
| S17.3 | `TestVerifyView_S17_3_NothingSelectsASource` | `brl-stocks --help` names no source, URL, provider or variable; `brl-stocks --source <url>` exits 2; a `source` key: status line `... · unknown config key "source"`, and the requests still reach the stand-in |

Not verified from outside, for these: how a long warning looks on the user's
terminal at its own width (hand check above, without a config file, at 80 and
at 120 columns).

### Increment 5: narrow terminal and keys (`view_increment5_verify_test.go`)

The 11 criteria of increment 5 in `research/build/full-screen-view/scope.md`
and QR11, on the real view. Resizes are real: the emulator is resized, then
the pseudo-terminal (`TIOCSWINSZ`, after which the kernel signals the tool),
and `SIGWINCH` is also sent. Keys are real bytes: arrows as a terminal sends
them (`ESC [ A`/`B`, or `ESC O A`/`B` if the tool switches on application
cursor keys; it did not), `j`, `k`, `s`, `?`. `pressAndSettle` presses a key
and waits for the screen to change and settle, or 2 s for no change. Run of
2026-11-25: the ordinary run of all files, about 2 min; 152 top-level checks
pass, 0 fail, 7 skipped (SLOW and LONG); the 8 new checks, the 11 criteria and
QR11 pass.

Open questions when this was written, answered since: Q84 by D92 (left and
right arrows do nothing) and Q85 by D93 (scrolling moves by one screen line,
row or group header). Their checks are in increment 6.

| Criterion | Check | Note |
|---|---|---|
| S1.6 | `TestVerifyView_S1_6_NarrowTerminal/{started_at_40,resized}` | Started at 40 columns, and started at 120 then resized to 100, 80, 70, 60, 50, 40 and back to 120: every row keeps ticker, price, percent and `HH:MM`, and no line is wider than the screen. Observed drop order (7.3): low and high at 70, name at 50, change value at 40; no sparkline column yet. Observed at 40 before increment 6: the summary line was cut (`IBOV 158.432 +0,32%    S&P 500 6,812.40`; USD/BRL gone); since D96 the narrow summary line is checked by S2.9 and S2.10 |
| S2.4 | `TestVerifyView_S2_4_S9_6_7_Scroll/S2_4` | 25 rows, 120 x 15: the summary line stays the first line through every scroll |
| S9.2, S9.3, S9.4 | `TestVerifyView_S9_2_4_SortCycle/S9_2` ... `/S9_4` | No groups; file order VALE3 ITUB4 MSFT AAPL PETR4. `s`: PETR4 MSFT VALE3 AAPL ITUB4 (`sort: day change`); `s`: by ticker (`sort: ticker`); `s`: file order |
| S9.5, S9.10 | `TestVerifyView_S9_5_10_SortInsideGroups` | Groups US then Brasil; after `s`: `US MSFT AAPL Brasil PETR4 VALE3 ITUB4` |
| S9.6, S9.7 | `TestVerifyView_S2_4_S9_6_7_Scroll/S9_6`, `/S9_7` | No groups, where every screen line is a row (still valid for the wording of D93). Down arrow, `j`, `j`: first row 2, 3, 4; up arrow, `k`: 3, 2. With group headers: increment 6 |
| S9.8 | `TestVerifyView_S9_8_Help` | A line for each of `q` quit, `r` refresh, `s` sort, `↑ / k` and `↓ / j` scroll, `?` help. Observed (7.3): `x` closes it and the list is back as before |
| S9.9 | `TestVerifyView_S9_9_OtherKeysDoNothing` | `x`, `z`, `5`, space, Tab, Enter: the screen identical, each watched for 2 s (an absence of change can only be watched over a time) |
| Q84 (observation) | `TestVerifyView_S2_4_S9_6_7_Scroll` (evidence) | On a scrolled list, right and left arrows changed nothing. Decided by D92; checked as S9.11 in increment 6 |
| Q85 (observation) | removed in increment 6 | Decided by D93; the observation became the check `TestVerifyView_S9_6_7_ScrollByScreenLine` |

| Requirement | Check | Note |
|---|---|---|
| QR11 | `TestVerifyView_QR11_KeysDuringFetch/QR11`, `/QR11_answered` | Since D100 the stand-in answers after 4 s (was 5 s), and PETR4's price is 12.34; `?`, `x`, `s`, `s` pressed between +0.3 s and +0.9 s, during the fetch at start; from writing the key to the first change of the emulated screen: 16 to 18 ms each (limit 100 ms; 2026-11-25 with 4 s: 17 to 18 ms). `QR11_answered`: the held fetch is answered (every request 200, PETR4 `R$ 12,34`, no error line). Not parallel. Until the emulator, not a real terminal's paint |

Not verified from outside, for these: how a resize looks in the user's own
terminal window, and its key encodings (hand check above: drag the window
narrower and wider, and press the arrows, `j`, `k`, `s`, `?`).

### Increment 6: the review's answers, D92 to D96 (`view_increment6_verify_test.go`)

The criteria of increment 6 in `research/build/full-screen-view/scope.md`
(S2.9, S2.10, S9.11, S12.6 to S12.8, S15.10, S18.5, S18.6, and S9.6, S9.7
reworded), on the real view; S18.5 and S18.6 as plain commands with the
output redirected. `resizeAndSettle` resizes and waits for the redraw;
`ttyRun` runs the binary with each of stdin, stdout and stderr on a
pseudo-terminal or on a file. Widths are not fixed numbers: they are derived
from the whole line read at a wide terminal (summary line at 120 columns,
status line at 200). Changed for D92 to D96: the S12.2 check (above), the
comments of the increment-5 scroll check, and the Q85 observation (removed,
now a check). Run of 2026-11-25: the ordinary run of all files, about 2 min
15 s; 158 top-level checks pass, 0 fail, 7 skipped (SLOW and LONG); the 7 new
checks and the 11 criteria pass.

| Criterion | Check | Note |
|---|---|---|
| S2.9 | `TestVerifyView_S2_9_10_SummaryNarrow/S2_9` | Whole line `IBOV 158.432 +0,32%    S&P 500 6,812.40 -0.11%    USD/BRL 5,4321 +0,08%` (71), separator 4 spaces; without percents 50; with one percent at least 57. At 50, 53, 56 (resized) and 50, 56 (started there): `IBOV 158.432    S&P 500 6,812.40    USD/BRL 5,4321` |
| S2.10 | `TestVerifyView_S2_9_10_SummaryNarrow/S2_10` | At 49 and 32: `IBOV 158.432    S&P 500 6,812.40`; at 31 and 12: `IBOV 158.432`; at 11: no summary line (the column header is the first line). Every entry whole, no percent, as many as fit with the 4-space separator, resized and started |
| S9.6, S9.7 | `TestVerifyView_S9_6_7_ScrollByScreenLine/S9_6`, `/S9_7` | 25 tickers: PETR4, AAPL outside groups, `Brasil` (BBDC4), `Outros` (23); 120 x 15. The top line under the column header after `j`, down, `j`, down, `j`: `AAPL`, `Brasil`, `BBDC4`, `Outros`, `ABEV3`; then `k`, up, `k`, up, `k`: `Outros`, `BBDC4`, `Brasil`, `AAPL`, `PETR4` |
| S9.11 | `TestVerifyView_S9_11_LeftRightDoNothing` | The same list scrolled by three `j`; right, left (as the terminal mode sends them), then both again as CSI and as SS3: the screen identical, each watched for 2 s. `j` afterwards still scrolls |
| S12.6 | `TestVerifyView_S12_6_ConfigPathWithTilde/*` | No config; `XDG_CONFIG_HOME` unset (`~/.config/brl-stocks/config.toml`) and `~/xdg` (`~/xdg/brl-stocks/config.toml`); 120 and 200 columns; the home folder's real path nowhere on screen |
| S12.7, S12.8 | `TestVerifyView_S12_7_8_SortLeftOutBeforeWarning/S12_7`, `/S12_8` | Whole status line 114 wide: `B3 open · US open · sort: file order · no config: example watchlist; put yours at ~/.config/brl-stocks/config.toml`; without the sort 95. At 95, 96, 104, 113 (resized and started): `B3 open · US open · no config: example watchlist; put yours at ~/.config/brl-stocks/config.toml`. Recorded: at 114 the whole line; at 94 the warning cut by one character (`...config.tom`, no ellipsis) |
| S15.10 | `TestVerifyView_S15_10_CacheFolderWithTilde/*` | Cache folder 0555 at `~/.cache/brl-stocks` (`XDG_CACHE_HOME` unset) and `~/xc/brl-stocks`: status line `... · quotes are not kept: ~/.cache/brl-stocks cannot be written`. Skipped as root |
| S18.5, S18.6 | `TestVerifyView_S18_5_6_NoTerminal/S18_5`, `/S18_6` | Pipes; pipes with `--config`; stdout to a file with stdin and stderr on a terminal; stdout and stderr to files with stdin on a terminal: exit 2, no request, nothing on stdout, one line `brl-stocks: the full-screen view needs a terminal; for redirected output use \`brl-stocks print\`` |

Recorded outside the criteria: with stdin `/dev/null` and the output on a
terminal the view opens (and `q` on the terminal quits it, exit 0); the spec
speaks of the output only. Without a terminal, an invalid or a missing config
gives the no-terminal line and exit 2, not the config message. At 11 columns
and less the rows are cut (`PETR4     R`), below what S1.6 can keep.

Not verified from outside, for these: the look on the user's own terminal
(hand check: without a config file, `brl-stocks` in a window of about 100
columns, then narrower than 95: the sort goes first, then the warning is cut;
narrow the window below 71 and 50 columns and watch the summary line; with
groups, press `j` and `k` past a group header; press the left and right
arrows; and `brl-stocks > /tmp/out.txt; echo $?` prints `2` with one line on
the terminal).

## Live refresh, slice 3 increment 1 (`refresh_increment1_verify_test.go`)

The 8 criteria of increment 1 in `research/build/live-refresh/scope.md`
(S5.1, S5.2, S5.3, S5.7, S5.8, S5.10, S8.3, S15.3), QR6, QR11 during a
heartbeat refresh, and QR8 with the view, on the real view left open on a
pseudo-terminal while **real** time passes (the tool's own clock; nothing is
stepped from outside). Requests are counted from the stand-in's record by
arrival time, within a window that starts when the process starts. `hold`
keeps the view open for the criterion's time and samples the screen;
it fails the check if the tool exits. Also shown again with the view open:
S4.4, S5.4, S5.5. Run of 2026-11-25 (evidence `20261125T145015Z`): the ordinary run of all files, 3 min 39 s; 166 top-level checks pass, 0 fail, 12 skipped (SLOW and LONG); the 8 new ordinary checks, the 8 criteria (S8.3 partly), QR6 and QR11 pass.

Added to the stand-in, without changing the existing controls:
`opens_at_unix` (`{"b3": <unix>}`: before that time the market is closed and
its last session was the same hours on the previous weekday, 7 h long; from
then on it is open) and `quote_before_close_seconds` (the quote time of a
market whose session is over lies that many seconds before the close).

"Scaled" means the criterion's time was shortened, at the smallest interval
(10 s) unless said otherwise.

```sh
# only these checks (about 3 min 20 s, in parallel)
mise exec -- go test -tags verification ./verification/ -run 'TestVerifyView_(S5_|QR6_|QR11_KeysDuringHeartbeat)' -v
# the literal 10-minute S5.1, S5.2 and QR6 (in parallel, about 10.5 min)
BRL_VERIFY_SLOW=1 mise exec -- go test -tags verification ./verification/ -run 'TestVerifySlow_View_(S5|QR6)' -v -timeout 20m
# QR8 / S15.3 with the view (8 h) and S8.3 (3 h), with the print QR8 run
BRL_VERIFY_LONG=1 mise exec -- go test -tags verification ./verification/ -run TestVerifyLong -v -timeout 9h
```

| Criterion | Check | Note |
|---|---|---|
| S5.1 | `TestVerifyView_S5_1_S4_4_S8_3_Heartbeat/S5_1`; literal: `TestVerifySlow_View_S5_1_DefaultIntervalTenMinutes` (SLOW) | Scaled: `refresh_seconds = 10` for 100 s (ten intervals, as 10 min are ten default intervals), so "no `refresh_seconds`" is only in the SLOW check, not run yet. Empty cache; the fetch at start counts. 2026-11-25: 10 each for PETR4 and VALE3 (+0 s, +10.01 s ... +90.07 s) |
| S5.2 | `TestVerifyView_S5_2_IntervalFromConfig`; literal: `TestVerifySlow_View_S5_2_Interval120TenMinutes` (SLOW) | Scaled: `refresh_seconds = 20` for 100 s (five intervals). 2026-11-25: 5 for every row (+0, +20 s ... +80 s) |
| S5.3 | `TestVerifyView_S5_3_ClosedMarketNotRequested/S5_3` | Reworded by D111/D113 (2026-11-27): the US period ended more than 5 min ago; the stand-in's ended 6 min before every answer (before: closed 2 h ago, for "more than 30 minutes"). Scaled: 60 s in place of 10 min; `print` leaves a quote for every US row first; B3 open as control. 2026-11-25: AAPL, MSFT and ^GSPC 0; control PETR4 5 |
| S5.4, S5.5 (shown again) | `TestVerifyView_S5_3_ClosedMarketNotRequested/S5_4`, `/S5_5` | After 60 s open: US rows show the close, no stale mark; `B3 open · US closed` |
| S5.7 | `TestVerifyView_S5_7_NextSessionStarts` | B3's session starts 25 s after the view opens (`opens_at_unix`); last session the previous weekday. Skipped on a weekend (São Paulo). 2026-11-25: every B3 row first requested 0.4 s after the start (0.85 s in an earlier run); status line `B3 closed` then `B3 open`; no B3 request while closed |
| S5.8 | `TestVerifyView_S5_8_RequestedAgainAfterClose/{ended_60min_ago,ended_99min_ago}/S5_8` | Reworded by D111 (2026-11-27): B3's period ended less than 100 min ago; the stand-in's ended 60 and 99 min before every answer, quote time at the close (before: closed 60 s ago with a quote time 10 min before the close). The second request at most 11 s after the first. 2026-11-27: every B3 row at +0, +10 s, +20 s in both |
| S5.10 | `TestVerifyView_S5_10_IntervalFloor` | `refresh_seconds = 3`, 60 s: the shortest gap between two arrivals of one symbol at least 10 s (50 ms allowed), at most 7 per symbol. 2026-11-25: 6 per symbol, shortest gap 10.004 s; status line `... · refresh_seconds is below 10; using 10` |
| S8.3 | `TestVerifyView_S5_1_S4_4_S8_3_Heartbeat/S8_3` (partial); `TestVerifyLong_View_S8_3_NotFoundThreeHours` (LONG) | Partial: in 100 s, XXXX9 (404) is asked for once (at start), not on every interval. The hour itself only in the 3-hour LONG check, not run yet |
| S4.4 (shown again) | `TestVerifyView_S5_1_S4_4_S8_3_Heartbeat/S4_4` | PETR4 at the top and in two groups: as many requests as VALE3 (±1), never two within 5 s; shown in its three places |
| S15.3 | `TestVerifyView_QR6_S15_3_HealthyThreeMinutes/S15_3`; `TestVerifyLong_View_QR8_S15_3_EightHours` (LONG) | Baseline checked against D108 (2026-11-26, "the first refresh in which every row has its full sparkline"): the stand-in's made-up series already holds 24 prices per entry (the session from 2 h before the answer, every 5 min) in the first answer, so the first full refresh is that refresh; unchanged. Scaled: 3 min at 10 s in place of 8 h. 20 tickers and 3 summary entries, quotes change on every answer (`vary_quotes`); the size (regular files under the cache folder) after the first full refresh, then every 10 s. 2026-11-25: first 22101 bytes, min 22032 (−0.3%), max 22312 (+1.0%), 18 samples, 410 requests after the first refresh. The 8-hour run (interval 60 s, sampled every minute) has not been run |

| Requirement | Check | Note |
|---|---|---|
| QR6 | `TestVerifyView_QR6_S15_3_HealthyThreeMinutes/QR6`; literal: `TestVerifySlow_View_QR6_TenMinutes` (SLOW) | Scaled: 3 min in place of 10, sampled every 10 s, interval 10 s, 20 tickers. Every row has a price and no `!` age mark in every sample; PETR4's price changes on screen (set-up). 2026-11-25: 18 samples, no stale mark, PETR4 changed 17 times |
| QR11 | `TestVerifyView_QR11_KeysDuringHeartbeatRefresh/QR11`, `/QR11_answered` | The view opens on a healthy source; the stand-in then answers after 4 s (D100; was 5 s) with PETR4 at 12.34; when the first request of the next heartbeat refresh has arrived, `?`, `x`, `s`, `s` are pressed. `QR11_answered`: the held refresh is answered (its requests 200, PETR4 `R$ 12,34`, no stale row, no error line). Not parallel. 2026-11-25 with 5 s: 1 to 7 ms for each key; with 4 s: 8 to 17 ms, pressed 3 to 542 ms after the held request arrived, 8 requests in flight. Until the emulator, not a real terminal's paint |
| QR8 (view) | `TestVerifyLong_View_QR8_S15_3_EightHours` (LONG) | Baseline as S15.3 (D108: full sparklines from the first refresh, unchanged). As S15.3, 8 h (`BRL_VERIFY_LONG_DURATION`) at 60 s; also asserts QR6 every minute. Not run yet |

Recorded outside the criteria (2026-11-25, before D111): with B3 closed 60 s ago and the quote time at the close, the B3 rows were still requested every interval; Q62 was then answered by D111 (a settle time, no closing quote), and that observation is now S5.8's check. The heartbeat drifts by about 7 ms per interval (+90.07 s after nine intervals of 10 s), so each next refresh is timed from the end of the last, not from a fixed schedule; at 60 s over 8 h that is about 3.5 s, within S5.1's range.

Not verified from outside, for these: the view left open overnight on a real
terminal, across a real close and open of the markets; the 3- and 8-hour
LONG checks and the 10-minute SLOW checks, not run here; whether a real
source's closing quote ends the period after the close (Q62). Hand check:
build the tool and the stand-in as in the hand check of the view above,
leave `brl-stocks` open in a terminal window (`refresh_seconds = 10`) and
watch, every few minutes, that the quote times move and no `!` mark appears;
`curl -s http://127.0.0.1:18765/_requests | grep -c PETR4` grows by about
6 a minute; switch B3 closed with
`curl -X POST -d '{"closed_markets":["b3"]}' http://127.0.0.1:18765/_control`
and watch the count stop growing after the period after the close; then
`curl -X POST -d '{"opens_at_unix":{"b3":'$(( $(date +%s) + 60 ))'}}' http://127.0.0.1:18765/_control`
and watch the status line turn to `B3 open` and the count grow again within
about 70 s, with no key pressed.

## Live refresh, slice 3 increment 2 (`refresh_increment2_verify_test.go`)

The 5 criteria of increment 2 in `research/build/live-refresh/scope.md`
(S6.4, S6.5, S6.6, S7.5, S7.10), the criteria shown again on a heartbeat
refresh that fails (S6.1, S6.2, S6.3, S6.8, S6.9, S6.10, S6.11, S8.2), and
QR4 and QR5 with the view open and `brl-stocks print` called every 2 s, every
instance counted at the one stand-in. Real time, as in increment 1. An
"attempt" of a block is a burst of requests (a gap of more than 3 s starts a
new one). The waits after failures follow the interval and are scaled to
10 s; the waits of a block (5, 10, 20, 30 min) do not, so the ordinary run
sees the first wait end (S7.10, QR5) and the second wait announced on screen,
and the series is behind SLOW and LONG. Run of 2026-11-25 (evidence `20261125T155447Z`): the ordinary run of all files, 5 min 51 s; 171 top-level checks pass, 0 fail, 18 skipped (SLOW and LONG); the 5 new ordinary checks, the 5 criteria, the 8 shown again, QR4 and QR5 (scaled) pass.

```sh
# only these checks (about 5 min 30 s, in parallel)
mise exec -- go test -tags verification ./verification/ -run 'TestVerifyView_(S6_|S7_10|QR4_Scaled|QR5_)' -v
# S6.4 to 320 s, S7.5 5 and 10 min, QR4 10 min, QR5 16.5 min (in parallel, about 17 min)
BRL_VERIFY_SLOW=1 mise exec -- go test -tags verification ./verification/ -run 'TestVerifySlow_View_(S6_4|S7_5|QR4|QR5)' -v -timeout 30m
# S6.4 at 60 s with the 30-minute cap, S7.5's full series (about 1 h 36 min), with the other LONG checks
BRL_VERIFY_LONG=1 mise exec -- go test -tags verification ./verification/ -run TestVerifyLong -v -timeout 9h
```

| Criterion | Check | Note |
|---|---|---|
| S6.4 | `TestVerifyView_S6_4_S6_1_2_8_9_10_S8_2_RowKeepsFailing/S6_4`; `TestVerifySlow_View_S6_4_TenSecondIntervalToFiveMinutes` (SLOW); `TestVerifyLong_View_S6_4_DefaultIntervalWithCap` (LONG) | Scaled: interval 10 s, so gaps 10, 20, 40, 80 s (−0.5 s / +1.5 s allowed). `print` leaves the cache, then VALE3 answers 503 for good; the other rows answer. The cap of 1,800 s only in LONG, not run. 2026-11-25: gaps 10.01, 20.01, 40.03, 80.05 s; PETR4 every 10 s meanwhile |
| S6.5, S6.6 | `TestVerifyView_S6_3_5_6_11_FailThenRecover/S6_5`, `/S6_6` | Five rows stale after a failed heartbeat refresh (503); the source answers again; marks and error line gone. 2026-11-25: gone 10.0 s after the source answered again (the next attempt, one interval after the first failure) |
| S7.5 | `TestVerifyView_S7_10_S7_5_BlockEndsWithoutKey/S7_5`, `TestVerifyView_QR5_S7_5_Scaled_BlockWithViewAndPrint/S7_5`; `TestVerifySlow_View_S7_5_FiveAndTenMinutes` (SLOW); `TestVerifyLong_View_S7_5_FullSeries` (LONG) | Ordinary: the first wait (5 min) observed as requests, the second (10 min) as the next attempt time on screen after the second refusal. 20 and 30 min only in LONG, not run. 2026-11-25: first attempt 5 min 0.2 s after the first refused answer; error line `source is rate-limiting: 5 rows not updated · next attempt 16:00`; with QR5, after the second refusal the next attempt reads 10 min later (16:10) |
| S7.10 | `TestVerifyView_S7_10_S7_5_BlockEndsWithoutKey/S7_10` | Not scaled (5 min). The view opens healthy; 429 for everything until the first refusal; the source answers 3 s later; no key. 2026-11-25: every row requested at +5 min 0.19 s, then every 10 s; no error line or stale mark afterwards |
| S6.1, S6.2, S6.3, S6.9 (shown again) | `TestVerifyView_S6_3_5_6_11_FailThenRecover/S6_1` ... `/S6_9`; S6.1, S6.2, S6.9 also `..._RowKeepsFailing/*` | On a failed heartbeat refresh: prices kept, `!10s` marks, one line `source unreachable: 5 rows not updated · next attempt HH:MM` |
| S6.8, S6.10, S8.2 (shown again) | `TestVerifyView_S6_4_S6_1_2_8_9_10_S8_2_RowKeepsFailing/S6_8`, `/S6_10`, `/S8_2` | Only VALE3 stale; at the end of the run its mark reads the real age in minutes (scaled from 12 min); XXXX9 not found and the other rows keep refreshing |
| S6.11 (shown again) | `TestVerifyView_S6_3_5_6_11_FailThenRecover/S6_11` | ^GSPC alone 503 on a heartbeat refresh: `source unreachable: summary not updated · next attempt HH:MM` |

| Requirement | Check | Note |
|---|---|---|
| QR4 | `TestVerifyView_QR4_Scaled_ViewAndPrintEveryTwoSeconds`; `TestVerifySlow_View_QR4_TenMinutes` (SLOW) | Scaled: 2 min at 10 s (12 intervals; at most 18 per entry, 414 in total), 20 tickers and 3 summary entries, the view open and `print` every 2 s. 2026-11-25: 60 `print` calls, 276 requests, 12 per entry (one per interval of the view: no `print` call fetched). Full (10 min at 60 s, 345) not run |
| QR5 | `TestVerifyView_QR5_S7_5_Scaled_BlockWithViewAndPrint/QR5`; `TestVerifySlow_View_QR5_SixteenMinutes` (SLOW) | Scaled: 5 min 20 s from the first refused answer in place of 10 min; the view open and `print --refresh` every 2 s, 23 entries; requests counted from the arrivals after the first refusal was written. 2026-11-25: 160 calls; 6 arrivals after the first refusal was written (5 of the first refresh at +0 s, 1 at +5 min). Full not run |

Recorded outside the criteria: after the 5-minute wait of a refusing source (QR5) the tool sent one request, then stopped on its refusal (D98's first request); when the source answered (S7.10) all 8 entries were requested within 10 ms. With the view and `print` together, `print` never fetched while the view was open: the view did all the work and `print` read the cache. A view started on a refusing source with no cache reads `B3 unknown · US unknown`. Stale marks round down (`!2m` at 2 min 47 s).

Not verified from outside, for these: the waits of 20 and 30 minutes and the
cap of 1,800 s (LONG, not run), the 10-minute QR4 and QR5 (SLOW, not run), and
the look of the error line during a block on a real terminal. Hand check: with
the stand-in and the view of the hand check above, run
`curl -X POST -d '{"status":429}' http://127.0.0.1:18765/_control`, watch the
error line `source is rate-limiting: ... · next attempt HH:MM` appear at the
next refresh, then `curl -X POST -d '{}' http://127.0.0.1:18765/_control` and
watch the line go at that HH:MM (5 minutes later) with no key pressed.

## Live refresh, slice 3 increment 3: `r` (`refresh_increment3_verify_test.go`)

The 8 criteria of increment 3 in `research/build/live-refresh/scope.md`
(S7.1, S7.2, S7.3, S7.4, S7.6, S7.7, S7.8, S7.9) on the real view in real
time, and outside the criteria `r` pressed ten times fast, held down, and
during a heartbeat refresh that is still running. `r` is a real byte on the
pseudo-terminal; it is pressed more than 5 s after the last attempt of the
rows it should fetch (the 5-second guard of 6.5). The default interval (60 s)
is used where the heartbeat must not be the cause of a request. Run of 2026-11-25 (evidence `20261125T184812Z`): the ordinary run of all files, 6 min 15 s; 179 top-level checks pass, 0 fail, 18 skipped (SLOW and LONG); the 8 new checks and the 8 criteria pass; this file alone runs in about 40 s.

```sh
# only these checks (about 40 s, in parallel)
mise exec -- go test -tags verification ./verification/ -run 'TestVerifyView_(S7_[1-9]_R|S7_3_4|S7_9|R_During)' -v
```

| Criterion | Check | Note |
|---|---|---|
| S7.1 | `TestVerifyView_S7_1_RRequestsB3Within2s` | Default interval; `r` at +7 s; from the key's write to the arrival at the stand-in. 2026-11-25: 3 to 7 ms |
| S7.2 | `TestVerifyView_S7_2_RDuringWaitAfterFailure` | Scaled: one failure (503 at the fetch at start) puts the next attempt one minute away (error line `... · next attempt HH:MM`), not four; the source then answers; `r` at +7 s. 2026-11-25: every failed row 3 to 4 ms after `r` |
| S7.3, S7.4 | `TestVerifyView_S7_3_4_RDuringBlock/{429,403}/S7_3`, `/S7_4` | Refusal at the fetch at start; `r` at +7 s and +9 s; 5 s watched. No request; status line `B3 unknown · US unknown · next attempt 18:50 · sort: file order` (5 min after the refusal), before and after `r` |
| S7.6 | `TestVerifyView_S7_6_RRequestsClosedMarket` | US closed 2 h ago, cache left by `print`; AAPL and MSFT not requested at start, then 1 ms after `r` |
| S7.7 | `TestVerifyView_S7_7_RTwice/twice_2s/S7_7` | `r` at +7 s and +9 s: one request per row |
| S7.8 | `TestVerifyView_S7_8_RSkipsNotFound` | Scaled: XXXX9 (404) tried 7 s before `r`, not ten minutes; not requested after `r`, the other rows are |
| S7.9 | `TestVerifyView_S7_9_TwentyB3RowsBeforeInterval` | 20 B3 tickers, every answer after 900 ms, default interval: every row requested before 60 s. 2026-11-25: the last 1.8 s after `r` |

| Outside the criteria | Check | Note |
|---|---|---|
| `r` ten times 50 ms apart; held (30 times, 30 ms apart) | `TestVerifyView_S7_7_RTwice/ten_fast_50ms`, `/held_30x30ms` | Asserted as S7.7's rule: one request per row. 2026-11-25: one each |
| `r` during a running heartbeat refresh | `TestVerifyView_R_DuringRunningHeartbeat/{with_r_4s,control_no_r_4s}` | Interval 10 s; answers held 4 s (D100; the 5 s variants were removed); `r` 0.5 s after the heartbeat's first request arrived. Asserted: every held request answered, no stale row and no error line 2 s after the held answers, and none 25 s after the heartbeat started. See below |

Recorded outside the criteria: (1) A heartbeat request answered after 5 s fails in the view: in two runs with `r` and one without, every held request was given up at 5.0 s (the stand-in recorded the client gone, or wrote 200 to a closed connection), the rows turned stale (`!16s`) and the error line read `source unreachable: 5 rows not updated · next attempt HH:MM`; the next attempt, one interval after the failure, recovered them. With answers after 4 s they were taken. So the view gives a request about 5 s. Settled by D100: QR11 is now measured with a stand-in that answers after 4 s, and every QR11 check here uses 4 s. (2) `r` 0.5 s into a running heartbeat refresh sends nothing and changes nothing on screen: no ticker was requested twice, with 4 s or 5 s answers. (3) During a block the status line reads `B3 unknown · US unknown · next attempt HH:MM · sort: file order` before `r` too, and the error line also gives the time. (4) With 20 B3 rows answering after 900 ms, the requests after `r` were spread over 1.8 s (not all at once).

Not verified from outside, for these: S7.2's four-minute wait (scaled to one
minute; four minutes need three failures at the default interval); S7.8's ten
minutes (scaled to 7 s); how `r` looks on a real terminal (nothing on screen
changes when `r` starts a refresh). Hand check: in the view of the hand check
above (default interval), press `r` and watch
`curl -s http://127.0.0.1:18765/_requests | grep -c PETR4` grow by one; press
`r` again at once: it does not grow; switch the stand-in to
`{"status":429}`, wait for the error line, press `r`: the count stays and the
status line shows `next attempt HH:MM`.

## Live refresh, slice 3 increment 4: config changes (`refresh_increment4_verify_test.go`)

The 9 criteria of increment 4 in `research/build/live-refresh/scope.md`
(S8.4, S11.3, S11.4, S11.6, S11.10, S11.11, S11.14, S11.19, S11.20), 7.3's
order of problems on the error line, and outside the criteria a save by
rename, two saves 300 ms apart, an emptied file, a deleted file that comes
back and a config grown to 20 tickers. The checks edit the config in their
temporary folders while the real view runs (real time, real modification
times); each change is timed from the write (or rename, or delete) to the
first screen that shows it. The views run at 200 columns so the long
temporary paths fit; the error line is read as the body line that is neither
a row nor a group header (`otherLines`). The view's QR7 baseline follows the
check's own saves. Nothing is scaled. Run of 2026-11-25 (evidence `20261125T193747Z`): the ordinary run of all files, 6 min 19 s; 187 top-level checks pass, 0 fail, 18 skipped (SLOW and LONG); the 8 new checks, the 9 criteria and the 7.3 check pass; the timings below are from the runs of this file.

```sh
# only these checks (about 25 s, in parallel)
mise exec -- go test -tags verification ./verification/ -run 'TestVerifyView_(S11_3_S8|S11_4_T|S11_6_E|S11_10_11|S11_14_19|S11_20_C|Sec7_3|ConfigSaves)' -v
```

| Criterion | Check | Note |
|---|---|---|
| S11.3, S8.4 | `TestVerifyView_S11_3_S8_4_TickerAdded/S11_3`, `/S8_4` | Default interval; MSFT added at +3 s: row and price 22 ms after the save, MSFT requested 10 ms after it; XXXX9 (404) requested again 10 ms after the save |
| S11.4 | `TestVerifyView_S11_4_TickerRemoved` | AAPL removed: gone after 28 ms; the other rows keep their quotes with no request. A reorder (6.4) showed after 1.0 s |
| S11.6 | `TestVerifyView_S11_6_EditThroughLink` | `~/.config/brl-stocks` a link, `XDG_CONFIG_HOME` unset; the target's file edited: row after 21 ms; the link unchanged |
| S11.10, S11.11 | `TestVerifyView_S11_10_11_SyntaxError` | Unclosed array saved: prices kept; after 22 ms one line `invalid config <path>: toml: line 4 (last key "tickers"): expected a comma (',') or array terminator (']'), but got '['; showing the last valid watchlist` (read at 320 columns; cut at 200, see below); gone 21 ms after the fix |
| S11.14, S11.19 | `TestVerifyView_S11_14_19_Deleted` | After 22 ms: `config <path> was deleted: showing the last watchlist`; the rows stay |
| S11.20 | `TestVerifyView_S11_20_ConfigAppears` | Example watchlist, then a config with MSFT and BBDC4 at the default path: replaced after 21 ms; the no-config warning leaves the status line |
| 7.3 order of problems | `TestVerifyView_Sec7_3_ConfigProblemFirst` | 503 for everything: `source unreachable: 5 rows not updated · next attempt HH:MM`; a broken config saved: after 21 ms the config line alone (no cause line), still alone 3 s later; fixed: the fetch line back after 20 ms |

Recorded outside the criteria (`TestVerifyView_ConfigSaves_Observations`, and
the checks above): a save by rename was followed (row after 23 ms; a second
rename, 1.0 s); two saves 300 ms apart: the screen ended on the second
(726 ms after it); a deleted file that came back with MSFT: row with a price
and no error line after 21 ms; a config grown from 2 to 20 tickers: all 20
rows with quotes 27 ms after the save (18 requests). An emptied file (0 bytes)
is a valid config with no tickers: every row vanished, the default summary
line stayed, and nothing on screen said why. A config error line longer than
the screen is cut at a word without an ellipsis (at 200 columns the end
`; showing the last valid watchlist` is lost). Some changes showed after about
1 s instead of about 20 ms (a reorder, a second rename, the second of two
quick saves); all within 5 s.

Not verified from outside, for these: saves by real editors (vim, VS Code,
nano) on a real terminal, and a config in a real dotfiles repository behind a
link. Hand check: with the view of the hand check above open, edit
`/tmp/brl-view/cfg/brl-stocks/config.toml` in your editor: add `"MSFT"`, save,
watch the row appear; break a bracket, save, watch the error line name the
file; undo, save; `rm` the file and watch `... was deleted ...`; write it back.

## The sparkline, slice 4 increment 1 (`sparkline_increment1_verify_test.go`)

S10.1 to S10.23 on the real view. Q104 and Q105 were answered by D109
(`TODAY` starts where the sparklines start, now in S10.20) and D110 (halfway
takes the higher level, now S10.23); both asserted. The sparkline is
read as the last run of `▁▂▃▄▅▆▇█` blocks on a row; its column from the
emulator's cells. Added to the stand-in, without changing the existing
controls: `series` (`{"<symbol>": {"closes": [...], "before": [...],
"after": [...]}}`): the session's prices (null is a gap, `[]` no prices) and
prices timed before the session's start and after its end, in the answer's
`timestamp` and `indicators`. Nothing is scaled. Run of 2026-11-26 (evidence `20261126T021601Z`): the ordinary run of all files, 6 min 28 s; 197 top-level checks pass, 0 fail, 18 skipped (SLOW and LONG); the 11 new checks and S10.1 to S10.22 pass; this file alone runs in about 15 s.

```sh
# only these checks (about 15 s, in parallel)
mise exec -- go test -tags verification ./verification/ -run 'TestVerifyView_(S10_|SparklineEdge)|TestVerifyPrint_NoSparkline' -v
```

| Criterion | Check | Note |
|---|---|---|
| S10.1, S10.2, S10.4 to S10.7, S10.10 to S10.15, S10.17, S10.20 | `TestVerifyView_S10_SparklineShapes/S10_1` ... `/S10_20` | One screen: PETR4 (B3 open, 24 made-up rising prices): `▁▂▃▃▄▅▆▆▇█`, green, the row ends with it; AAPL (US closed 2 h ago) 10 to 19: `▁▂▃▃▄▅▆▆▇█`, red; VALE3 the same price 10 times: `▄▄▄▄▄▄▄▄▄▄`, no colour; ITUB4 4 prices: `▁▅▃█`; MSFT 25 prices, lowest first, highest last: `▁▃▃▃▄▃▃▄▄█`; BBDC4 no prices and XXXX9 not found: nothing in the column. Header `... TIME   TODAY`, TODAY at the sparklines' column |
| S10.3 | `TestVerifyView_S10_3_NoDetailOnKeys` | `r`, `s` three times, down, up, `j`, `k`, then `?` and `x`: no line that is not a row appears beyond those already there (the status line's sort aside) |
| S10.8, S10.21 | `TestVerifyView_S10_8_21_OneCellNarrower` | Widest line at 120 columns: 87 cells. At 86 (resized and started): no sparkline, no `TODAY`; at 87 both (control); resized back to 120: sparklines back |
| S10.9 | `TestVerifyView_S10_9_OutsideTheSession` | AAPL (closed): 1000 and 5 before the session, 0.01 and 900 after; PETR4 (open): 1000 and 0.01 before; MSFT 0.01 after; every session 10 to 19: all read `▁▂▃▃▄▅▆▆▇█` |
| S10.16 | `TestVerifyView_S10_16_NoDataYet` | Empty cache, answers held: each row its ticker and 7 `—`, nothing after the time's `—` |
| S10.18 | `TestVerifyView_S10_18_StaleKeepsSparkline` | Interval 10 s; 503 after the first fetch: PETR4 `!10s` with the same `▁▂▃▃▄▅▆▆▇█`, and every row's line unchanged |
| S10.19 | `TestVerifyView_S10_19_CachedSparkline` | A view run leaves the cache; answers then held 20 s: PETR4 shows `▁▂▃▃▄▅▆▆▇█` before any answer |
| S10.23 | `TestVerifyView_S10_23_HalfwayTakesHigher` | PETR4 10, 10.5, 17 (10.5 is level 0.5): `▁▂█`. Since 2026-11-26 (D110); before, the halfway price was only recorded (below) |
| S10.22 | `TestVerifyView_S10_22_AlignedAfterWidestTime/{own_rows,20_tickers}` | `print` leaves the cache; 12 s later VALE3 answers 503 (`!12s`): every sparkline and `TODAY` start two cells after the end of `HH:MM !12s`. QR1 |

Recorded outside the criteria (`TestVerifyView_SparklineEdgeCases_Observations`,
`TestVerifyPrint_NoSparklineNoSeries`): gaps are left out (10, gap, 12, gap,
gap, 15, 19 drew `▁▃▅█`, 4 characters); one price draws `▄`; a range of 0.01
to 100000 drew `▁▄█▁█`; 34.150 to 34.153 drew `▁▃▆▃█` (the smallest range is
stretched to the full height); 0, 1, 14 drew `▁▂█` and ITUB4's 34, 35, 34.5,
36 drew `▁▅▃█`, both prices at exactly level 0.5 or 3.5 taking the higher
level (since asserted as S10.23, D110). `print` (table) has no sparkline and no
`TODAY`, `print --json` no series key (7.4, 7.5). With 20 tickers every
sparkline lines up (S10.22).

Not verified from outside, for these: how the blocks and colours look in the
user's own terminal and font (a font without the eighth blocks would show
boxes). Hand check: in the view of the hand check above, look at the `TODAY`
column at full width, then narrow the window until it goes.

## Close, open and holidays, Q62 increment 1 (`markethours_increment1_verify_test.go`)

The criteria of increment 1 in `research/build/market-hours/scope.md` as
reworded by D111 to D113 (S5.3, S5.8, S5.9, S5.11, S5.13 to S5.17, S13.5),
through the real build on the real clock. Settle cases: the stand-in's
trading period ends a chosen time before every answer (`closed_ago_seconds`).
Holiday cases: the answer carries yesterday's period, at the hours of today's
usual open placed a minute ahead (`opens_at_unix`), and keeps carrying it
(`period_from_unix`, never, or until 4 min after the open). Added to the
stand-in, without changing the existing controls: `period_from_unix` and
`session_seconds` (the length of an `opens_at_unix` session, so that S5.15
reaches the usual close in 10 minutes). A holiday check skips itself when
the usual open is not a weekday (New York). The reworded S5.3, S5.8 and S13.5
stay in their files (rows above). Run of 2026-11-27 (evidence `20261127T213605Z`): the ordinary run of all files, 7 min 33 s; 202 top-level checks pass, 0 fail, 20 skipped (SLOW and LONG); the SLOW checks of this file were run once the same day (evidence `20261127T212402Z`) and pass.

```sh
# the ordinary checks of this file (about 2 min 40 s, in parallel)
mise exec -- go test -tags verification ./verification/ -run 'TestVerifyView_(S5_9_15|S5_11_Pre|S5_13_|S5_14_)' -v
# S5.15 to the close, S5.16 and print across a holiday's open (about 12 min)
BRL_VERIFY_SLOW=1 mise exec -- go test -tags verification ./verification/ -run 'TestVerifySlow_(View_S5_15_16|Print_HolidayEveryMinute)' -v -timeout 25m
```

| Criterion | Check | Note |
|---|---|---|
| S5.9 | `TestVerifyView_S5_9_15_HolidayAtOpen/S5_9` | US holiday, usual open 60 s after the view opens; every status line from 5 s to 100 s after it: `B3 open · US closed · sort: file order` |
| S5.11 | `TestVerifyView_S5_11_PreMarketShowsClose` | US session 30 min ahead; the answer's regular series ends at 229.87, with 235.00 and 236.50 after the last session's end: view and `print` show `US$ 229.87`; status `US closed` |
| S5.13 | `TestVerifyView_S5_13_B3SettledNotRequested` | Scaled: 60 s in place of 10 min. B3's period ended 101 min before every answer; `print` leaves the cache; no B3 row (nor ^BVSP) requested; US control 5 |
| S5.14 | `TestVerifyView_S5_14_ClosedDuringSettle/{ended_60s_ago,ended_3600s_ago,ended_5940s_ago}` | Status line `B3 closed · US open · sort: file order` throughout 25 s, while the B3 rows are still requested every 10 s |
| S5.15 | `TestVerifyView_S5_9_15_HolidayAtOpen/S5_15` (partial: to 100 s after the open); `TestVerifySlow_View_S5_15_16/S5_15_to_the_close` (SLOW, a 10-minute session) | `print` leaves the cache first, so the view's start sends nothing. 2026-11-27: AAPL and MSFT at the open (+1 m 0 s) and 4 m 59 s later, nothing else until 30 s after the close |
| S5.16 | `TestVerifySlow_View_S5_15_16/S5_16_late_session` (SLOW) | The answer carries today's session from 4 min after the open. 2026-11-27: the second look at +5 m 59.3 s (4 m 59 s after the first), `US open` on the next sample (+6 m 0 s), then every 10 s |
| S5.17 | not verified | Needs the next weekday's usual open while the view stays open since the holiday: a day of real time. What would make it visible: a clock the stand-in and the tool share (the acceptance tests' boundary). Hand check: on a real US holiday, leave `brl-stocks` open overnight against the real source and see the US rows requested within a minute of the next day's open |

Recorded outside the criteria: `print` called every minute across a
holiday's usual open (`TestVerifySlow_Print_HolidayEveryMinute`, SLOW, run
2026-11-27): the US rows were requested by the call 30 s after the open and
by the call 5 m 30 s after it, and by no other call (two per row, as 6.1
says); B3 (open) was fetched by every call. During the settle time the status
line says only `B3 closed`: nothing tells the user that the rows are still
being refreshed.
