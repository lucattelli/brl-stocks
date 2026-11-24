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
| QR8 | `TestVerifySlow_QR8_Scaled_CacheSizeTwentyMinutes` (SLOW), `TestVerifyLong_QR8_CacheSizeEightHours` (LONG) | Quotes change on every answer (`vary_quotes`), open market, 23 entries, `print` every interval + 1 s so each call fetches; cache size (sum of the regular files) after the first full refresh and after every call, each within 10%. SLOW is scaled: 20 min at interval 10 s (2026-11-24: 110 samples, first 20979 bytes, min 20866, max 21273). The 8-hour run at interval 60 s is to be started separately (`BRL_VERIFY_LONG=1`); it has not been run |
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
