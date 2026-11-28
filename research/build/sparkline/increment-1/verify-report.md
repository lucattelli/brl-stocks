# Verify report: the sparkline (slice 4), increment 1

The verifier's summary, unedited:

> The sparkline passes all of S10.1–S10.22 on the real view, with no failing check. The final run of all ordinary checks took 6 min 28 s: 197 pass, 0 fail, 18 skipped (SLOW and LONG). The new file `verification/sparkline_increment1_verify_test.go` runs in about 15 s. Three commits on slice4/1-verify, plus a README section.
>
> **1. 421c2e4, the two checks you named** (made stricter where needed, none loosened):
> - **S5.4:** it now expects `DD/MM` when the close falls on the previous day in UTC.
> - **S7.2:** `pressR` already took the time before writing the key. The real cause is that the stand-in records arrivals in whole milliseconds, rounded down. The arrival filter now rounds its bounds down the same way. That fixes every check that counts arrivals after a moment.
>
> **2. c567370, one more check fault**, found in tonight's full run. `S9.9` watched 12 s at a 10 s interval, so the heartbeat refresh changed the screen (the quote time's minute moved) without any key. It now runs at the default interval.
>
> **3. 1999dea, the sparkline:** S10.9: prices before the open (1000, 0.01) and after the close (0.01, 900) leave the line at `▁▂▃▃▄▅▆▆▇█`. S10.13 and S10.14: `▁▂▃▃▄▅▆▆▇█` and `▄▄▄▄▄▄▄▄▄▄`. Colours: green, red, none. Blank column: not-found, no data yet, and no prices. S10.8 and S10.21: at 86 cells (widest line 87) there is no sparkline and no `TODAY`. S10.22 and Q104: sparklines and `TODAY` sit two cells after `HH:MM !12s`, also with 20 tickers. Stand-in: a new `series` control for exact prices, gaps, and prices outside the session.
>
> **Outside the criteria:** gaps are left out (7 slots with 3 gaps draw 4 characters); one price draws `▄`; a range of 0.01 to 100000 drew `▁▄█▁█`; a 0.003 range is stretched to the full height; prices exactly halfway between levels took the higher one, as Q105 recommends (recorded, not asserted); resizing back to full width restores the sparklines; `print` and `print --json` show no sparkline and no series.
>
> **Not verified:** how the blocks look in a real terminal and font. Hand check: look at the `TODAY` column in a real terminal, then narrow the window until it goes.

## Coordinator's check and sorting

- Rerun by the coordinator: all ordinary checks pass (`go test -tags verification ./verification/ -count=1`, 443 s). gofmt and vet clean.
- The three check faults (S5.4 near midnight, S7.2 at millisecond resolution, S9.9 watched across a heartbeat refresh) were faults of the checks against the spec; fixed to follow it. S7.2's bounds are rounded to the stand-in's own resolution, which is not a loosening: a request in the same millisecond as the press cannot be told from one before it.
- Findings outside the criteria: none is a fault. Gaps left out and one price as `▄` follow D104 and D105 (each price drawn, a flat line `▄`). Halfway rounding is Q105.
- One real screen, read by the coordinator (evidence `TestVerifyView_S10_SparklineShapes`, colour codes taken out):

```
TICKER  NAME                       LAST  CHANGE               LOW      HIGH  TIME   TODAY
PETR4   Petrobras PN           R$ 38,42   +0,31  +0,81%     38,02     38,60  02:26  ▁▂▃▃▄▅▆▆▇█
VALE3   Vale ON                R$ 61,07   +0,00  +0,00%     60,80     61,55  02:26  ▄▄▄▄▄▄▄▄▄▄
ITUB4   Itau Unibanco PN       R$ 34,15   -0,35  -1,01%     34,01     34,62  02:26  ▁▅▃█
XXXX9   —                     not found       —       —         —         —  —
```
