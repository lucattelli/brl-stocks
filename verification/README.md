# Verification of the one-shot mode, from outside

These checks run the **real binary** as a child process against a local
stand-in for the quote source, and assert only on what a user or a script can
observe: standard output, the error stream, the exit status, the requests the
stand-in received, and files on disk. They were written from `docs/spec.md`
(sections 6.3, 6.5, 7, stories S13 to S15, QR3, QR4, QR5, QR7) without reading
the implementation.

A failing check is a finding about the tool. Do not adjust a check until it
passes.

## Run

```sh
# ordinary checks, about 1 minute
mise exec -- go test -tags verification ./verification/ -v

# the slow check as well (recovery after a block: a little over 5 minutes)
BRL_VERIFY_SLOW=1 mise exec -- go test -tags verification ./verification/ -v -timeout 15m

# only the slow check
BRL_VERIFY_SLOW=1 mise exec -- go test -tags verification ./verification/ -run TestVerifySlow -v -timeout 15m
```

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
| S15.3 | not verified | Needs the view running for eight hours of open market |
| S15.4, S15.7 | `TestVerifyS15_04_07_DamagedCacheIsAbsentAndSilent` | Every file in the cache folder emptied, or filled with garbage; compared with a run on no cache |
| S15.5 | not verified | About the full-screen view, which needs a terminal; outside this suite |
| S15.6 | not verified | About the full-screen view |
| S15.8, S15.9 | `TestVerifyS15_08_09_UnwritableCacheFolder` | For `print` only. Three ways of "cannot be written". Skipped when run as root |

### Quality requirements

| Requirement | Check | Note |
|---|---|---|
| QR3 | `TestVerifyQR3_FreshCacheZeroRequestsWithin100ms` | As specified: 20 tickers, 3 summary entries, median of 10 runs. Runs alone (not parallel) |
| QR4 | `TestVerifyQR4_Scaled_PrintEveryTwoSeconds`, `TestVerifyConcurrency_TenPrintsOnEmptyCache` | Scaled and partial: no view (needs a terminal), 30 s in place of 10 minutes, interval 10 s in place of 60, 8 entries in place of 23; bound 6 per entry (1.5 x 4 intervals). The spec's own measurement is not reproduced |
| QR5 | `TestVerifyQR5_BlockUnderLoad`, `TestVerifyQR5_TenPrintsAgainstRefusingSource` | Partial: no view; 30 s in place of 10 minutes. The doubling of the wait after a second refusal (6.3) is not verified: it needs 5 + 10 minutes |
| QR7 | `TestVerifyQR7_ConfigNeverTouched` | Config folder and `config.toml` are both symbolic links. In addition every check compares the config file's checksum around every run |

### From the workflows, not numbered criteria

| Source | Check | Note |
|---|---|---|
| 6.5 row 3 | `TestVerifyW6_5_RefreshWithinFiveSecondsDoesNotFetch` | A forced fetch does not fetch a row attempted less than 5 s ago |
| 6.3 row 1 | `TestVerifyW6_3_RefusedRowsAreStale` | "the rows whose fetch was refused are stale", judged by the stand-in's record of which requests it refused |
| 6.5, QR4 | `TestVerifyConcurrency_TenPrintsOnEmptyCache` | Ten calls at the same moment, empty cache; table and JSON. The spec states no number; see the comment on the check for what is asserted |
| 6.3 rows 4 and 5 | `TestVerifySlow_W6_3_RecoveryAfterBlock` | SLOW. Nothing sent 20 s, 150 s and 285 s after the refusal; quotes again after 5 min 8 s; the record shows how the tool resumed |
| 7.3, 7.4 | `TestVerifySec7_4_RowAndSummaryFormat` | The fields of a row and of the summary line, as in the spec's example |

### Not verified from outside

- Colour on a terminal (S16), and everything about the full-screen view: no
  terminal is attached; every run here is piped.
- "Closed for more than 30 minutes" (S13.5) is tested with 2 hours. The period
  right after the close (6.1) is not explored.
- The hourly wait of a not-found row (6.5 row 1) and waits longer than one
  interval after repeated failures (6.2): they need more real time.
